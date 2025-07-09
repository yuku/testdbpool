package testhelper

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
	if err != nil {
		t.Fatalf("failed to create connection pool: %v", err)
	}

	// No need to close the pool - numpool handles cleanup automatically

	return pool
}

// GetTestConn returns a pgx.Conn for testing.
// It uses environment variables for configuration.
func GetTestConn(t *testing.T) *pgx.Conn {
	t.Helper()

	connString := os.Getenv("DATABASE_URL")
	if connString == "" {
		connString = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, connString)
	if err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}

	t.Cleanup(func() {
		_ = conn.Close(ctx)
	})

	return conn
}

