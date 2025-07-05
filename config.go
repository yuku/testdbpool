package testdbpool

import (
	"context"
	"fmt"
	"runtime"

	"github.com/jackc/pgx/v5"
)

type Config struct {
	// PoolName is the name of the test database pool.
	// Pool instances with the same PoolName runs in parallel sharing the same
	// template database and test databases.
	PoolName string

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

	// ResetDatabase is a function that resets the database to its initial state.
	// It is called when the testdb pool is reused.
	// After this function is called, the database should be in the same state
	// as it was after the SetupTemplate function was called.
	ResetDatabase func(context.Context, *pgx.Conn) error
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

	if c.ResetDatabase == nil {
		return fmt.Errorf("ResetDatabase function is required")
	}

	return nil
}

func (c *Config) maxSize() int {
	if c.MaxSize == 0 {
		return runtime.GOMAXPROCS(0)
	}
	return c.MaxSize
}
