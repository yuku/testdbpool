package testdbpool_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/yuku/testdbpool"
	"github.com/yuku/testdbpool/internal/testhelper"
)

func TestPoolIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	pool := testhelper.GetTestDBPool(t)

	config := &testdbpool.Config{
		PoolID:       "test-pool-integration",
		DBPool:       pool,
		MaxDatabases: 3,
		SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
			// Create a test table in template
			_, err := conn.Exec(ctx, `CREATE TABLE test_table (id SERIAL PRIMARY KEY, name TEXT)`)
			return err
		},
		ResetDatabase: func(ctx context.Context, conn *pgx.Conn) error {
			// Truncate all tables to reset
			_, err := conn.Exec(ctx, `TRUNCATE test_table RESTART IDENTITY`)
			return err
		},
	}

	// Create pool
	testPool, err := testdbpool.New(ctx, config)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer testPool.Close()

	// Test acquiring a database
	t.Run("Acquire", func(t *testing.T) {
		db, err := testPool.Acquire(ctx)
		if err != nil {
			t.Fatalf("failed to acquire database: %v", err)
		}
		defer db.Close()

		// Verify we can use the connection
		var result int
		err = db.Conn().QueryRow(ctx, "SELECT 1").Scan(&result)
		if err != nil {
			t.Fatalf("failed to query: %v", err)
		}
		if result != 1 {
			t.Errorf("expected 1, got %d", result)
		}

		// Verify the test table exists
		var exists bool
		err = db.Conn().QueryRow(ctx, `
			SELECT EXISTS (
				SELECT FROM information_schema.tables 
				WHERE table_name = 'test_table'
			)
		`).Scan(&exists)
		if err != nil {
			t.Fatalf("failed to check table: %v", err)
		}
		if !exists {
			t.Error("test_table should exist")
		}
	})

	// Test multiple acquire/release cycles
	t.Run("MultipleAcquireRelease", func(t *testing.T) {
		// Acquire all available databases
		var dbs []*testdbpool.TestDB
		for i := range config.MaxDatabases {
			db, err := testPool.Acquire(ctx)
			if err != nil {
				t.Fatalf("failed to acquire database %d: %v", i, err)
			}
			dbs = append(dbs, db)
		}

		// Verify each database is unique
		seen := make(map[string]bool)
		for i, db := range dbs {
			if seen[db.DatabaseName()] {
				t.Errorf("database %d has duplicate name: %s", i, db.DatabaseName())
			}
			seen[db.DatabaseName()] = true
		}

		// Release all databases
		for _, db := range dbs {
			if err := db.Release(ctx); err != nil {
				t.Fatalf("failed to release database: %v", err)
			}
		}

		// Acquire again to verify reuse
		db, err := testPool.Acquire(ctx)
		if err != nil {
			t.Fatalf("failed to re-acquire database: %v", err)
		}
		defer db.Close()

		// Verify it's one of the previously used databases
		if !seen[db.DatabaseName()] {
			t.Errorf("expected reused database, got new one: %s", db.DatabaseName())
		}
	})

	// Test reset functionality
	t.Run("ResetDatabase", func(t *testing.T) {
		db, err := testPool.Acquire(ctx)
		if err != nil {
			t.Fatalf("failed to acquire database: %v", err)
		}
		defer db.Close()

		// Insert data
		_, err = db.Conn().Exec(ctx, "INSERT INTO test_table (name) VALUES ('test')")
		if err != nil {
			t.Fatalf("failed to insert: %v", err)
		}

		// Verify data exists
		var count int
		err = db.Conn().QueryRow(ctx, "SELECT COUNT(*) FROM test_table").Scan(&count)
		if err != nil {
			t.Fatalf("failed to count: %v", err)
		}
		if count != 1 {
			t.Errorf("expected 1 row, got %d", count)
		}

		// Release and re-acquire
		dbName := db.DatabaseName()
		db.Release(ctx)

		// Try to acquire the same database again
		for range config.MaxDatabases {
			db2, err := testPool.Acquire(ctx)
			if err != nil {
				t.Fatalf("failed to re-acquire: %v", err)
			}

			if db2.DatabaseName() == dbName {
				// Found the same database, verify it was reset
				var count2 int
				err = db2.Conn().QueryRow(ctx, "SELECT COUNT(*) FROM test_table").Scan(&count2)
				if err != nil {
					t.Fatalf("failed to count after reset: %v", err)
				}
				if count2 != 0 {
					t.Errorf("expected 0 rows after reset, got %d", count2)
				}
				db2.Release(ctx)
				break
			}
			db2.Release(ctx)
		}
	})
}

