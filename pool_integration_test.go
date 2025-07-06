package testdbpool

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/yuku/testdbpool/internal"
)

// TestNewWithDBBasedPoolManagement tests that New() uses database-based pool management
func TestNewWithDBBasedPoolManagement(t *testing.T) {
	ctx := context.Background()
	conn := internal.GetRootConnection(t)

	// Ensure tables exist
	err := EnsureTablesExist(conn)
	require.NoError(t, err)

	// Clean up test data
	_, err = conn.Exec(ctx, "DELETE FROM testdbpool_databases")
	require.NoError(t, err)
	_, err = conn.Exec(ctx, "DELETE FROM testdbpool_registry")
	require.NoError(t, err)

	// Create a pool with PoolName
	poolName := "test_integration_pool"
	config := Config{
		Conn:     conn,
		PoolName: poolName,
		MaxSize:  2,
		SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "CREATE TABLE test_table (id INT)")
			return err
		},
		ResetDatabase: func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "TRUNCATE TABLE test_table CASCADE;")
			return err
		},
	}

	pool, err := New(config)
	require.NoError(t, err)
	defer pool.Close()

	// Verify pool was registered in database
	var count int
	err = conn.QueryRow(ctx, "SELECT COUNT(*) FROM testdbpool_registry WHERE pool_name = $1", poolName).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count, "Pool should be registered in database")

	// Acquire a database
	db, err := pool.Acquire()
	require.NoError(t, err)
	defer db.Release()

	// Verify database is tracked in database
	err = conn.QueryRow(ctx, `
		SELECT COUNT(*) FROM testdbpool_databases 
		WHERE pool_name = $1 AND in_use = true AND process_id = $2
	`, poolName, os.Getpid()).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count, "Database should be tracked as in use")

	// Verify template was created
	var exists bool
	templateName := "testdb_template_" + poolName
	err = conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", templateName).Scan(&exists)
	require.NoError(t, err)
	require.True(t, exists, "Template database should be created")
}

// TestCrossProcessPoolSharing tests that multiple processes can share a pool
func TestCrossProcessPoolSharing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cross-process test in short mode")
	}

	ctx := context.Background()
	conn := internal.GetRootConnection(t)

	// Ensure tables exist
	err := EnsureTablesExist(conn)
	require.NoError(t, err)

	// Clean up test data
	_, err = conn.Exec(ctx, "DELETE FROM testdbpool_databases")
	require.NoError(t, err)
	_, err = conn.Exec(ctx, "DELETE FROM testdbpool_registry")
	require.NoError(t, err)

	poolName := "cross_process_pool"
	
	// Write a helper program that acquires a database and prints its name
	helperCode := `
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yuku/testdbpool"
)

func main() {
	ctx := context.Background()
	
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		connStr = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
	}
	
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		panic(err)
	}
	defer conn.Close(ctx)

	config := testdbpool.Config{
		Conn:     conn,
		PoolName: "` + poolName + `",
		MaxSize:  2,
		SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "CREATE TABLE test_table (id INT)")
			return err
		},
		ResetDatabase: func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "TRUNCATE TABLE test_table CASCADE")
			return err
		},
	}

	pool, err := testdbpool.New(config)
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	db, err := pool.Acquire()
	if err != nil {
		panic(err)
	}
	
	// Print the database name
	fmt.Println(db.Pool.Config().ConnConfig.Database)
	
	// Hold the database for 2 seconds to ensure overlap with other process
	time.Sleep(2 * time.Second)
	
	db.Release()
}
`

	// Write helper program to file
	helperFile := "/tmp/testdbpool_helper.go"
	err = os.WriteFile(helperFile, []byte(helperCode), 0644)
	require.NoError(t, err)
	defer os.Remove(helperFile)

	// Run two processes concurrently
	cmd1 := exec.Command("go", "run", helperFile)
	cmd1.Env = append(os.Environ(), "DATABASE_URL="+os.Getenv("DATABASE_URL"))
	
	cmd2 := exec.Command("go", "run", helperFile)
	cmd2.Env = append(os.Environ(), "DATABASE_URL="+os.Getenv("DATABASE_URL"))

	// Start both processes
	var output1, output2 []byte
	var err1, err2 error
	var wg sync.WaitGroup
	wg.Add(2)
	
	go func() {
		defer wg.Done()
		output1, err1 = cmd1.CombinedOutput()
		if err1 != nil {
			t.Logf("cmd1 output: %s", output1)
		}
	}()
	
	go func() {
		defer wg.Done()
		output2, err2 = cmd2.CombinedOutput()
		if err2 != nil {
			t.Logf("cmd2 output: %s", output2)
		}
	}()
	
	wg.Wait()
	
	require.NoError(t, err1, "cmd1 failed")
	require.NoError(t, err2, "cmd2 failed")

	dbName1 := string(output1)
	dbName2 := string(output2)

	// The two processes should get different databases (since MaxSize=2)
	require.NotEqual(t, dbName1, dbName2, "Processes should get different databases")

	// Verify both databases are registered
	var count int
	err = conn.QueryRow(ctx, `
		SELECT COUNT(*) FROM testdbpool_databases WHERE pool_name = $1
	`, poolName).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 2, count, "Both databases should be registered")
}