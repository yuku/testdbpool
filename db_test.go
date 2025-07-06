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