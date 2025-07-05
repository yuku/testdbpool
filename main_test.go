package testdbpool

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

// getRootConnection returns a connection to the PostgreSQL database.
// The returned connection must have full privileges to create databases and
// manage the pool.
func getRootConnection(t *testing.T) *pgx.Conn {
	t.Helper()

	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		host := getEnvOrDefault("PGHOST", "localhost")
		port := getEnvOrDefault("PGPORT", "5432")
		user := getEnvOrDefault("PGUSER", "postgres")
		password := getEnvOrDefault("PGPASSWORD", "postgres")

		if password != "" {
			connStr = fmt.Sprintf("postgres://%s:%s@%s:%s/postgres?sslmode=disable",
				user, password, host, port)
		} else {
			connStr = fmt.Sprintf("postgres://%s@%s:%s/postgres?sslmode=disable",
				user, host, port)
		}
	}

	conn, err := pgx.Connect(context.Background(), connStr)
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}
	if err := conn.Ping(context.Background()); err != nil {
		t.Fatalf("PostgreSQL not available: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(context.Background()); err != nil {
			t.Errorf("failed to close connection: %v", err)
		}
	})
	return conn
}

// getEnvOrDefault retrieves an environment variable or returns a default value
// if the variable is not set.
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
