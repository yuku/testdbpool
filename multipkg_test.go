package testdbpool_test

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/yuku/testdbpool"
	"github.com/yuku/testdbpool/internal/testhelper"
)

// TestMultiPackagePoolSharing tests that multiple goroutines can safely share the same pool
// This simulates the scenario where multiple packages use the same testdbpool instance
func TestMultiPackagePoolSharing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	if os.Getenv("TESTDBPOOL_RUN_MULTIPKG_TESTS") != "1" {
		t.Skip("Skipping multipkg test. Set TESTDBPOOL_RUN_MULTIPKG_TESTS=1 to run.")
	}

	ctx := context.Background()
	pool := testhelper.GetTestDBPool(t)

	config := &testdbpool.Config{
		PoolID:       "multi-package-test",
		DBPool:       pool,
		MaxDatabases: 5,
		SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, `
				CREATE TABLE enum_values (
					enum_value VARCHAR(10) PRIMARY KEY
				);

				INSERT INTO enum_values (enum_value) VALUES
					('value1'),
					('value2'),
					('value3');

				CREATE TABLE entities (
					id SERIAL PRIMARY KEY,
					enum_value VARCHAR(10) NOT NULL REFERENCES enum_values(enum_value)
				);
			`)
			return err
		},
		ResetDatabase: func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "TRUNCATE TABLE entities CASCADE;")
			return err
		},
	}

	testPool, err := testdbpool.New(ctx, config)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer testPool.Close()

	// Simulate multiple packages using the same pool concurrently
	const numPackages = 10
	const testsPerPackage = 3

	var wg sync.WaitGroup
	errors := make(chan error, numPackages*testsPerPackage)

	// Each "package" runs multiple tests
	for pkg := range numPackages {
		wg.Add(1)
		go func(packageID int) {
			defer wg.Done()

			// Each package runs multiple tests
			for test := range testsPerPackage {
				func(testID int) {
					db, err := testPool.Acquire(ctx)
					if err != nil {
						errors <- err
						return
					}
					defer db.Close()

					// Verify schema exists
					var count int
					err = db.Conn().QueryRow(ctx, "SELECT COUNT(*) FROM enum_values").Scan(&count)
					if err != nil {
						errors <- err
						return
					}
					if count != 3 {
						errors <- err
						return
					}

					// Insert test data
					_, err = db.Conn().Exec(ctx, 
						"INSERT INTO entities (enum_value) VALUES ($1)", "value1")
					if err != nil {
						errors <- err
						return
					}

					// Verify data was inserted
					err = db.Conn().QueryRow(ctx, "SELECT COUNT(*) FROM entities").Scan(&count)
					if err != nil {
						errors <- err
						return
					}
					if count != 1 {
						errors <- err
						return
					}

					t.Logf("Package %d, Test %d completed successfully with DB %s", 
						packageID, testID, db.DatabaseName())
				}(test)
			}
		}(pkg)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("package test error: %v", err)
	}
}

// TestConcurrentPoolAccess tests that the pool handles concurrent access correctly
func TestConcurrentPoolAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	pool := testhelper.GetTestDBPool(t)

	config := &testdbpool.Config{
		PoolID:       "concurrent-access-test",
		DBPool:       pool,
		MaxDatabases: 3, // Limited to force contention
		SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, `
				CREATE TABLE test_data (
					id SERIAL PRIMARY KEY,
					worker_id INT NOT NULL,
					data TEXT NOT NULL
				);
			`)
			return err
		},
		ResetDatabase: func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "TRUNCATE TABLE test_data RESTART IDENTITY;")
			return err
		},
	}

	testPool, err := testdbpool.New(ctx, config)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer testPool.Close()

	// Track database usage
	dbUsage := make(map[string][]int) // dbName -> []workerIDs
	var mu sync.Mutex

	const numWorkers = 20
	var wg sync.WaitGroup
	errors := make(chan error, numWorkers)

	for worker := range numWorkers {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			db, err := testPool.Acquire(ctx)
			if err != nil {
				errors <- err
				return
			}
			defer db.Close()

			// Track which worker used which database
			mu.Lock()
			dbUsage[db.DatabaseName()] = append(dbUsage[db.DatabaseName()], workerID)
			mu.Unlock()

			// Insert worker data
			_, err = db.Conn().Exec(ctx, 
				"INSERT INTO test_data (worker_id, data) VALUES ($1, $2)",
				workerID, "data from worker")
			if err != nil {
				errors <- err
				return
			}

			// Verify our data exists
			var count int
			err = db.Conn().QueryRow(ctx, 
				"SELECT COUNT(*) FROM test_data WHERE worker_id = $1", workerID).Scan(&count)
			if err != nil {
				errors <- err
				return
			}
			if count != 1 {
				errors <- err
				return
			}

			t.Logf("Worker %d used database %s", workerID, db.DatabaseName())
		}(worker)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("worker error: %v", err)
	}

	// Verify database reuse
	mu.Lock()
	defer mu.Unlock()

	if len(dbUsage) == 0 {
		t.Error("no databases were used")
	}

	if len(dbUsage) > config.MaxDatabases {
		t.Errorf("used %d databases, but max is %d", len(dbUsage), config.MaxDatabases)
	}

	// Each database should have been used by multiple workers (due to limited pool size)
	totalUsage := 0
	for dbName, workers := range dbUsage {
		totalUsage += len(workers)
		t.Logf("Database %s was used by %d workers: %v", dbName, len(workers), workers)
	}

	if totalUsage != numWorkers {
		t.Errorf("expected %d total usages, got %d", numWorkers, totalUsage)
	}
}