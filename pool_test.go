package testdbpool

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

// TestAcquireSequentially ensures that the pool can be acquired sequentially
// without issues. It uses a small pool size to force the creation of new
// databases for each acquisition. The test checks that the template database is
// created correctly and that each acquired pool has the expected schema and data.
func TestAcquireSequentially(t *testing.T) {
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
			_, err := conn.Exec(ctx, "TRUNCATE TABLE entities CASCADE;")
			return err
		},
	})
	require.NoError(t, err, "failed to create pool")

	// Ensure the template database hasn't been created yet.
	var count int
	err = rootConn.QueryRow(context.Background(), "SELECT COUNT(*) FROM pg_database WHERE datname LIKE 'testdb_%';").Scan(&count)
	require.NoError(t, err, "failed to count existing test databases")
	require.Zero(t, count, "expected no existing test databases")

	for range 3 {
		func() {
			acquired, err := dbpool.Acquire()
			require.NoError(t, err, "failed to acquire pool")
			defer acquired.Release()

			require.NotNil(t, acquired.Pool, "acquired pool should not be nil")
			require.NoError(t, acquired.Pool.Ping(context.Background()), "failed to ping acquired pool")

			count, err := countTable(context.Background(), acquired.Pool, "enum_values")
			require.NoError(t, err, "failed to count rows in enum_values")
			require.Equal(t, 3, count, "expected 3 rows in enum_values")

			count, err = countTable(context.Background(), acquired.Pool, "entities")
			require.NoError(t, err, "failed to count rows in entities")
			require.Zero(t, count, "expected 0 rows in entities")

			// Insert a row into entities
			_, err = acquired.Pool.Exec(context.Background(), "INSERT INTO entities (enum_value) VALUES ('value1')")
			require.NoError(t, err, "failed to insert into entities")

			count, err = countTable(context.Background(), acquired.Pool, "entities")
			require.NoError(t, err, "failed to count rows in entities after insert")
			require.Equal(t, 1, count, "expected 1 row in entities after insert")
		}()
	}

	// Confirm that there are no more than maxSize databases created
	// This is a simple check to ensure that the pool does not create more databases than allowed
	// by MaxSize.
	rows, err := rootConn.Query(context.Background(), "SELECT datname FROM pg_database WHERE datname LIKE 'testdb_%' AND datname <> 'testdb_template';")
	require.NoError(t, err, "failed to query databases")
	defer rows.Close()

	var datnames []string
	for rows.Next() {
		var datname string
		require.NoError(t, rows.Scan(&datname), "failed to scan database name")
		datnames = append(datnames, datname)
	}
	require.Lenf(t, datnames, maxSize, "expected %d databases, got %d: %v", maxSize, len(datnames), datnames)
	require.NoError(t, rows.Err(), "error iterating over database names")
}

// TestAcquireParallel tests Acquire in parallel to single dbpool.
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
			_, err := conn.Exec(ctx, "TRUNCATE TABLE entities CASCADE;")
			return err
		},
	})
	require.NoError(t, err, "failed to create pool")

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

	require.NoError(t, g.Wait(), "error during parallel acquisition")

	rows, err := rootConn.Query(context.Background(), "SELECT datname FROM pg_database WHERE datname LIKE 'testdb_%' AND datname <> 'testdb_template';")
	require.NoError(t, err, "failed to query databases")
	defer rows.Close()

	var datnames []string
	for rows.Next() {
		var datname string
		require.NoError(t, rows.Scan(&datname), "failed to scan database name")
		datnames = append(datnames, datname)
	}
	require.Lenf(t, datnames, maxSize, "expected %d databases, got %d: %v", maxSize, len(datnames), datnames)
	require.NoError(t, rows.Err(), "error iterating over database names")
}

// TestParallelDBPool creates multiple dbpools in parallel to test
// concurrent access and database creation.
func TestParallelDBPool(t *testing.T) {
	g, ctx := errgroup.WithContext(context.Background())

	poolName := "dbpool" // Name of the pool to share across parallel tests
	parallelism := 2     // Number of parallel pools to create

	for i := range parallelism {
		g.Go(func() error {
			rootConn := getRootConnection(t)
			dbpool1, err := New(Config{
				PoolName: poolName,
				Conn:     rootConn,
				MaxSize:  1,
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
			require.NoErrorf(t, err, "[%d] failed to create pool", i)

			t.Logf("[%d] Acquiring pool", i)
			acquired, err := dbpool1.Acquire()
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

	require.NoError(t, g.Wait(), "error during parallel acquisition")
}
