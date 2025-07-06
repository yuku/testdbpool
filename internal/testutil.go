package internal

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GetRootConnection returns a connection to the PostgreSQL database.
// The returned connection must have full privileges to create databases and
// manage the pool.
func GetRootConnection(t *testing.T) *pgx.Conn {
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

// GetRootConnectionNoCleanup returns a connection to the PostgreSQL database
// without registering automatic cleanup. The caller is responsible for closing
// the connection.
func GetRootConnectionNoCleanup(t *testing.T) (*pgx.Conn, error) {
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
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	if err := conn.Ping(context.Background()); err != nil {
		conn.Close(context.Background())
		return nil, fmt.Errorf("PostgreSQL not available: %w", err)
	}
	return conn, nil
}

// CountTable counts the number of rows in the specified table.
func CountTable(ctx context.Context, conn *pgxpool.Pool, tableName string) (int, error) {
	var count int
	err := conn.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count rows in %s: %w", tableName, err)
	}
	return count, nil
}
