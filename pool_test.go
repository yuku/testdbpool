package testdbpool

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestAcquire(t *testing.T) {

	rootConn := getRootConnection(t)

	maxSize := 1
	dbpool, err := New(Config{
		Conn:    rootConn,
		MaxSize: maxSize,
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

	for range 10 {
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

		count, err := countTable(context.Background(), pool, "enum_values")
		if err != nil {
			t.Fatalf("failed to count rows in enum_values: %v", err)
		}
		if count != 3 {
			t.Fatalf("expected 3 rows in enum_values, got %d", count)
		}

		count, err = countTable(context.Background(), pool, "entities")
		if err != nil {
			t.Fatalf("failed to count rows in entities: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected 0 rows in entities, got %d", count)
		}

		// Insert a row into entities
		_, err = pool.Exec(context.Background(), "INSERT INTO entities (enum_value) VALUES ('value1')")
		if err != nil {
			t.Fatalf("failed to insert into entities: %v", err)
		}

		count, err = countTable(context.Background(), pool, "entities")
		if err != nil {
			t.Fatalf("failed to count rows in entities: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected 1 rows in entities, got %d", count)
		}
	}

	// Confirm that there are no more than maxSize databases created
	// This is a simple check to ensure that the pool does not create more databases than allowed
	// by MaxSize.
	rows, err := rootConn.Query(context.Background(), "SELECT datname FROM pg_database WHERE datname LIKE 'testdb_%' AND datname <> 'testdb_template';")
	if err != nil {
		t.Fatalf("failed to query databases: %v", err)
	}
	defer rows.Close()
	var datnames []string
	for rows.Next() {
		var datname string
		if err := rows.Scan(&datname); err != nil {
			t.Fatalf("failed to scan database name: %v", err)
		}
		datnames = append(datnames, datname)
	}
	if len(datnames) != maxSize {
		t.Fatalf("expected %d databases, got %d: %v", maxSize, len(datnames), datnames)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("error iterating over database names: %v", err)
	}
}
