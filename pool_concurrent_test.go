package testdbpool_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuku/testdbpool"
)

// TestMaxPoolSizeEnforcement verifies that the pool correctly enforces MaxPoolSize
// when more goroutines than the pool size try to acquire databases simultaneously
func TestMaxPoolSizeEnforcement(t *testing.T) {
	rootDB := getTestRootDB(t)
	defer rootDB.Close()

	poolID := "test_max_pool_enforcement"
	
	// Clean up before test
	testdbpool.Cleanup(rootDB, poolID)
	
	const maxPoolSize = 3
	const numGoroutines = 10
	
	pool, err := testdbpool.New(testdbpool.Configuration{
		RootConnection:  rootDB,
		PoolID:          poolID,
		MaxPoolSize:     maxPoolSize,
		AcquireTimeout:  5 * time.Second,
		TemplateCreator: createTestSchema,
		ResetFunc:       testdbpool.ResetByTruncate([]string{"posts", "users"}, nil),
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	
	defer testdbpool.Cleanup(rootDB, poolID)

	// Use atomic counters to track results
	var activeCount int32
	var successCount int32
	var poolExhaustedCount int32
	var otherErrorCount int32

	// Barrier to ensure all goroutines start at the same time
	var startWg sync.WaitGroup
	startWg.Add(1)

	// WaitGroup for all goroutines to complete
	var wg sync.WaitGroup

	// Channel to signal when databases should be held
	holdChan := make(chan struct{})

	// First, fill the pool with goroutines that hold databases
	for i := 0; i < maxPoolSize; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			
			t.Run(fmt.Sprintf("holder_%d", id), func(t *testing.T) {
				db, err := pool.Acquire(t)
				if err != nil {
					t.Fatalf("holder %d: failed to acquire database: %v", id, err)
				}
				
				atomic.AddInt32(&activeCount, 1)
				
				// Verify connection works
				var result int
				if err := db.QueryRow("SELECT 1").Scan(&result); err != nil {
					t.Errorf("holder %d: failed to query: %v", id, err)
				}
				
				// Hold the database until signaled
				<-holdChan
				
				atomic.AddInt32(&activeCount, -1)
			})
		}(i)
	}

	// Wait for all holder goroutines to acquire databases
	for atomic.LoadInt32(&activeCount) < int32(maxPoolSize) {
		time.Sleep(10 * time.Millisecond)
	}

	// Now try to acquire more databases concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Wait for the start signal
			startWg.Wait()

			// Try to acquire a database
			t.Run(fmt.Sprintf("concurrent_acquire_%d", id), func(t *testing.T) {
				_, err := pool.Acquire(t)
				if err != nil {
					if containsString(err.Error(), "pool exhausted") {
						atomic.AddInt32(&poolExhaustedCount, 1)
					} else {
						atomic.AddInt32(&otherErrorCount, 1)
						t.Errorf("unexpected error: %v", err)
					}
					return
				}

				// This shouldn't happen since pool is full
				atomic.AddInt32(&successCount, 1)
				t.Error("Expected pool exhausted error, but successfully acquired database")
			})
		}(i)
	}

	// Start all goroutines at once
	startWg.Done()

	// Wait for all acquire attempts to complete
	time.Sleep(100 * time.Millisecond)

	// Verify results
	t.Logf("Max pool size: %d", maxPoolSize)
	t.Logf("Active holders: %d", activeCount)
	t.Logf("Number of concurrent acquire attempts: %d", numGoroutines)
	t.Logf("Successful acquisitions (should be 0): %d", successCount)
	t.Logf("Pool exhausted errors: %d", poolExhaustedCount)
	t.Logf("Other errors: %d", otherErrorCount)

	// All attempts should fail with pool exhausted
	if poolExhaustedCount != int32(numGoroutines) {
		t.Errorf("Expected %d pool exhausted errors, got %d", numGoroutines, poolExhaustedCount)
	}

	// No successful acquisitions should happen
	if successCount != 0 {
		t.Errorf("Expected 0 successful acquisitions, got %d", successCount)
	}

	// Signal holders to release databases
	close(holdChan)

	// Wait for all goroutines to complete
	wg.Wait()
}

// TestPoolWaitingBehavior tests what happens when goroutines wait for available databases
func TestPoolWaitingBehavior(t *testing.T) {
	rootDB := getTestRootDB(t)
	defer rootDB.Close()

	poolID := "test_pool_waiting"
	
	// Clean up before test
	testdbpool.Cleanup(rootDB, poolID)
	
	pool, err := testdbpool.New(testdbpool.Configuration{
		RootConnection:  rootDB,
		PoolID:          poolID,
		MaxPoolSize:     2,
		AcquireTimeout:  2 * time.Second,
		TemplateCreator: createTestSchema,
		ResetFunc:       testdbpool.ResetByTruncate([]string{"posts", "users"}, nil),
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	
	defer testdbpool.Cleanup(rootDB, poolID)

	// Acquire all databases
	db1, err := pool.Acquire(t)
	if err != nil {
		t.Fatalf("failed to acquire first database: %v", err)
	}

	db2, err := pool.Acquire(t)
	if err != nil {
		t.Fatalf("failed to acquire second database: %v", err)
	}

	// Now try to acquire a third one in a goroutine
	errChan := make(chan error, 1)
	go func() {
		_, err := pool.Acquire(t)
		errChan <- err
	}()

	// Wait a bit to ensure the goroutine is blocked
	time.Sleep(500 * time.Millisecond)

	// Check that we haven't received an error yet (still waiting)
	select {
	case err := <-errChan:
		if err == nil {
			t.Error("Expected to be blocked, but acquired a database")
		} else if !containsString(err.Error(), "pool exhausted") {
			t.Errorf("Expected pool exhausted error, got: %v", err)
		}
	default:
		// Good, still waiting
	}

	// Release one database
	db1.Close()

	// The waiting goroutine should now fail with pool exhausted
	// (because we don't implement waiting, just immediate failure)
	select {
	case err := <-errChan:
		if err == nil {
			t.Error("Expected pool exhausted error, got nil")
		} else if !containsString(err.Error(), "pool exhausted") {
			t.Errorf("Expected pool exhausted error, got: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("Timed out waiting for acquire to complete")
	}

	// Clean up
	db2.Close()
}

// TestRapidAcquireRelease tests rapid acquire/release cycles
func TestRapidAcquireRelease(t *testing.T) {
	rootDB := getTestRootDB(t)
	defer rootDB.Close()

	poolID := "test_rapid_acquire"
	
	// Clean up before test
	testdbpool.Cleanup(rootDB, poolID)
	
	pool, err := testdbpool.New(testdbpool.Configuration{
		RootConnection:  rootDB,
		PoolID:          poolID,
		MaxPoolSize:     2,
		TemplateCreator: createTestSchema,
		ResetFunc:       testdbpool.ResetByTruncate([]string{"posts", "users"}, nil),
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	
	defer testdbpool.Cleanup(rootDB, poolID)

	const numIterations = 20
	successCount := 0

	for i := 0; i < numIterations; i++ {
		// Create a sub-test for each iteration
		t.Run(fmt.Sprintf("iteration_%d", i), func(t *testing.T) {
			db, err := pool.Acquire(t)
			if err != nil {
				t.Fatalf("failed to acquire database on iteration %d: %v", i, err)
			}

			// Quick operation
			var result int
			if err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&result); err != nil {
				t.Errorf("failed to query on iteration %d: %v", i, err)
			}

			successCount++
			// Database is automatically released when sub-test completes
		})
	}

	if successCount != numIterations {
		t.Errorf("Expected %d successful iterations, got %d", numIterations, successCount)
	}
}

