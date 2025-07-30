package testdbpool_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuku/testdbpool"
	"github.com/yuku/testdbpool/internal/testutil"
)

func TestPool_Cleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that requires database connection")
	}

	ctx := context.Background()
	connPool := testutil.GetTestDBPool(t)
	t.Cleanup(testutil.CleanupNumpool(connPool))

	t.Run("cleanup empty pool", func(t *testing.T) {
		pool, err := testdbpool.New(ctx, &testdbpool.Config{
			ID:           "test-cleanup-empty",
			Pool:         connPool,
			MaxDatabases: 2,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `CREATE TABLE test_table (id SERIAL PRIMARY KEY)`)
				return err
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pool)

		assert.NotPanics(t, pool.Cleanup)
	})

	t.Run("cleanup pool with acquired databases", func(t *testing.T) {
		pool, err := testdbpool.New(ctx, &testdbpool.Config{
			ID:           "test-cleanup-with-dbs",
			Pool:         connPool,
			MaxDatabases: 2,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `CREATE TABLE test_table (id SERIAL PRIMARY KEY, name TEXT)`)
				return err
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pool)

		db, err := pool.Acquire(ctx)
		require.NoError(t, err)
		require.NotNil(t, db)

		// Check the template database and test databases exist
		require.True(t, testutil.DBExists(t, connPool, db.Name()))
		require.True(t, testutil.DBExists(t, connPool, pool.TemplateDBName()))

		// Call Cleanup - this should not panic even with active databases
		assert.NotPanics(t, pool.Cleanup)

		// Verify that template database and test databases are dropped
		require.False(t, testutil.DBExists(t, connPool, db.Name()))
		require.False(t, testutil.DBExists(t, connPool, pool.TemplateDBName()))
	})

	t.Run("cleanup is idempotent", func(t *testing.T) {
		pool, err := testdbpool.New(ctx, &testdbpool.Config{
			ID:           "test-cleanup-idempotent",
			Pool:         connPool,
			MaxDatabases: 2,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `CREATE TABLE test_table (id SERIAL PRIMARY KEY)`)
				return err
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pool)

		// Acquire a database
		db, err := pool.Acquire(ctx)
		require.NoError(t, err)
		require.NotNil(t, db)

		// Call Cleanup multiple times - should not panic
		assert.NotPanics(t, pool.Cleanup)
		assert.NotPanics(t, pool.Cleanup)
		assert.NotPanics(t, pool.Cleanup)
	})

	t.Run("cleanup after manual close", func(t *testing.T) {
		pool, err := testdbpool.New(ctx, &testdbpool.Config{
			ID:           "test-cleanup-after-close",
			Pool:         connPool,
			MaxDatabases: 2,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `CREATE TABLE test_table (id SERIAL PRIMARY KEY)`)
				return err
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pool)

		// Acquire a database
		db, err := pool.Acquire(ctx)
		require.NoError(t, err)
		require.NotNil(t, db)

		// Manually close the pool first
		err = pool.Close(ctx)
		assert.NoError(t, err)

		// Then call Cleanup - should not panic
		assert.NotPanics(t, pool.Cleanup)

		// Since the pool is closed,
		require.False(t, testutil.DBExists(t, connPool, db.Name()))
		require.False(t, testutil.DBExists(t, connPool, pool.TemplateDBName()))
	})
}
