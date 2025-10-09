package testutil

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/yuku/numpool"
)

// GetTestDBPool returns a pgxpool.Pool for testing.
// It uses environment variables for configuration.
func GetTestDBPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	connString := os.Getenv("DATABASE_URL")
	if connString == "" {
		connString = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
	}

	pool, err := pgxpool.New(context.Background(), connString)
	require.NoError(t, err, "failed to create connection pool")
	t.Cleanup(pool.Close)

	return pool
}

func CleanupNumpool(pool *pgxpool.Pool) func() {
	return func() {
		_ = numpool.Cleanup(context.Background(), pool)
	}
}

func DBExists(t *testing.T, pool *pgxpool.Pool, dbName string) bool {
	t.Helper()
	var exists bool
	err := pool.
		QueryRow(context.Background(), "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName).
		Scan(&exists)
	require.NoError(t, err)
	return exists
}

// CreateTemplateDatabase creates a template database for testing.
// The poolID should match the ID used in testdbpool.Config.
// The setup function is called to initialize the schema in the template database.
func CreateTemplateDatabase(t *testing.T, pool *pgxpool.Pool, poolID string, setup func(context.Context, *pgx.Conn) error) {
	t.Helper()
	ctx := context.Background()

	templateName := fmt.Sprintf("testdbpooltmpl_%s", poolID)

	// Drop template database if it exists (cleanup from previous test runs)
	_, _ = pool.Exec(ctx, fmt.Sprintf(
		"ALTER DATABASE %s IS_TEMPLATE false",
		pgx.Identifier{templateName}.Sanitize(),
	))
	_, _ = pool.Exec(ctx, fmt.Sprintf(
		"DROP DATABASE IF EXISTS %s",
		pgx.Identifier{templateName}.Sanitize(),
	))

	// Create template database
	_, err := pool.Exec(ctx, fmt.Sprintf(
		"CREATE DATABASE %s IS_TEMPLATE true",
		pgx.Identifier{templateName}.Sanitize(),
	))
	require.NoError(t, err, "failed to create template database")

	// Connect to template database
	cfg := pool.Config().ConnConfig.Copy()
	cfg.Database = templateName
	conn, err := pgx.ConnectConfig(ctx, cfg)
	require.NoError(t, err, "failed to connect to template database")
	defer func() {
		_ = conn.Close(ctx)
	}()

	// Run setup function
	err = setup(ctx, conn)
	require.NoError(t, err, "failed to setup template database")
}

// CreateTemplateDatabaseWithOwner creates a template database with a specific owner for testing.
func CreateTemplateDatabaseWithOwner(t *testing.T, pool *pgxpool.Pool, poolID string, owner string, setup func(context.Context, *pgx.Conn) error) {
	t.Helper()
	ctx := context.Background()

	templateName := fmt.Sprintf("testdbpooltmpl_%s", poolID)

	// Drop template database if it exists (cleanup from previous test runs)
	_, _ = pool.Exec(ctx, fmt.Sprintf(
		"ALTER DATABASE %s IS_TEMPLATE false",
		pgx.Identifier{templateName}.Sanitize(),
	))
	_, _ = pool.Exec(ctx, fmt.Sprintf(
		"DROP DATABASE IF EXISTS %s",
		pgx.Identifier{templateName}.Sanitize(),
	))

	// Create template database with owner
	_, err := pool.Exec(ctx, fmt.Sprintf(
		"CREATE DATABASE %s OWNER %s IS_TEMPLATE true",
		pgx.Identifier{templateName}.Sanitize(),
		pgx.Identifier{owner}.Sanitize(),
	))
	require.NoError(t, err, "failed to create template database with owner")

	// Connect to template database
	cfg := pool.Config().ConnConfig.Copy()
	cfg.Database = templateName
	conn, err := pgx.ConnectConfig(ctx, cfg)
	require.NoError(t, err, "failed to connect to template database")
	defer func() {
		_ = conn.Close(ctx)
	}()

	// Run setup function
	err = setup(ctx, conn)
	require.NoError(t, err, "failed to setup template database")
}
