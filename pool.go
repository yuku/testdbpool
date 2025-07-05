package testdbpool

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool manages test database pools
type Pool struct {
	rootConn *pgx.Conn
}

// Acquire creates a new test database and returns a connection pool to it
func (p *Pool) Acquire() (*pgxpool.Pool, error) {
	ctx := context.Background()
	
	// Generate unique database name
	dbName := "testdb_" + uuid.New().String()[:8]
	
	// Create the test database
	_, err := p.rootConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName))
	if err != nil {
		return nil, fmt.Errorf("failed to create test database: %w", err)
	}
	
	// Get connection config from root connection
	config := p.rootConn.Config()
	
	// Build connection string for the new database
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		config.User,
		config.Password,
		config.Host,
		config.Port,
		dbName,
	)
	
	// Add SSL mode if present
	if config.TLSConfig == nil {
		connStr += "?sslmode=disable"
	}
	
	// Create pool for the new database
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		// Clean up database if pool creation fails
		p.rootConn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
		return nil, fmt.Errorf("failed to create pool: %w", err)
	}
	
	return pool, nil
}
