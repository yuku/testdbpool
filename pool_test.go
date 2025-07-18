package testdbpool_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/yuku/testdbpool"
	"github.com/yuku/testdbpool/internal/testhelper"
)

func TestNew(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	pool := testhelper.GetTestDBPool(t)

	t.Run("ValidConfig", func(t *testing.T) {
		config := &testdbpool.Config{
			PoolID:       "test-new-valid",
			DBPool:       pool,
			MaxDatabases: 2,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				return nil
			},
			ResetDatabase: func(ctx context.Context, conn *pgx.Conn) error {
				return nil
			},
		}

		testPool, err := testdbpool.New(ctx, config)
		require.NoError(t, err, "failed to create test database pool")

		// Verify pool was created
		require.Same(t, testPool.Config(), config, "expected same config")
		require.NotNil(t, testPool.Numpool(), "expected numpool to be initialized")
		require.NotEmpty(t, testPool.TemplateDB(), "expected templateDB to be set")
		require.NotNil(t, testPool.DatabaseNames(), "expected databaseNames map to be initialized")
	})

	t.Run("returns error if nil is given", func(t *testing.T) {
		_, err := testdbpool.New(ctx, nil)
		require.Error(t, err, "expected error for nil config")
	})

	t.Run("returns error if invalid config is given", func(t *testing.T) {
		invalidConfig := &testdbpool.Config{
			PoolID: "test-new-invalid",
			// Missing required fields
		}
		_, err := testdbpool.New(ctx, invalidConfig)
		require.Error(t, err, "expected error for invalid config")
	})

	t.Run("TemplateNaming", func(t *testing.T) {
		poolID := "test-template-name"
		config := &testdbpool.Config{
			PoolID:       poolID,
			DBPool:       pool,
			MaxDatabases: 1,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				return nil
			},
			ResetDatabase: func(ctx context.Context, conn *pgx.Conn) error {
				return nil
			},
		}

		testPool, err := testdbpool.New(ctx, config)
		require.NoError(t, err, "failed to create test database pool")
		require.Equal(t, "testdb_template_"+poolID, testPool.TemplateDB())
	})
}

func TestPool_DropAllDatabases(t *testing.T) {
	ctx := context.Background()
	pool := testhelper.GetTestDBPool(t)

	testPool, err := testdbpool.New(ctx, &testdbpool.Config{
		PoolID:       "test-drop-all",
		DBPool:       pool,
		MaxDatabases: 5,
		SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
			return nil // no setup needed for this test
		},
		ResetDatabase: func(ctx context.Context, conn *pgx.Conn) error {
			return nil // no reset needed for this test
		},
	})
	require.NoError(t, err, "failed to create test database pool")

	// Drop template database if it exists
	_, err = pool.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %s`, pgx.Identifier{testPool.TemplateDB()}.Sanitize()))
	require.NoError(t, err, "failed to drop template database")

	// Ensure the template database is created
	_, err = pool.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s`, pgx.Identifier{testPool.TemplateDB()}.Sanitize()))
	require.NoError(t, err, "failed to create template database")

	// Create test db 0, 1 and 2
	for i := range 5 {
		dbname := testPool.GetDatabaseName(i)

		// Drop existing database if it exists for robustness
		_, err := pool.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %s`, pgx.Identifier{dbname}.Sanitize()))
		require.NoErrorf(t, err, "failed to drop existing test database %d", i)

		if i < 3 {
			// Create new database. Postgres does not has create if not exists for databases.
			_, err = pool.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s`, pgx.Identifier{dbname}.Sanitize()))
			require.NoErrorf(t, err, "failed to create test database %d", i)
		}

		// Check that the databases exist (use raw name for datname comparison)
		row := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbname)
		var exists bool
		err = row.Scan(&exists)
		require.NoError(t, err, "failed to check database existence")
		if i < 3 {
			require.True(t, exists, "database %s should exist", dbname)
		} else {
			require.False(t, exists, "database %s should not exist", dbname)
		}
	}

	// Now drop all databases
	err = testPool.DropAllDatabases(ctx)
	require.NoError(t, err, "failed to drop all databases")

	// Verify all databases are dropped
	row := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", testPool.GetTemplateDBName())
	var exists bool
	err = row.Scan(&exists)
	require.NoError(t, err, "failed to check template database existence")
	require.False(t, exists, "template database %s should not exist after drop", testPool.GetTemplateDBName())
	for i := range 5 {
		dbname := testPool.GetDatabaseName(i)
		row := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbname)
		var exists bool
		err = row.Scan(&exists)
		require.NoError(t, err, "failed to check database existence after drop")
		require.False(t, exists, "database %s should not exist after drop", dbname)
	}
}
