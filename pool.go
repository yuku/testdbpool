package testdbpool

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/puddle/v2"
)

// DatabaseResource represents a pooled database with its connection pool
type DatabaseResource struct {
	dbName string
	pool   *pgxpool.Pool
}

// Pool manages test database pools using puddle for resource management
type Pool struct {
	rootConn        *pgx.Conn
	templateName    string
	templateCreated bool
	setupTemplate   func(context.Context, *pgx.Conn) error
	resetDatabase   func(context.Context, *pgx.Conn) error
	resourcePool    *puddle.Pool[*DatabaseResource]
	templateMu      sync.Mutex // Protects template creation
	rootConnMu      sync.Mutex // Protects rootConn usage
}

func New(config Config) (*Pool, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	pool := &Pool{
		rootConn:      config.Conn,
		templateName:  "testdb_template",
		setupTemplate: config.SetupTemplate,
		resetDatabase: config.ResetDatabase,
	}

	// Clean up previous session before creating puddle pool
	if err := pool.cleanupPreviousSession(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to clean up previous session: %w", err)
	}

	// Create puddle pool configuration
	puddleConfig := &puddle.Config[*DatabaseResource]{
		Constructor: func(ctx context.Context) (*DatabaseResource, error) {
			return pool.createDatabaseResource(ctx)
		},
		Destructor: func(resource *DatabaseResource) {
			pool.destroyDatabaseResource(resource)
		},
		MaxSize: int32(config.maxSize()),
	}

	// Create puddle pool
	puddlePool, err := puddle.NewPool(puddleConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create puddle pool: %w", err)
	}

	pool.resourcePool = puddlePool

	return pool, nil
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

// createDatabaseResource creates a new database and returns a DatabaseResource
func (p *Pool) createDatabaseResource(ctx context.Context) (*DatabaseResource, error) {
	// Ensure template database is created with proper synchronization
	p.templateMu.Lock()
	if !p.templateCreated {
		err := p.createTemplateDatabase(ctx)
		if err != nil {
			p.templateMu.Unlock()
			return nil, fmt.Errorf("failed to create template database: %w", err)
		}
	}
	p.templateMu.Unlock()

	// Generate unique database name
	dbName := "testdb_" + uuid.New().String()[:8]

	// Create the test database from template with connection protection
	p.rootConnMu.Lock()
	_, err := p.rootConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", dbName, p.templateName))
	p.rootConnMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("failed to create test database: %w", err)
	}

	// Get connection config from root connection
	p.rootConnMu.Lock()
	config := p.rootConn.Config()
	p.rootConnMu.Unlock()

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
		p.rootConnMu.Lock()
		p.rootConn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
		p.rootConnMu.Unlock()
		return nil, fmt.Errorf("failed to create pool: %w", err)
	}

	return &DatabaseResource{
		dbName: dbName,
		pool:   pool,
	}, nil
}

// destroyDatabaseResource cleans up a database resource
func (p *Pool) destroyDatabaseResource(resource *DatabaseResource) {
	ctx := context.Background()

	// Close the pool first
	resource.pool.Close()

	// Terminate all connections to the database
	p.rootConnMu.Lock()
	_, err := p.rootConn.Exec(ctx, fmt.Sprintf(`
		SELECT pg_terminate_backend(pid)
		FROM pg_stat_activity
		WHERE datname = '%s' AND pid <> pg_backend_pid()
	`, resource.dbName))
	if err != nil {
		// Ignore errors from terminating backends
	}

	// Drop the database
	_, err = p.rootConn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", resource.dbName))
	p.rootConnMu.Unlock()
	if err != nil {
		// Log but don't fail on cleanup
	}
}

// resetDatabaseResource resets a database to a clean state from template
func (p *Pool) resetDatabaseResource(ctx context.Context, resource *DatabaseResource) error {
	// Get a connection from the pool to execute the reset function
	conn, err := resource.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection for reset: %w", err)
	}
	defer conn.Release()

	// Execute the user-provided reset function
	if err := p.resetDatabase(ctx, conn.Conn()); err != nil {
		return fmt.Errorf("failed to reset database: %w", err)
	}

	return nil
}

// Acquire gets a database from the pool
func (p *Pool) Acquire() (*pgxpool.Pool, error) {
	ctx := context.Background()

	// Acquire a resource from puddle pool
	puddleResource, err := p.resourcePool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire database resource: %w", err)
	}

	resource := puddleResource.Value()

	// Reset the database to ensure clean state
	if err := p.resetDatabaseResource(ctx, resource); err != nil {
		puddleResource.Release()
		return nil, fmt.Errorf("failed to reset database: %w", err)
	}

	// We need to release the puddle resource back to the pool
	// but we're returning the pgxpool.Pool to maintain API compatibility
	// The caller is responsible for the pgxpool, while puddle manages the database lifecycle
	puddleResource.Release()

	return resource.pool, nil
}

// createTemplateDatabase creates a template database and runs the setup function
func (p *Pool) createTemplateDatabase(ctx context.Context) error {
	// Create template database
	p.rootConnMu.Lock()
	_, err := p.rootConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", p.templateName))
	if err != nil {
		p.rootConnMu.Unlock()
		return fmt.Errorf("failed to create template database: %w", err)
	}

	// Connect to template database to run setup
	config := p.rootConn.Config()
	p.rootConnMu.Unlock()
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
		p.rootConnMu.Lock()
		p.rootConn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", p.templateName))
		p.rootConnMu.Unlock()
		return fmt.Errorf("failed to connect to template database: %w", err)
	}
	defer templateConn.Close(ctx)

	// Run setup function
	if err := p.setupTemplate(ctx, templateConn); err != nil {
		// Clean up template database on setup failure
		p.rootConnMu.Lock()
		p.rootConn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", p.templateName))
		p.rootConnMu.Unlock()
		return fmt.Errorf("failed to setup template database: %w", err)
	}

	p.templateCreated = true
	return nil
}

// Close closes the pool and cleans up all resources
func (p *Pool) Close() {
	if p.resourcePool != nil {
		p.resourcePool.Close()
	}
}
