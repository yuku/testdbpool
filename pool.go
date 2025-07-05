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
	rootConn        *pgx.Conn
	templateName    string
	templateCreated bool
	setupTemplate   func(context.Context, *pgx.Conn) error
}

// cleanupPreviousSession cleans up the template database and test databases
// created in the previous run. This method is executed after the pool is
// initialized and before acquiring a new test database.
// The reason why cleanup is not done after each test end is to keep the
// databases available for debugging purposes.
func (p *Pool) cleanupPreviousSession(ctx context.Context) error {
	return fmt.Errorf("not implemented")
}

// Acquire creates a new test database and returns a connection pool to it
func (p *Pool) Acquire() (*pgxpool.Pool, error) {
	ctx := context.Background()

	// Ensure template database is created
	if !p.templateCreated {
		if err := p.createTemplateDatabase(ctx); err != nil {
			return nil, fmt.Errorf("failed to create template database: %w", err)
		}
	}

	// Generate unique database name
	dbName := "testdb_" + uuid.New().String()[:8]

	// Create the test database from template
	_, err := p.rootConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", dbName, p.templateName))
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

// createTemplateDatabase creates a template database and runs the setup function
func (p *Pool) createTemplateDatabase(ctx context.Context) error {
	// Create template database
	_, err := p.rootConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", p.templateName))
	if err != nil {
		return fmt.Errorf("failed to create template database: %w", err)
	}

	// Connect to template database to run setup
	config := p.rootConn.Config()
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		config.User,
		config.Password,
		config.Host,
		config.Port,
		p.templateName,
	)

	if config.TLSConfig == nil {
		connStr += "?sslmode=disable"
	}

	templateConn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		// Clean up template database on connection failure
		p.rootConn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", p.templateName))
		return fmt.Errorf("failed to connect to template database: %w", err)
	}
	defer templateConn.Close(ctx)

	// Run setup function
	if err := p.setupTemplate(ctx, templateConn); err != nil {
		// Clean up template database on setup failure
		p.rootConn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", p.templateName))
		return fmt.Errorf("failed to setup template database: %w", err)
	}

	p.templateCreated = true
	return nil
}
