package testdbpool_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/yuku/testdbpool"
	"github.com/yuku/testdbpool/internal/testhelper"
)

func TestPoolIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()

	// Test acquiring a database
	t.Run("Acquire", func(t *testing.T) {
		pool := testhelper.GetTestDBPool(t)

		config := &testdbpool.Config{
			PoolID:       "test-acquire-integration",
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
		require.NoError(t, err, "failed to create pool")

		// Use timeout to prevent hanging
		acquireCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		db, err := testPool.Acquire(acquireCtx)
		require.NoError(t, err, "failed to acquire database")
		defer func() { _ = db.Close() }()

		// Verify we can use the connection
		var result int
		err = db.Pool().QueryRow(ctx, "SELECT 1").Scan(&result)
		require.NoError(t, err, "failed to query")
		if result != 1 {
			t.Errorf("expected 1, got %d", result)
		}

		// Verify the test table exists
		var exists bool
		err = db.Pool().QueryRow(ctx, `
			SELECT EXISTS (
				SELECT FROM information_schema.tables
				WHERE table_name = 'test_table'
			)
		`).Scan(&exists)
		require.NoError(t, err, "failed to check table existence")
		if !exists {
			t.Error("test_table should exist")
		}
	})

	// Test multiple acquire/release cycles
	t.Run("MultipleAcquireRelease", func(t *testing.T) {
		pool := testhelper.GetTestDBPool(t)

		config := &testdbpool.Config{
			PoolID:       "test-multiple-integration",
			DBPool:       pool,
			MaxDatabases: 3,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `CREATE TABLE test_table (id SERIAL PRIMARY KEY, name TEXT)`)
				return err
			},
			ResetDatabase: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `TRUNCATE test_table RESTART IDENTITY`)
				return err
			},
		}

		testPool, err := testdbpool.New(ctx, config)
		require.NoError(t, err, "failed to create pool")

		// Use timeout
		acquireCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		// Acquire all available databases
		var dbs []*testdbpool.TestDB
		for i := range config.MaxDatabases {
			db, err := testPool.Acquire(acquireCtx)
			require.NoErrorf(t, err, "failed to acquire database %d", i)
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
			err := db.Release(ctx)
			require.NoError(t, err, "failed to release database")
		}

		// Acquire again to verify reuse
		db, err := testPool.Acquire(acquireCtx)
		require.NoError(t, err, "failed to re-acquire database")
		defer func() { _ = db.Close() }()

		// Verify it's one of the previously used databases
		if !seen[db.DatabaseName()] {
			t.Errorf("expected reused database, got new one: %s", db.DatabaseName())
		}
	})

	// Test reset functionality
	t.Run("ResetDatabase", func(t *testing.T) {
		pool := testhelper.GetTestDBPool(t)

		config := &testdbpool.Config{
			PoolID:       "test-reset-integration",
			DBPool:       pool,
			MaxDatabases: 2,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `CREATE TABLE test_table (id SERIAL PRIMARY KEY, name TEXT)`)
				return err
			},
			ResetDatabase: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `TRUNCATE test_table RESTART IDENTITY`)
				return err
			},
		}

		testPool, err := testdbpool.New(ctx, config)
		require.NoError(t, err, "failed to create pool")

		// Use timeout
		acquireCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		db, err := testPool.Acquire(acquireCtx)
		require.NoError(t, err, "failed to acquire database")

		// Insert data
		_, err = db.Pool().Exec(ctx, "INSERT INTO test_table (name) VALUES ('test')")
		require.NoError(t, err, "failed to insert")

		// Verify data exists
		var count int
		err = db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM test_table").Scan(&count)
		require.NoError(t, err, "failed to count")
		if count != 1 {
			t.Errorf("expected 1 row, got %d", count)
		}

		// Release and re-acquire
		dbName := db.DatabaseName()
		_ = db.Release(ctx)

		// Try to acquire the same database again
		for range 2 {
			db2, err := testPool.Acquire(acquireCtx)
			require.NoError(t, err, "failed to re-acquire")

			if db2.DatabaseName() == dbName {
				// Found the same database, verify it was reset
				var count2 int
				err = db2.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM test_table").Scan(&count2)
				require.NoError(t, err, "failed to count after reset")
				if count2 != 0 {
					t.Errorf("expected 0 rows after reset, got %d", count2)
				}
				_ = db2.Release(ctx)
				break
			}
			_ = db2.Release(ctx)
		}
	})
}
