package testdbpool

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestAcquire(t *testing.T) {
	rootConn := getRootConnection(t)
	dbpool, err := New(Config{
		Conn: rootConn,
		SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, `
				CREATE TABLE enum_values (
					enum_value VARCHAR(10) PRIMARY KEY
				);

				INSERT INTO enum_values (enum_value) VALUES
					('value1'),
					('value2'),
					('value3');
			`)
			return err
		},
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}

	// Ensure the template database hasn't been created yet.
	var count int64
	err = rootConn.QueryRow(context.Background(), "SELECT COUNT(*) FROM pg_database WHERE datname LIKE 'testdb_%';").Scan(&count)
	if err != nil {
		t.Fatalf("failed to count existing test databases: %v", err)
	}
	if count > 0 {
		t.Fatalf("found %d existing test databases, expected none", count)
	}

	pool, err := dbpool.Acquire()
	if err != nil {
		t.Fatalf("failed to acquire pool: %v", err)
	}

	if pool == nil {
		t.Fatal("acquired pool is nil")
	}

	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("failed to ping acquired pool: %v", err)
	}

	err = pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM enum_values;").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query enum_values: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 rows in enum_values, got %d", count)
	}
}
