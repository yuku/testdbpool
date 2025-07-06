package multipkgs

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/yuku/testdbpool"
	"github.com/yuku/testdbpool/internal"
	"golang.org/x/sync/errgroup"
)

// shared pool name for sub-packages to use
const poolName = "testdbpool_multi_package"

func RunTest(t *testing.T) {
	rootConn := internal.GetRootConnection(t)

	maxSize := 1
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
			count, err := internal.CountTable(ctx, acquired.Pool, "enum_values")
			if err != nil {
				return fmt.Errorf("[%d] failed to count rows in enum_values: %w", i, err)
			}
			if count != 3 {
				return fmt.Errorf("[%d] expected 3 rows in enum_values, got %d", i, count)
			}

			t.Logf("[%d] Counting rows in entities", i)
			count, err = internal.CountTable(ctx, acquired.Pool, "entities")
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
			count, err = internal.CountTable(ctx, acquired.Pool, "entities")
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
	require.Lenf(t, datnames, maxSize, "expected %d databases, got %d: %v", maxSize, len(datnames), datnames)
	require.NoError(t, rows.Err(), "error iterating over database names")
}
