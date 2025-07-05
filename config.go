package testdbpool

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type Config struct {
	// Conn is the connection to the database with full privileges.
	// This connection is used to create the pool and manage the database.
	Conn *pgx.Conn

	// SetupTemplate is a function that sets up the template database.
	// It is called only once when the pool is created.
	// Each test database is created from this template.
	SetupTemplate func(context.Context, *pgx.Conn) error
}

func (c *Config) Validate() error {
	if c.Conn == nil {
		return fmt.Errorf("RootConnection is required")
	}

	if c.SetupTemplate == nil {
		return fmt.Errorf("SetupTemplate function is required")
	}

	return nil
}

func New(config Config) (*Pool, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	pool := &Pool{
		rootConn: config.Conn,
	}

	return pool, nil
}
