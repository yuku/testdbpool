package testdbpool

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuku/numpool"
)

// Pool manages a pool of test databases.
type Pool struct {
	config        *Config
	manager       *numpool.Manager
	numpool       *numpool.Numpool
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
	manager, err := numpool.Setup(ctx, config.DBPool)
	if err != nil {
		return nil, fmt.Errorf("failed to setup numpool: %w", err)
	}

	// Create or open numpool
	np, err := manager.GetOrCreate(ctx, numpool.Config{
		ID:                config.PoolID,
		MaxResourcesCount: int32(config.MaxDatabases),
	})
	if err != nil {
		manager.Close()
		return nil, fmt.Errorf("failed to create numpool: %w", err)
	}

	pool := &Pool{
		config:        config,
		manager:       manager,
		numpool:       np,
		templateDB:    fmt.Sprintf("testdb_template_%s", config.PoolID),
		databaseNames: make(map[int]string),
	}

	return pool, nil
}

// Close closes the pool and releases all resources.
func (p *Pool) Close() {
	if p.manager != nil {
		p.manager.Close()
		p.manager = nil
	}
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
	dbPool, err := p.connectToDatabase(ctx, dbName)
	if err != nil {
		_ = resource.Release(ctx)
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	return &TestDB{
		pool:     p,
		resource: resource,
		dbPool:   dbPool,
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
		templatePool, err := p.connectToDatabase(ctx, p.templateDB)
		if err != nil {
			return fmt.Errorf("failed to connect to template database: %w", err)
		}
		defer templatePool.Close()

		// Get a connection for setup
		conn, err := templatePool.Acquire(ctx)
		if err != nil {
			return fmt.Errorf("failed to acquire connection for setup: %w", err)
		}
		defer conn.Release()

		// Run setup function
		if err := p.config.SetupTemplate(ctx, conn.Conn()); err != nil {
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

// connectToDatabase creates a connection pool to a specific database.
func (p *Pool) connectToDatabase(ctx context.Context, dbName string) (*pgxpool.Pool, error) {
	// Get connection config from the main pool
	baseConfig := p.config.DBPool.Config()

	// Create a new config based on the main pool's config
	// We need to build a connection string that ParseConfig can use
	connConfig := baseConfig.ConnConfig

	// Build connection string
	var connString string
	if connConfig.Host == "" {
		// Unix socket connection
		connString = fmt.Sprintf("host=%s port=%d user=%s dbname=%s",
			"/var/run/postgresql",
			connConfig.Port,
			connConfig.User,
			dbName,
		)
	} else {
		// TCP connection
		connString = fmt.Sprintf("host=%s port=%d user=%s dbname=%s",
			connConfig.Host,
			connConfig.Port,
			connConfig.User,
			dbName,
		)
	}

	// Add password if set
	if connConfig.Password != "" {
		connString += fmt.Sprintf(" password=%s", connConfig.Password)
	}

	// Add SSL mode
	if connConfig.TLSConfig != nil {
		connString += " sslmode=require"
	} else {
		connString += " sslmode=disable"
	}

	// Parse config
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Set pool size - most tests don't need many connections
	config.MaxConns = 5
	config.MinConns = 1

	return pgxpool.NewWithConfig(ctx, config)
}

// DropAllDatabases drops all databases in the pool.
// It drops all databases not only created by this pool, but also any that can
// be found in the PostgreSQL instance.
// This is a destructive operation and should be used with caution.
func (p *Pool) DropAllDatabases(ctx context.Context) error {
	// Drop all numbered databases for this pool
	for i := 0; i < p.config.MaxDatabases; i++ {
		dbName := p.getDatabaseName(i)

		// Drop the database if it exists
		_, err := p.config.DBPool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", pgx.Identifier{dbName}.Sanitize()))
		if err != nil {
			return fmt.Errorf("failed to drop database %s: %w", dbName, err)
		}
	}

	// Also drop the template database
	_, err := p.config.DBPool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", pgx.Identifier{p.templateDB}.Sanitize()))
	if err != nil {
		return fmt.Errorf("failed to drop template database %s: %w", p.templateDB, err)
	}

	// Clear the database names map since all databases are dropped
	p.mu.Lock()
	p.databaseNames = make(map[int]string)
	p.mu.Unlock()

	return nil
}
