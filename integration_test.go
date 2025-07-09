package testdbpool_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/yuku/testdbpool"
	"github.com/yuku/testdbpool/internal/testhelper"
)

func TestPoolIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	pool := testhelper.GetTestDBPool(t)

	config := &testdbpool.Config{
		PoolID:       "test-pool-integration",
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
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer testPool.Close()

	// Test acquiring a database
	t.Run("Acquire", func(t *testing.T) {
		db, err := testPool.Acquire(ctx)
		if err != nil {
			t.Fatalf("failed to acquire database: %v", err)
		}
		defer db.Close()

		// Verify we can use the connection
		var result int
		err = db.Conn().QueryRow(ctx, "SELECT 1").Scan(&result)
		if err != nil {
			t.Fatalf("failed to query: %v", err)
		}
		if result != 1 {
			t.Errorf("expected 1, got %d", result)
		}

		// Verify the test table exists
		var exists bool
		err = db.Conn().QueryRow(ctx, `
			SELECT EXISTS (
				SELECT FROM information_schema.tables 
				WHERE table_name = 'test_table'
			)
		`).Scan(&exists)
		if err != nil {
			t.Fatalf("failed to check table: %v", err)
		}
		if !exists {
			t.Error("test_table should exist")
		}
	})
}