// Package pgxpool provides a wrapper around testdbpool to return pgxpool.Pool instances
// instead of *sql.DB, enabling the use of pgx-specific features in tests.
package pgxpool

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/yuku/testdbpool"
)

// Wrapper wraps testdbpool.Pool to provide pgxpool.Pool instances
type Wrapper struct {
	pool *testdbpool.Pool
	// Configuration for connection string building
	config Config
}

// Config holds configuration for the pgxpool wrapper
type Config struct {
	// PasswordSource defines how to obtain the database password
	// If nil, defaults to DefaultPasswordSource
	PasswordSource PasswordSource
	
	// HostSource defines how to obtain the database host
	// If nil, defaults to DefaultHostSource  
	HostSource HostSource
	
	// Additional connection parameters to append to connection string
	// e.g. "sslmode=require&connect_timeout=10"
	AdditionalParams string
}

// PasswordSource is a function that returns the database password
type PasswordSource func() (string, error)

// HostSource is a function that returns host and port
type HostSource func(*sql.DB) (host string, port string, error error)

// New creates a new wrapper around testdbpool.Pool
func New(pool *testdbpool.Pool) *Wrapper {
	return NewWithConfig(pool, Config{})
}

// NewWithConfig creates a new wrapper with custom configuration
func NewWithConfig(pool *testdbpool.Pool, config Config) *Wrapper {
	// Set defaults
	if config.PasswordSource == nil {
		config.PasswordSource = DefaultPasswordSource
	}
	if config.HostSource == nil {
		config.HostSource = DefaultHostSource
	}
	
	return &Wrapper{
		pool:   pool,
		config: config,
	}
}

// Acquire gets a pgxpool.Pool from the test database pool
func (w *Wrapper) Acquire(t *testing.T) (*pgxpool.Pool, error) {
	// Get *sql.DB from testdbpool
	sqlDB, err := w.pool.Acquire(t)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire database from pool: %w", err)
	}

	// Build connection string
	connString, err := w.buildConnectionString(sqlDB)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// Create pgxpool config
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pgxpool config: %w", err)
	}

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
func (w *Wrapper) AcquireWithConfig(t *testing.T, configFunc func(*pgxpool.Config)) (*pgxpool.Pool, error) {
	// Get *sql.DB from testdbpool
	sqlDB, err := w.pool.Acquire(t)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire database from pool: %w", err)
	}

	// Build connection string
	connString, err := w.buildConnectionString(sqlDB)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
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

// AcquireBoth gets both *sql.DB and *pgxpool.Pool from the same test database
// This is useful when you need both interfaces in your tests
func (w *Wrapper) AcquireBoth(t *testing.T) (*sql.DB, *pgxpool.Pool, error) {
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

// buildConnectionString builds a connection string from the sql.DB connection
func (w *Wrapper) buildConnectionString(db *sql.DB) (string, error) {
	// Get database name
	var dbName string
	err := db.QueryRow("SELECT current_database()").Scan(&dbName)
	if err != nil {
		return "", fmt.Errorf("failed to get database name: %w", err)
	}

	// Get host and port
	host, port, err := w.config.HostSource(db)
	if err != nil {
		return "", fmt.Errorf("failed to get host and port: %w", err)
	}

	// Get user
	var user string
	err = db.QueryRow("SELECT current_user").Scan(&user)
	if err != nil {
		return "", fmt.Errorf("failed to get current user: %w", err)
	}

	// Get password
	password, err := w.config.PasswordSource()
	if err != nil {
		return "", fmt.Errorf("failed to get password: %w", err)
	}

	// Build base connection string with proper URL encoding
	var connString string
	if password != "" {
		connString = fmt.Sprintf("postgres://%s:%s@%s:%s/%s", 
			url.QueryEscape(user), 
			url.QueryEscape(password), 
			host, port, dbName)
	} else {
		connString = fmt.Sprintf("postgres://%s@%s:%s/%s", 
			url.QueryEscape(user), 
			host, port, dbName)
	}

	// Add default parameters
	params := []string{"sslmode=disable"}
	
	// Add additional parameters
	if w.config.AdditionalParams != "" {
		params = append(params, w.config.AdditionalParams)
	}

	if len(params) > 0 {
		connString += "?" + strings.Join(params, "&")
	}

	return connString, nil
}

// DefaultPasswordSource tries to get password from common environment variables
func DefaultPasswordSource() (string, error) {
	// Try common environment variable names
	envVars := []string{"DB_PASSWORD", "PGPASSWORD", "POSTGRES_PASSWORD"}
	for _, env := range envVars {
		if pass := os.Getenv(env); pass != "" {
			return pass, nil
		}
	}
	return "", nil
}

// DefaultHostSource tries to get host and port from the database connection
func DefaultHostSource(db *sql.DB) (host string, port string, error error) {
	// Try to get from PostgreSQL functions
	err := db.QueryRow(`
		SELECT 
			COALESCE(host(inet_server_addr()), 'localhost'),
			COALESCE(inet_server_port()::text, '5432')
	`).Scan(&host, &port)
	
	if err != nil {
		// Fallback to environment variables
		host = os.Getenv("DB_HOST")
		if host == "" {
			host = os.Getenv("PGHOST")
			if host == "" {
				host = "localhost"
			}
		}
		
		port = os.Getenv("DB_PORT")
		if port == "" {
			port = os.Getenv("PGPORT")
			if port == "" {
				port = "5432"
			}
		}
		
		return host, port, nil
	}
	
	return host, port, nil
}

// EnvPasswordSource creates a PasswordSource that reads from a specific environment variable
func EnvPasswordSource(envVar string) PasswordSource {
	return func() (string, error) {
		password := os.Getenv(envVar)
		if password == "" {
			return "", fmt.Errorf("environment variable %s is not set", envVar)
		}
		return password, nil
	}
}

// StaticPasswordSource creates a PasswordSource that returns a fixed password
func StaticPasswordSource(password string) PasswordSource {
	return func() (string, error) {
		return password, nil
	}
}

// EnvHostSource creates a HostSource that reads from specific environment variables
func EnvHostSource(hostVar, portVar string) HostSource {
	return func(*sql.DB) (string, string, error) {
		host := os.Getenv(hostVar)
		if host == "" {
			return "", "", fmt.Errorf("environment variable %s is not set", hostVar)
		}
		
		port := os.Getenv(portVar)
		if port == "" {
			port = "5432" // Default PostgreSQL port
		}
		
		return host, port, nil
	}
}