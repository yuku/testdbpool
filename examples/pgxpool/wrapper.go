package pgxpool

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/yuku/testdbpool"
)

// PoolWrapper wraps testdbpool.Pool to provide pgxpool.Pool instances
type PoolWrapper struct {
	pool *testdbpool.Pool
}

// NewPoolWrapper creates a new wrapper around testdbpool.Pool
func NewPoolWrapper(pool *testdbpool.Pool) *PoolWrapper {
	return &PoolWrapper{pool: pool}
}

// Acquire gets a pgxpool.Pool from the test database pool
func (w *PoolWrapper) Acquire(t *testing.T) (*pgxpool.Pool, error) {
	// Get *sql.DB from testdbpool
	sqlDB, err := w.pool.Acquire(t)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire database from pool: %w", err)
	}

	// Get database name from the connection
	var dbName string
	err = sqlDB.QueryRow("SELECT current_database()").Scan(&dbName)
	if err != nil {
		return nil, fmt.Errorf("failed to get database name: %w", err)
	}

	// Get connection parameters
	var host, port, user string
	err = sqlDB.QueryRow(`
		SELECT 
			host(inet_server_addr()),
			inet_server_port()::text,
			current_user
	`).Scan(&host, &port, &user)
	if err != nil {
		// Fallback to localhost if server_addr is NULL (common in Docker)
		host = "localhost"
		port = "5432"
		sqlDB.QueryRow("SELECT current_user").Scan(&user)
	}

	// Build connection string for pgxpool
	// Note: This assumes password authentication is handled via environment variables
	// or .pgpass file since we can't extract password from existing connection
	connString := fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=disable", user, host, port, dbName)

	// If password is available via environment variable, use it
	if pass := getPasswordFromEnv(); pass != "" {
		connString = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, pass, host, port, dbName)
	}

	// Create pgxpool config
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pgxpool config: %w", err)
	}

	// Configure pool settings
	config.MaxConns = 10
	config.MinConns = 2

	// Create pgxpool
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create pgxpool: %w", err)
	}

	// Register cleanup to close pgxpool when test ends
	t.Cleanup(func() {
		pool.Close()
	})

	return pool, nil
}

// AcquireWithConfig gets a pgxpool.Pool with custom configuration
func (w *PoolWrapper) AcquireWithConfig(t *testing.T, configFunc func(*pgxpool.Config)) (*pgxpool.Pool, error) {
	// Get *sql.DB from testdbpool
	sqlDB, err := w.pool.Acquire(t)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire database from pool: %w", err)
	}

	// Get database name from the connection
	var dbName string
	err = sqlDB.QueryRow("SELECT current_database()").Scan(&dbName)
	if err != nil {
		return nil, fmt.Errorf("failed to get database name: %w", err)
	}

	// Get connection parameters
	var host, port, user string
	err = sqlDB.QueryRow(`
		SELECT 
			host(inet_server_addr()),
			inet_server_port()::text,
			current_user
	`).Scan(&host, &port, &user)
	if err != nil {
		// Fallback to localhost if server_addr is NULL
		host = "localhost"
		port = "5432"
		sqlDB.QueryRow("SELECT current_user").Scan(&user)
	}

	// Build connection string
	connString := fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=disable", user, host, port, dbName)
	if pass := getPasswordFromEnv(); pass != "" {
		connString = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, pass, host, port, dbName)
	}

	// Create pgxpool config
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pgxpool config: %w", err)
	}

	// Apply custom configuration
	if configFunc != nil {
		configFunc(config)
	}

	// Create pgxpool
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create pgxpool: %w", err)
	}

	// Register cleanup
	t.Cleanup(func() {
		pool.Close()
	})

	return pool, nil
}

// AcquireSQLDB gets both *sql.DB and *pgxpool.Pool from the same test database
// This is useful when you need both interfaces in your tests
func (w *PoolWrapper) AcquireBoth(t *testing.T) (*sql.DB, *pgxpool.Pool, error) {
	// Get pgxpool first
	pgxPool, err := w.Acquire(t)
	if err != nil {
		return nil, nil, err
	}

	// Convert pgxpool to *sql.DB using stdlib
	sqlDB := stdlib.OpenDBFromPool(pgxPool)

	// Register cleanup for sql.DB
	t.Cleanup(func() {
		sqlDB.Close()
	})

	return sqlDB, pgxPool, nil
}

// getPasswordFromEnv tries to get password from environment variables
func getPasswordFromEnv() string {
	// Try common environment variable names
	envVars := []string{"DB_PASSWORD", "PGPASSWORD", "POSTGRES_PASSWORD"}
	for _, env := range envVars {
		if pass := os.Getenv(env); pass != "" {
			return pass
		}
	}
	return ""
}

// Helper function to get environment variable with fallback
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}