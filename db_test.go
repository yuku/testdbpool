package testdbpool

import (
	"context"
	"os"
	"strings"
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

func TestDatabaseAllocation(t *testing.T) {
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

	// Register a test pool
	poolName := "test_alloc_pool"
	templateDB := "testdb_template_alloc"
	maxSize := 2
	err = registerPoolInDB(conn, poolName, templateDB, maxSize)
	require.NoError(t, err)

	// Get current process ID
	processID := os.Getpid()

	t.Run("AcquireFirstDatabase", func(t *testing.T) {
		// Acquire a database when pool is empty
		dbInfo, err := acquireDatabaseFromDB(conn, poolName, processID)
		require.NoError(t, err)
		require.NotNil(t, dbInfo)
		require.Equal(t, poolName, dbInfo.poolName)
		require.True(t, dbInfo.inUse)
		require.Equal(t, processID, dbInfo.processID)
		require.NotEmpty(t, dbInfo.databaseName)
		require.True(t, strings.HasPrefix(dbInfo.databaseName, "testdb_"))

		// Verify database is marked as in use in DB
		var inUse bool
		err = conn.QueryRow(ctx, `
			SELECT in_use FROM testdbpool_databases 
			WHERE database_name = $1
		`, dbInfo.databaseName).Scan(&inUse)
		require.NoError(t, err)
		require.True(t, inUse)
	})

	t.Run("ReleaseDatabase", func(t *testing.T) {
		// First acquire a database
		dbInfo, err := acquireDatabaseFromDB(conn, poolName, processID)
		require.NoError(t, err)
		dbName := dbInfo.databaseName

		// Release it
		err = releaseDatabaseInDB(conn, dbName)
		require.NoError(t, err)

		// Verify it's marked as not in use
		var inUse bool
		var procID *int
		err = conn.QueryRow(ctx, `
			SELECT in_use, process_id FROM testdbpool_databases 
			WHERE database_name = $1
		`, dbName).Scan(&inUse, &procID)
		require.NoError(t, err)
		require.False(t, inUse)
		require.Nil(t, procID)
	})

	t.Run("ReuseReleasedDatabase", func(t *testing.T) {
		// Clean up and start fresh
		_, err = conn.Exec(ctx, "DELETE FROM testdbpool_databases WHERE pool_name = $1", poolName)
		require.NoError(t, err)

		// Acquire and release a database
		dbInfo1, err := acquireDatabaseFromDB(conn, poolName, processID)
		require.NoError(t, err)
		dbName1 := dbInfo1.databaseName

		err = releaseDatabaseInDB(conn, dbName1)
		require.NoError(t, err)

		// Acquire again - should reuse the same database
		dbInfo2, err := acquireDatabaseFromDB(conn, poolName, processID)
		require.NoError(t, err)
		require.Equal(t, dbName1, dbInfo2.databaseName)
	})

	t.Run("MaxSizeEnforcement", func(t *testing.T) {
		// Clean up
		_, err = conn.Exec(ctx, "DELETE FROM testdbpool_databases WHERE pool_name = $1", poolName)
		require.NoError(t, err)

		// Acquire databases up to maxSize
		var acquired []string
		for i := 0; i < maxSize; i++ {
			dbInfo, err := acquireDatabaseFromDB(conn, poolName, processID+i)
			require.NoError(t, err)
			acquired = append(acquired, dbInfo.databaseName)
		}

		// Try to acquire one more - should return nil (no available database)
		dbInfo, err := acquireDatabaseFromDB(conn, poolName, processID+maxSize)
		require.NoError(t, err)
		require.Nil(t, dbInfo, "Should return nil when max size reached")

		// Release one
		err = releaseDatabaseInDB(conn, acquired[0])
		require.NoError(t, err)

		// Now we should be able to acquire
		dbInfo, err = acquireDatabaseFromDB(conn, poolName, processID+maxSize)
		require.NoError(t, err)
		require.NotNil(t, dbInfo)
		require.Equal(t, acquired[0], dbInfo.databaseName) // Should reuse the released one
	})
}