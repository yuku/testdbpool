package testdbpool

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestAcquire(t *testing.T) {
	dbpool, err := New(Config{
		Conn: getRootConnection(t),
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

	rows, err := pool.Query(context.Background(), "SELECT COUNT(*) FROM enum_values;")
	if err != nil {
		t.Fatalf("failed to query enum_values: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("expected at least one row in enum_values")
	}
	var count int
	if err := rows.Scan(&count); err != nil {
		t.Fatalf("failed to scan count from enum_values: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 rows in enum_values, got %d", count)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("error during rows iteration: %v", err)
	}
}
