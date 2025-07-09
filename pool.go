package testdbpool

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/yuku/numpool"
)

// Pool manages a pool of test databases.
type Pool struct {
	config        *Config
	numpool       *numpool.Pool
	templateDB    string
	databaseNames map[int]string // maps resource index to database name
	mu            sync.RWMutex   // protects databaseNames
	setupOnce     sync.Once
	setupErr      error
}

// New creates a new test database pool.
func New(ctx context.Context, config *Config) (*Pool, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Setup numpool database if needed
	conn, err := config.DBPool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	if err := numpool.Setup(ctx, conn.Conn()); err != nil {
		return nil, fmt.Errorf("failed to setup numpool: %w", err)
	}

	// Create or open numpool
	np, err := numpool.CreateOrOpen(ctx, numpool.Config{
		Pool:              config.DBPool,
		ID:                config.PoolID,
		MaxResourcesCount: int32(config.MaxDatabases),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create numpool: %w", err)
	}

	pool := &Pool{
		config:        config,
		numpool:       np,
		templateDB:    fmt.Sprintf("testdb_template_%s", config.PoolID),
		databaseNames: make(map[int]string),
	}

	return pool, nil
}

// Acquire obtains a test database from the pool.
func (p *Pool) Acquire(ctx context.Context) (*TestDB, error) {
	// Ensure template database is set up
	p.setupOnce.Do(func() {
		p.setupErr = p.setupTemplateDatabase(ctx)
	})
	if p.setupErr != nil {
		return nil, fmt.Errorf("template database setup failed: %w", p.setupErr)
	}

	// Acquire resource from numpool
	resource, err := p.numpool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire resource: %w", err)
	}

	// Get or create database for this index
	dbName := p.getDatabaseName(resource.Index())

	// Ensure database exists
	if err := p.ensureDatabaseExists(ctx, dbName); err != nil {
		_ = resource.Release(ctx)
		return nil, fmt.Errorf("failed to ensure database exists: %w", err)
	}

	// Connect to the database
	conn, err := p.connectToDatabase(ctx, dbName)
	if err != nil {
		_ = resource.Release(ctx)
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	return &TestDB{
		pool:     p,
		resource: resource,
		conn:     conn,
		dbName:   dbName,
	}, nil
}

// setupTemplateDatabase creates and initializes the template database.
func (p *Pool) setupTemplateDatabase(ctx context.Context) error {
	conn, err := p.config.DBPool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	// Create template database if it doesn't exist
	var exists bool
	err = conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", p.templateDB).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check template database: %w", err)
	}

	if !exists {
		// Create template database
		_, err = conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", pgx.Identifier{p.templateDB}.Sanitize()))
		if err != nil {
			return fmt.Errorf("failed to create template database: %w", err)
		}

		// Connect to template database to set it up
		templateConn, err := p.connectToDatabase(ctx, p.templateDB)
		if err != nil {
			return fmt.Errorf("failed to connect to template database: %w", err)
		}
		defer func() { _ = templateConn.Close(ctx) }()

		// Run setup function
		if err := p.config.SetupTemplate(ctx, templateConn); err != nil {
			return fmt.Errorf("failed to setup template database: %w", err)
		}
	}

	return nil
}

// getDatabaseName returns the database name for a given resource index.
func (p *Pool) getDatabaseName(index int) string {
	p.mu.RLock()
	name, exists := p.databaseNames[index]
	p.mu.RUnlock()

	if exists {
		return name
	}

	// Generate new name
	name = fmt.Sprintf("testdb_%s_%d", p.config.PoolID, index)

	p.mu.Lock()
	p.databaseNames[index] = name
	p.mu.Unlock()

	return name
}

// ensureDatabaseExists creates the database if it doesn't exist.
func (p *Pool) ensureDatabaseExists(ctx context.Context, dbName string) error {
	conn, err := p.config.DBPool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	// Check if database exists
	var exists bool
	err = conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check database: %w", err)
	}

	if !exists {
		// Create database from template
		_, err = conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s",
			pgx.Identifier{dbName}.Sanitize(),
			pgx.Identifier{p.templateDB}.Sanitize()))
		if err != nil {
			return fmt.Errorf("failed to create database: %w", err)
		}
	}

	return nil
}

// connectToDatabase creates a connection to a specific database.
func (p *Pool) connectToDatabase(ctx context.Context, dbName string) (*pgx.Conn, error) {
	// Get connection config from pool
	config := p.config.DBPool.Config().ConnConfig.Copy()
	config.Database = dbName

	return pgx.ConnectConfig(ctx, config)
}
