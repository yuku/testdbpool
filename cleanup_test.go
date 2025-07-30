package testdbpool_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuku/testdbpool"
	"github.com/yuku/testdbpool/internal/testutil"
)

func TestListPools(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	connPool := testutil.GetTestDBPool(t)
	t.Cleanup(testutil.CleanupNumpool(connPool))

	t.Run("empty prefix returns empty list", func(t *testing.T) {
		pools, err := testdbpool.ListPools(ctx, connPool, "nonexistent-prefix-")
		require.NoError(t, err)
		assert.Empty(t, pools, "should return empty list for non-existent prefix")
	})

	t.Run("lists pools with matching prefix", func(t *testing.T) {
		prefix := "test-list-pools-"

		// Create multiple pools with the same prefix
		poolIDs := []string{
			fmt.Sprintf("%shash1", prefix),
			fmt.Sprintf("%shash2", prefix),
			fmt.Sprintf("%shash3", prefix),
		}

		var createdPools []*testdbpool.Pool
		for _, poolID := range poolIDs {
			pool, err := testdbpool.New(ctx, &testdbpool.Config{
				ID:           poolID,
				Pool:         connPool,
				MaxDatabases: 1,
				SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
					_, err := conn.Exec(ctx, `CREATE TABLE test_table (id SERIAL PRIMARY KEY)`)
					return err
				},
			})
			require.NoError(t, err)
			createdPools = append(createdPools, pool)
		}

		// Cleanup all pools at the end
		t.Cleanup(func() {
			for _, pool := range createdPools {
				pool.Cleanup()
			}
		})

		// List pools with the prefix
		pools, err := testdbpool.ListPools(ctx, connPool, prefix)
		require.NoError(t, err)

		// Should find all created pools with correct IDs
		assert.ElementsMatch(t, pools, poolIDs, "should find all pools with matching prefix")
	})

	t.Run("prefix filtering works correctly", func(t *testing.T) {
		// Create pools with different prefixes
		pool1, err := testdbpool.New(ctx, &testdbpool.Config{
			ID:           "prefix1-hash",
			Pool:         connPool,
			MaxDatabases: 1,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `CREATE TABLE test_table (id SERIAL PRIMARY KEY)`)
				return err
			},
		})
		require.NoError(t, err)
		t.Cleanup(pool1.Cleanup)

		pool2, err := testdbpool.New(ctx, &testdbpool.Config{
			ID:           "prefix2-hash",
			Pool:         connPool,
			MaxDatabases: 1,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `CREATE TABLE test_table (id SERIAL PRIMARY KEY)`)
				return err
			},
		})
		require.NoError(t, err)
		t.Cleanup(pool2.Cleanup)

		// List pools with prefix1
		pools, err := testdbpool.ListPools(ctx, connPool, "prefix1-")
		require.NoError(t, err)

		assert.Len(t, pools, 1, "should find only one pool with prefix1")
		assert.Contains(t, pools, "prefix1-hash")
		assert.NotContains(t, pools, "prefix2-hash")
	})
}

func TestCleanupPool(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	connPool := testutil.GetTestDBPool(t)
	t.Cleanup(testutil.CleanupNumpool(connPool))

	t.Run("cleanup nonexistent pool returns error", func(t *testing.T) {
		err := testdbpool.CleanupPool(ctx, connPool, "nonexistent-pool")
		assert.Error(t, err, "should return error for non-existent pool")
	})

	t.Run("cleanup existing pool succeeds", func(t *testing.T) {
		poolID := "test-cleanup-pool"

		// Create a pool
		pool, err := testdbpool.New(ctx, &testdbpool.Config{
			ID:           poolID,
			Pool:         connPool,
			MaxDatabases: 2,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `CREATE TABLE test_table (id SERIAL PRIMARY KEY, name TEXT)`)
				return err
			},
		})
		require.NoError(t, err)

		// Acquire a database to ensure it's actually created
		db, err := pool.Acquire(ctx)
		require.NoError(t, err)

		// Insert some data to verify database exists
		_, err = db.Pool().Exec(ctx, `INSERT INTO test_table (name) VALUES ('test')`)
		require.NoError(t, err)

		// Release the database
		require.NoError(t, db.Release(ctx))

		// Close the pool (but don't cleanup)
		require.NoError(t, pool.Close(ctx))

		// Verify pool exists before cleanup
		pools, err := testdbpool.ListPools(ctx, connPool, "test-cleanup-")
		require.NoError(t, err)
		assert.Contains(t, pools, poolID, "pool should exist before cleanup")

		// Cleanup the pool
		err = testdbpool.CleanupPool(ctx, connPool, poolID)
		require.NoError(t, err, "cleanup should succeed")

		// Verify pool no longer exists
		pools, err = testdbpool.ListPools(ctx, connPool, "test-cleanup-")
		require.NoError(t, err)
		assert.NotContains(t, pools, poolID, "pool should not exist after cleanup")
	})

	t.Run("cleanup pool with active connections", func(t *testing.T) {
		poolID := "test-cleanup-active"

		// Create a pool
		pool, err := testdbpool.New(ctx, &testdbpool.Config{
			ID:           poolID,
			Pool:         connPool,
			MaxDatabases: 1,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `CREATE TABLE test_table (id SERIAL PRIMARY KEY)`)
				return err
			},
		})
		require.NoError(t, err)

		// Acquire but don't release to simulate active connections
		db, err := pool.Acquire(ctx)
		require.NoError(t, err)

		// Cleanup should still work (forcefully if needed)
		err = testdbpool.CleanupPool(ctx, connPool, poolID)
		require.NoError(t, err, "cleanup should succeed even with active connections")

		// Release the database (should handle cleanup gracefully)
		_ = db.Release(ctx)
		// This might error since the pool was cleaned up, which is expected

		// Verify pool no longer exists
		pools, err := testdbpool.ListPools(ctx, connPool, "test-cleanup-active")
		require.NoError(t, err)
		assert.Empty(t, pools, "pool should not exist after cleanup")
	})
}

func TestListAndCleanupIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	connPool := testutil.GetTestDBPool(t)
	t.Cleanup(testutil.CleanupNumpool(connPool))

	t.Run("cleanup scenario with multiple pools", func(t *testing.T) {
		prefix := "myapp-test-"

		// Simulate creating pools with different "hashes"
		oldPoolIDs := []string{
			fmt.Sprintf("%sold-hash-1", prefix),
			fmt.Sprintf("%sold-hash-2", prefix),
		}
		currentPoolID := fmt.Sprintf("%scurrent-hash", prefix)

		// Create old pools
		var oldPools []*testdbpool.Pool
		for _, poolID := range oldPoolIDs {
			pool, err := testdbpool.New(ctx, &testdbpool.Config{
				ID:           poolID,
				Pool:         connPool,
				MaxDatabases: 1,
				SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
					_, err := conn.Exec(ctx, `CREATE TABLE users (id SERIAL PRIMARY KEY, name TEXT)`)
					return err
				},
			})
			require.NoError(t, err)
			oldPools = append(oldPools, pool)
		}

		// Create current pool
		currentPool, err := testdbpool.New(ctx, &testdbpool.Config{
			ID:           currentPoolID,
			Pool:         connPool,
			MaxDatabases: 1,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `CREATE TABLE users (id SERIAL PRIMARY KEY, name TEXT)`)
				return err
			},
		})
		require.NoError(t, err)

		// Close old pools (but don't cleanup)
		for _, pool := range oldPools {
			require.NoError(t, pool.Close(ctx))
		}
		t.Cleanup(currentPool.Cleanup)

		// List all pools with prefix
		pools, err := testdbpool.ListPools(ctx, connPool, prefix)
		require.NoError(t, err)
		assert.Len(t, pools, 3, "should find all 3 pools")

		// Cleanup old pools
		for _, poolID := range oldPoolIDs {
			err := testdbpool.CleanupPool(ctx, connPool, poolID)
			require.NoError(t, err, "should cleanup old pool: %s", poolID)
		}

		// Verify only current pool remains
		pools, err = testdbpool.ListPools(ctx, connPool, prefix)
		require.NoError(t, err)
		assert.Len(t, pools, 1, "should have only 1 pool remaining")
		assert.Contains(t, pools, currentPoolID, "current pool should remain")
	})
}
