package testdbpool_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
		t.Parallel()

		testPool, err := testdbpool.New(ctx, &testdbpool.Config{
			PoolID:       t.Name(),
			DBPool:       pool,
			MaxDatabases: 1,
			SetupTemplate: func(ctx context.Context, pool *pgxpool.Pool) error {
				return errors.New("intentional setup error")
			},
			ResetDatabase: func(ctx context.Context, pool *pgxpool.Pool) error {
				return errors.New("should not be called due to setup error")
			},
		})
		require.NoError(t, err, "failed to create test database pool")
		require.NoError(t, err, testPool.DropAllDatabases(ctx))

		// Acquire database successfully
		db, err := testPool.Acquire(ctx)
		require.ErrorContains(t, err, "intentional setup error", "expected error during setup template")
		require.Nil(t, db, "expected nil database on error")
	})

	t.Run("ResetDatabaseError", func(t *testing.T) {
		t.Parallel()

		testPool, err := testdbpool.New(ctx, &testdbpool.Config{
			PoolID:       t.Name(),
			DBPool:       pool,
			MaxDatabases: 1,
			SetupTemplate: func(ctx context.Context, pool *pgxpool.Pool) error {
				_, err := pool.Exec(ctx, "CREATE TABLE test_table (id INT)")
				return err
			},
			ResetDatabase: func(ctx context.Context, pool *pgxpool.Pool) error {
				return errors.New("intentional reset error")
			},
		})
		require.NoError(t, err, "failed to create test database pool")
		require.NoError(t, err, testPool.DropAllDatabases(ctx))

		// Acquire database successfully
		db, err := testPool.Acquire(ctx)
		require.NoError(t, err, "failed to acquire test database")

		// Release should return error due to reset failure
		err = db.Release(ctx)
		require.ErrorContains(t, err, "intentional reset error")
	})

	t.Run("DatabasePoolConnection", func(t *testing.T) {
		t.Parallel()

		// Test that the pool connection works correctly
		testPool, err := testdbpool.New(ctx, &testdbpool.Config{
			PoolID:       t.Name(),
			DBPool:       pool,
			MaxDatabases: 2, // Increase to avoid contention
			SetupTemplate: func(ctx context.Context, pool *pgxpool.Pool) error {
				_, err := pool.Exec(ctx, "CREATE TABLE test_table (id INT)")
				return err
			},
			ResetDatabase: func(ctx context.Context, pool *pgxpool.Pool) error {
				_, err := pool.Exec(ctx, "TRUNCATE test_table")
				return err
			},
		})
		require.NoError(t, err, "failed to create test database pool")
		require.NoError(t, err, testPool.DropAllDatabases(ctx))

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
		t.Parallel()

		testPool, err := testdbpool.New(ctx, &testdbpool.Config{
			PoolID:       t.Name(),
			DBPool:       pool,
			MaxDatabases: 1,
			SetupTemplate: func(ctx context.Context, pool *pgxpool.Pool) error {
				_, err := pool.Exec(ctx, "CREATE TABLE test_table (id INT)")
				return err
			},
			ResetDatabase: func(ctx context.Context, pool *pgxpool.Pool) error {
				return nil
			},
		})
		require.NoError(t, err, "failed to create test database pool")
		require.NoError(t, err, testPool.DropAllDatabases(ctx))

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
