package testutil

import (
	"context"
	"os"
	"testing"

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
