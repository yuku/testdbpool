package testdbpool

import (
	"context"
	"fmt"
	"runtime"

	"github.com/jackc/pgx/v5"
)

type Config struct {
	// Conn is the connection to the database with full privileges.
	// This connection is used to create the pool and manage the database.
	Conn *pgx.Conn

	// MaxSize is the maximum number of test databases that can be created.
	// If MaxSize is 0, runtime.GOMAXPROCS(0).
	MaxSize int

	// SetupTemplate is a function that sets up the template database.
	// It is called only once when the pool is created.
	// Each test database is created from this template.
	SetupTemplate func(context.Context, *pgx.Conn) error
}

func (c *Config) Validate() error {
	if c.Conn == nil {
		return fmt.Errorf("RootConnection is required")
	}

	if c.MaxSize < 0 {
		return fmt.Errorf("MaxSize must be non-negative")
	}

	if c.SetupTemplate == nil {
		return fmt.Errorf("SetupTemplate function is required")
	}

	return nil
}

func (c *Config) maxSize() int {
	if c.MaxSize == 0 {
		return runtime.GOMAXPROCS(0)
	}
	return c.MaxSize
}

func New(config Config) (*Pool, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	pool := &Pool{
		rootConn:      config.Conn,
		templateName:  "testdb_template",
		setupTemplate: config.SetupTemplate,
	}
	if err := pool.cleanupPreviousSession(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to clean up previous session: %w", err)
	}

	return pool, nil
}
