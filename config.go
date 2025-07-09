package testdbpool

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Config holds the configuration for creating a test database pool.
type Config struct {
	// PoolID is the unique identifier for this test database pool.
	// Multiple pools can coexist with different IDs.
	PoolID string

	// DBPool is the PostgreSQL connection pool used for database operations.
	DBPool *pgxpool.Pool

	// MaxDatabases is the maximum number of test databases in the pool.
	// Must be between 1 and 64 (limited by numpool's bitmap implementation).
	MaxDatabases int

	// SetupTemplate is called once to set up the template database.
	// The template database is used as a source for creating test databases.
	SetupTemplate func(ctx context.Context, conn *pgx.Conn) error

	// ResetDatabase is called before releasing a test database back to the pool.
	// It should restore the database to a clean state for the next use.
	ResetDatabase func(ctx context.Context, conn *pgx.Conn) error
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.PoolID == "" {
		return fmt.Errorf("PoolID is required")
	}

	if c.DBPool == nil {
		return fmt.Errorf("DBPool is required")
	}

	if c.MaxDatabases < 1 || c.MaxDatabases > 64 {
		return fmt.Errorf("MaxDatabases must be between 1 and 64, got %d", c.MaxDatabases)
	}

	if c.SetupTemplate == nil {
		return fmt.Errorf("SetupTemplate function is required")
	}

	if c.ResetDatabase == nil {
		return fmt.Errorf("ResetDatabase function is required")
	}

	return nil
}
