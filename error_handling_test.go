package testdbpool_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/yuku/testdbpool"
	"github.com/yuku/testdbpool/internal/testhelper"
)

func TestErrorHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	pool := testhelper.GetTestDBPool(t)

	t.Run("SetupTemplateError", func(t *testing.T) {
		config := &testdbpool.Config{
			PoolID:       "reset-error-test",
			DBPool:       pool,
			MaxDatabases: 1,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				return errors.New("intentional setup error")
			},
			ResetDatabase: func(ctx context.Context, conn *pgx.Conn) error {
				return errors.New("should not be called due to setup error")
			},
		}

		testPool, err := testdbpool.New(ctx, config)
		require.NoError(t, err, "failed to create test database pool")

		// Acquire database successfully
		db, err := testPool.Acquire(ctx)
		require.ErrorContains(t, err, "intentional reset error", "expected error during setup template")
		require.Nil(t, db, "expected nil database on error")
	})

	t.Run("ResetDatabaseError", func(t *testing.T) {
		config := &testdbpool.Config{
			PoolID:       "reset-error-test",
			DBPool:       pool,
			MaxDatabases: 1,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, "CREATE TABLE test_table (id INT)")
				return err
			},
			ResetDatabase: func(ctx context.Context, conn *pgx.Conn) error {
				return errors.New("intentional reset error")
			},
		}

		testPool, err := testdbpool.New(ctx, config)
		require.NoError(t, err, "failed to create test database pool")

		// Acquire database successfully
		db, err := testPool.Acquire(ctx)
		require.NoError(t, err, "failed to acquire test database")

		// Release should not fail even if reset fails
		// (the implementation ignores reset errors currently)
		require.NoError(t, db.Release(ctx), "failed to release test database")
	})

	t.Run("DatabasePoolConnection", func(t *testing.T) {
		// Test that the pool connection works correctly
		config := &testdbpool.Config{
			PoolID:       "pool-connection-test",
			DBPool:       pool,
			MaxDatabases: 2, // Increase to avoid contention
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, "CREATE TABLE test_table (id INT)")
				return err
			},
			ResetDatabase: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, "TRUNCATE test_table")
				return err
			},
		}

		testPool, err := testdbpool.New(ctx, config)
		require.NoError(t, err, "failed to create test database pool")

		// Use a timeout context to prevent indefinite hanging
		acquireCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		db, err := testPool.Acquire(acquireCtx)
		require.NoError(t, err, "failed to acquire test database")
		defer func() { _ = db.Close() }()

		// Verify pool connection works
		var result int
		err = db.Pool().QueryRow(ctx, "SELECT 1").Scan(&result)
		require.NoError(t, err, "failed to query test database")
		require.Equal(t, 1, result, "expected result to be 1")
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		config := &testdbpool.Config{
			PoolID:       "context-cancel-test",
			DBPool:       pool,
			MaxDatabases: 1,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, "CREATE TABLE test_table (id INT)")
				return err
			},
			ResetDatabase: func(ctx context.Context, conn *pgx.Conn) error {
				return nil
			},
		}

		testPool, err := testdbpool.New(ctx, config)
		require.NoError(t, err, "failed to create test database pool")

		// Acquire the only database
		db1, err := testPool.Acquire(ctx)
		require.NoError(t, err, "failed to acquire database")

		// Create cancelled context
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel() // Cancel immediately

		// Try to acquire with cancelled context - should fail quickly
		_, err = testPool.Acquire(cancelCtx)
		if err == nil {
			t.Error("expected error with cancelled context")
		}

		// Release the first database
		_ = db1.Close()

		// Now acquire should work with valid context
		db2, err := testPool.Acquire(ctx)
		require.NoError(t, err, "failed to acquire after release")
		_ = db2.Close()
	})
}
