package testdbpool

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yuku/testdbpool/internal"
)

func TestEnsureTablesExist(t *testing.T) {
	ctx := context.Background()
	conn := internal.GetRootConnection(t)

	// Drop tables if they exist to test creation
	_, _ = conn.Exec(ctx, "DROP TABLE IF EXISTS testdbpool_databases")
	_, _ = conn.Exec(ctx, "DROP TABLE IF EXISTS testdbpool_registry")

	// Call ensureTablesExist
	err := ensureTablesExist(conn)
	require.NoError(t, err)

	// Verify testdbpool_registry table exists with correct schema
	var exists bool
	err = conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = 'testdbpool_registry'
		)
	`).Scan(&exists)
	require.NoError(t, err)
	require.True(t, exists, "testdbpool_registry table should exist")

	// Verify columns in testdbpool_registry
	var columnCount int
	err = conn.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public' 
		AND table_name = 'testdbpool_registry'
		AND column_name IN ('pool_name', 'template_database', 'max_size', 'created_at', 'updated_at')
	`).Scan(&columnCount)
	require.NoError(t, err)
	require.Equal(t, 5, columnCount, "testdbpool_registry should have all required columns")

	// Verify testdbpool_databases table exists
	err = conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = 'testdbpool_databases'
		)
	`).Scan(&exists)
	require.NoError(t, err)
	require.True(t, exists, "testdbpool_databases table should exist")

	// Verify columns in testdbpool_databases
	err = conn.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public' 
		AND table_name = 'testdbpool_databases'
		AND column_name IN ('id', 'pool_name', 'database_name', 'in_use', 'process_id', 'created_at', 'last_used_at')
	`).Scan(&columnCount)
	require.NoError(t, err)
	require.Equal(t, 7, columnCount, "testdbpool_databases should have all required columns")

	// Test idempotency - calling again should not error
	err = ensureTablesExist(conn)
	require.NoError(t, err, "ensureTablesExist should be idempotent")
}

func TestPoolRegistry(t *testing.T) {
	ctx := context.Background()
	conn := internal.GetRootConnection(t)

	// Ensure tables exist
	err := ensureTablesExist(conn)
	require.NoError(t, err)

	// Clean up test data
	_, err = conn.Exec(ctx, "DELETE FROM testdbpool_databases")
	require.NoError(t, err)
	_, err = conn.Exec(ctx, "DELETE FROM testdbpool_registry")
	require.NoError(t, err)

	t.Run("RegisterNewPool", func(t *testing.T) {
		poolName := "test_pool_1"
		templateDB := "testdb_template_test_pool_1"
		maxSize := 5

		// Register a new pool
		err := registerPoolInDB(conn, poolName, templateDB, maxSize)
		require.NoError(t, err)

		// Verify it was registered
		info, err := getPoolInfoFromDB(conn, poolName)
		require.NoError(t, err)
		require.NotNil(t, info)
		require.Equal(t, poolName, info.poolName)
		require.Equal(t, templateDB, info.templateDatabase)
		require.Equal(t, maxSize, info.maxSize)
	})

	t.Run("RegisterExistingPool", func(t *testing.T) {
		poolName := "test_pool_2"
		templateDB := "testdb_template_test_pool_2"
		maxSize := 3

		// Register pool first time
		err := registerPoolInDB(conn, poolName, templateDB, maxSize)
		require.NoError(t, err)

		// Register same pool again - should not error
		err = registerPoolInDB(conn, poolName, templateDB, maxSize)
		require.NoError(t, err)

		// Verify only one entry exists
		var count int
		err = conn.QueryRow(ctx, "SELECT COUNT(*) FROM testdbpool_registry WHERE pool_name = $1", poolName).Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)
	})

	t.Run("GetNonExistentPool", func(t *testing.T) {
		// Try to get info for non-existent pool
		info, err := getPoolInfoFromDB(conn, "non_existent_pool")
		require.NoError(t, err)
		require.Nil(t, info, "Should return nil for non-existent pool")
	})

	t.Run("ConflictingPoolInfo", func(t *testing.T) {
		poolName := "test_pool_3"
		templateDB1 := "testdb_template_test_pool_3_v1"
		templateDB2 := "testdb_template_test_pool_3_v2"
		maxSize1 := 2
		maxSize2 := 4

		// Register pool with initial settings
		err := registerPoolInDB(conn, poolName, templateDB1, maxSize1)
		require.NoError(t, err)

		// Try to register same pool with different settings - should return error
		err = registerPoolInDB(conn, poolName, templateDB2, maxSize2)
		require.Error(t, err)
		require.Contains(t, err.Error(), "pool configuration mismatch")
	})
}