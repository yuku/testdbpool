package testdbpool_test

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/yuku/testdbpool"
)

// TestTimingIssues specifically tests for timing-related race conditions
func TestTimingIssues(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping timing test in short mode")
	}

	rootDB := getTestRootDB(t)
	defer func() { _ = rootDB.Close() }()

	t.Run("SimultaneousPoolCreation", func(t *testing.T) {
		poolID := "timing_test_pool"
		_ = testdbpool.Cleanup(rootDB, poolID)
		defer func() { _ = testdbpool.Cleanup(rootDB, poolID) }()

		// Try to create the same pool from multiple goroutines
		var wg sync.WaitGroup
		errors := make(chan error, 10)

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				_, err := testdbpool.New(testdbpool.Configuration{
					RootConnection:  rootDB,
					PoolID:          poolID,
					TemplateCreator: createTestSchema,
					ResetFunc:       testdbpool.ResetByTruncate([]string{"posts", "users"}, nil),
				})
				if err != nil {
					errors <- err
				}
			}()
		}

		wg.Wait()
		close(errors)

		// All should succeed
		for err := range errors {
			t.Errorf("Unexpected error during concurrent pool creation: %v", err)
		}
	})

	t.Run("RapidAcquireRelease", func(t *testing.T) {
		poolID := "rapid_timing_pool"
		_ = testdbpool.Cleanup(rootDB, poolID)

		pool, err := testdbpool.New(testdbpool.Configuration{
			RootConnection:  rootDB,
			PoolID:          poolID,
			MaxPoolSize:     2,
			AcquireTimeout:  100 * time.Millisecond, // Very short timeout
			TemplateCreator: createTestSchema,
			ResetFunc:       testdbpool.ResetByTruncate([]string{"posts", "users"}, nil),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = testdbpool.Cleanup(rootDB, poolID) }()

		// Rapidly acquire and release
		for i := 0; i < 20; i++ {
			func() {
				_, err := pool.Acquire(t)
				if err != nil {
					t.Logf("Acquire %d failed (expected some failures): %v", i, err)
					return
				}
				// Simulate very quick operation
				time.Sleep(10 * time.Millisecond)
			}()
		}
	})

	t.Run("DelayedTemplateCreation", func(t *testing.T) {
		poolID := "delayed_template_pool"
		_ = testdbpool.Cleanup(rootDB, poolID)

		// Create pool with slow template creation
		pool, err := testdbpool.New(testdbpool.Configuration{
			RootConnection: rootDB,
			PoolID:         poolID,
			TemplateCreator: func(ctx context.Context, db *sql.DB) error {
				// Simulate slow template creation
				time.Sleep(500 * time.Millisecond)
				return createTestSchema(ctx, db)
			},
			ResetFunc: testdbpool.ResetByTruncate([]string{"posts", "users"}, nil),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = testdbpool.Cleanup(rootDB, poolID) }()

		// Try to acquire from multiple goroutines while template is being created
		var wg sync.WaitGroup
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				t.Run(fmt.Sprintf("concurrent_acquire_%d", i), func(t *testing.T) {
					db, err := pool.Acquire(t)
					if err != nil {
						t.Fatalf("Failed to acquire: %v", err)
					}
					// Use the connection
					var result int
					err = db.QueryRow("SELECT 1").Scan(&result)
					if err != nil {
						t.Errorf("Failed to query: %v", err)
					}
				})
			}(i)
		}
		wg.Wait()
	})
}
