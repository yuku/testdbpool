package testdbpool

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"
)

func TestAcquireSequentially(t *testing.T) {
	// This test ensures that the pool can be acquired sequentially without
	// issues. It uses a small pool size to force the creation of new databases
	// for each acquisition. The test checks that the template database is
	// created correctly and that each acquired pool has the expected schema
	// and data.

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
		ResetDatabase: func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, `
				TRUNCATE TABLE entities CASCADE;
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

	for range 3 {
		func() {
			acquired, err := dbpool.Acquire()
			if err != nil {
				t.Fatalf("failed to acquire pool: %v", err)
			}
			defer acquired.Release()

			if acquired.Pool == nil {
				t.Fatal("acquired pool is nil")
			}

			if err := acquired.Pool.Ping(context.Background()); err != nil {
				t.Fatalf("failed to ping acquired pool: %v", err)
			}

			count, err := countTable(context.Background(), acquired.Pool, "enum_values")
			if err != nil {
				t.Fatalf("failed to count rows in enum_values: %v", err)
			}
			if count != 3 {
				t.Fatalf("expected 3 rows in enum_values, got %d", count)
			}

			count, err = countTable(context.Background(), acquired.Pool, "entities")
			if err != nil {
				t.Fatalf("failed to count rows in entities: %v", err)
			}
			if count != 0 {
				t.Fatalf("expected 0 rows in entities, got %d", count)
			}

			// Insert a row into entities
			_, err = acquired.Pool.Exec(context.Background(), "INSERT INTO entities (enum_value) VALUES ('value1')")
			if err != nil {
				t.Fatalf("failed to insert into entities: %v", err)
			}

			count, err = countTable(context.Background(), acquired.Pool, "entities")
			if err != nil {
				t.Fatalf("failed to count rows in entities: %v", err)
			}
			if count != 1 {
				t.Fatalf("expected 1 rows in entities, got %d", count)
			}
		}()
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

func TestAcquireParallel(t *testing.T) {
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
		ResetDatabase: func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, `
				TRUNCATE TABLE entities CASCADE;
			`)
			return err
		},
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}

	g, ctx := errgroup.WithContext(context.Background())

	for i := range 5 {
		g.Go(func() error {
			t.Logf("[%d] Acquiring pool", i)
			acquired, err := dbpool.Acquire()
			if err != nil {
				return fmt.Errorf("[%d] failed to acquire pool: %w", i, err)
			}
			defer acquired.Release()

			if acquired.Pool == nil {
				return fmt.Errorf("[%d] acquired pool is nil", i)
			}

			t.Logf("[%d] Pinging acquired pool", i)
			if err := acquired.Pool.Ping(ctx); err != nil {
				return fmt.Errorf("[%d] failed to ping acquired pool: %w", i, err)
			}

			t.Logf("[%d] Counting rows in enum_values", i)
			count, err := countTable(ctx, acquired.Pool, "enum_values")
			if err != nil {
				return fmt.Errorf("[%d] failed to count rows in enum_values: %w", i, err)
			}
			if count != 3 {
				return fmt.Errorf("[%d] expected 3 rows in enum_values, got %d", i, count)
			}

			t.Logf("[%d] Counting rows in entities", i)
			count, err = countTable(ctx, acquired.Pool, "entities")
			if err != nil {
				return fmt.Errorf("[%d] failed to count rows in entities: %w", i, err)
			}
			if count != 0 {
				return fmt.Errorf("[%d] expected 0 rows in entities, got %d", i, count)
			}

			t.Logf("[%d] Inserting into entities", i)
			_, err = acquired.Pool.Exec(ctx, "INSERT INTO entities (enum_value) VALUES ('value1')")
			if err != nil {
				return fmt.Errorf("[%d] failed to insert into entities: %w", i, err)
			}

			t.Logf("[%d] Counting rows in entities after insert", i)
			count, err = countTable(ctx, acquired.Pool, "entities")
			if err != nil {
				return fmt.Errorf("[%d] failed to count rows in entities: %w", i, err)
			}
			if count != 1 {
				return fmt.Errorf("[%d] expected 1 rows in entities, got %d", i, count)
			}

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		t.Fatalf("error during parallel acquisition: %v", err)
	}

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
