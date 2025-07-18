package testhelper

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// GetTestDBPool returns a pgxpool.Pool for testing.
// It uses environment variables for configuration.
func GetTestDBPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	connString := os.Getenv("DATABASE_URL")
	if connString == "" {
		connString = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err, "failed to create connection pool")
	t.Cleanup(pool.Close)

	return pool
}
