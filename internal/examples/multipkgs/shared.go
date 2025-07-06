package multipkgs

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/yuku/testdbpool"
	"github.com/yuku/testdbpool/internal"
)

// shared pool name for sub-packages to use
const poolName = "testdbpool_multi_package"

func RunTest(t *testing.T) {
	rootConn := internal.GetRootConnection(t)

	// Ensure tables exist for cross-process coordination
	err := testdbpool.EnsureTablesExist(rootConn)
	require.NoError(t, err, "failed to ensure tables exist")

	maxSize := 10 // Increase to handle multiple processes
	dbpool, err := testdbpool.New(testdbpool.Config{
		PoolName: poolName,
		Conn:     rootConn,
		MaxSize:  maxSize,
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
	})
	require.NoError(t, err, "failed to create pool")

	// Simple test - just acquire one database and verify it works
	t.Log("Acquiring database from pool")
	acquired, err := dbpool.Acquire()
	require.NoError(t, err, "failed to acquire database")
	defer acquired.Release()

	t.Log("Testing database connection")
	ctx := context.Background()
	require.NoError(t, acquired.Pool.Ping(ctx), "failed to ping database")

	// Verify tables were created
	var count int
	err = acquired.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM enum_values").Scan(&count)
	require.NoError(t, err, "failed to query enum_values")
	require.Equal(t, 3, count, "expected 3 enum values")

	// Query databases for this specific pool from testdbpool_databases table
	rows, err := rootConn.Query(context.Background(), `
		SELECT database_name 
		FROM testdbpool_databases 
		WHERE pool_name = $1
	`, poolName)
	require.NoError(t, err, "failed to query databases")
	defer rows.Close()

	var datnames []string
	for rows.Next() {
		var datname string
		require.NoError(t, rows.Scan(&datname), "failed to scan database name")
		datnames = append(datnames, datname)
	}
	// We should have at most maxSize databases, but likely fewer since we're only running 5 goroutines
	require.LessOrEqualf(t, len(datnames), maxSize, "expected at most %d databases, got %d: %v", maxSize, len(datnames), datnames)
	require.GreaterOrEqualf(t, len(datnames), 1, "expected at least 1 database, got %d: %v", len(datnames), datnames)
	require.NoError(t, rows.Err(), "error iterating over database names")
}
