package testdbpool

import (
	"context"
	"fmt"
	"runtime"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuku/numpool"
)

type Pool struct{}

type Config struct {
	// ID is a unique identifier for the TestDBPool instance.
	ID string

	// Pool is the pgxpool.Pool to use for database connections.
	Pool *pgxpool.Pool

	// MaxDatabases is the maximum number of test databases in the pool.
	// Must be between 1 and numpool.MaxResourcesLimit.
	// If not set (0), defaults to min(runtime.GOMAXPROCS(0), numpool.MaxResourcesLimit).
	MaxDatabases int

	// SetupTemplate is called once to set up the template database.
	// The template database is used as a source for creating test databases.
	SetupTemplate func(context.Context, *pgxpool.Pool) error

	// ResetDatabase is called before releasing a test database back to the pool.
	// It should restore the database to a clean state for the next use.
	ResetDatabase func(context.Context, *pgxpool.Pool) error
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("ID is required")
	}

	if c.Pool == nil {
		return fmt.Errorf("pool is required")
	}

	// Apply default for MaxDatabases if not set
	if c.MaxDatabases == 0 {
		gomaxprocs := runtime.GOMAXPROCS(0)
		c.MaxDatabases = min(gomaxprocs, numpool.MaxResourcesLimit)
	}

	if c.MaxDatabases < 1 || c.MaxDatabases > numpool.MaxResourcesLimit {
		return fmt.Errorf("MaxDatabases must be between 1 and %d, got %d", numpool.MaxResourcesLimit, c.MaxDatabases)
	}

	if c.SetupTemplate == nil {
		return fmt.Errorf("SetupTemplate function is required")
	}

	if c.ResetDatabase == nil {
		return fmt.Errorf("ResetDatabase function is required")
	}

	return nil
}

// New creates a new TestDBPool instance with the provided configuration.
func New(ctx context.Context, cfg *Config) (*Pool, error) {
	return nil, fmt.Errorf("not implemented")
}

// Acquire acquires a test database from the pool.
func (p *Pool) Acquire(ctx context.Context) (*TestDB, error) {
	return nil, fmt.Errorf("not implemented")
}
