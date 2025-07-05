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
	maxSize         int
	databases       []string // Track created database names
	currentIndex    int      // Index for round-robin reuse
}

// cleanupPreviousSession cleans up the template database and test databases
// created in the previous run. This method is executed after the pool is
// initialized and before acquiring a new test database.
// The reason why cleanup is not done after each test end is to keep the
// databases available for debugging purposes.
func (p *Pool) cleanupPreviousSession(ctx context.Context) error {
	// Drop all test databases from previous sessions
	_, err := p.rootConn.Exec(ctx, `
		SELECT pg_terminate_backend(pid)
		FROM pg_stat_activity
		WHERE datname LIKE 'testdb_%'
			AND pid <> pg_backend_pid()
	`)
	if err != nil {
		// Ignore errors from terminating backends as they might not exist
	}

	// Drop all databases that match our naming pattern
	rows, err := p.rootConn.Query(ctx, `
		SELECT datname FROM pg_database WHERE datname LIKE 'testdb_%'
	`)
	if err != nil {
		return fmt.Errorf("failed to list test databases: %w", err)
	}
	defer rows.Close()

	var databases []string
	for rows.Next() {
		var dbName string
		if err := rows.Scan(&dbName); err != nil {
			return fmt.Errorf("failed to scan database name: %w", err)
		}
		databases = append(databases, dbName)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to iterate over databases: %w", err)
	}

	// Drop each database
	for _, dbName := range databases {
		_, err = p.rootConn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
		if err != nil {
			// Log but don't fail on individual database drops
			// as they might be in use or already dropped
		}
	}

	return nil
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

	var dbName string

	// Check if we've reached maxSize
	if len(p.databases) >= p.maxSize {
		// Reuse existing database in round-robin fashion
		dbName = p.databases[p.currentIndex]
		p.currentIndex = (p.currentIndex + 1) % p.maxSize

		// Reset the database by dropping and recreating it from template
		if err := p.resetDatabase(ctx, dbName); err != nil {
			return nil, fmt.Errorf("failed to reset database %s: %w", dbName, err)
		}
	} else {
		// Create new database if under maxSize
		dbName = "testdb_" + uuid.New().String()[:8]

		// Create the test database from template
		_, err := p.rootConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", dbName, p.templateName))
		if err != nil {
			return nil, fmt.Errorf("failed to create test database: %w", err)
		}

		// Track the new database
		p.databases = append(p.databases, dbName)
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
		// Clean up database if pool creation fails (only if newly created)
		if len(p.databases) < p.maxSize {
			p.rootConn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
			p.databases = p.databases[:len(p.databases)-1]
		}
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

// resetDatabase drops and recreates a database from the template
func (p *Pool) resetDatabase(ctx context.Context, dbName string) error {
	// Terminate all connections to the database
	_, err := p.rootConn.Exec(ctx, fmt.Sprintf(`
		SELECT pg_terminate_backend(pid)
		FROM pg_stat_activity
		WHERE datname = '%s' AND pid <> pg_backend_pid()
	`, dbName))
	if err != nil {
		// Ignore errors from terminating backends
	}

	// Drop the database
	_, err = p.rootConn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	if err != nil {
		return fmt.Errorf("failed to drop database: %w", err)
	}

	// Recreate from template
	_, err = p.rootConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", dbName, p.templateName))
	if err != nil {
		return fmt.Errorf("failed to recreate database: %w", err)
	}

	return nil
}
