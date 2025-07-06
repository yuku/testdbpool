package testdbpool

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/puddle/v2"
)

// Global registry for shared pools
var (
	sharedPools   = make(map[string]*sharedPool)
	sharedPoolsMu sync.Mutex
)

// sharedPool represents a pool that can be shared across multiple Pool instances
type sharedPool struct {
	resourcePool    *puddle.Pool[*DatabaseResource]
	templateName    string
	templateCreated bool
	setupTemplate   func(context.Context, *pgx.Conn) error
	resetDatabase   func(context.Context, *pgx.Conn) error
	templateMu      sync.Mutex
	refCount        int
}

// DatabaseResource represents a pooled database with its connection pool
type DatabaseResource struct {
	dbName string
	pool   *pgxpool.Pool
	sqlDB  *sql.DB // Optional SQL DB handle for compatibility
}

// AcquiredDatabase represents an acquired database that must be released
type AcquiredDatabase struct {
	Pool           *pgxpool.Pool
	puddleResource *puddle.Resource[*DatabaseResource]
}

// Release returns the database back to the pool
func (ad *AcquiredDatabase) Release() {
	ad.puddleResource.Release()
}

// Pool manages test database pools using puddle for resource management
type Pool struct {
	rootConn   *pgx.Conn
	poolName   string
	shared     *sharedPool // Reference to shared pool if poolName is set
	rootConnMu sync.Mutex  // Protects rootConn usage
	// The following fields are only used when pool is not shared (poolName is empty)
	templateName    string
	templateCreated bool
	setupTemplate   func(context.Context, *pgx.Conn) error
	resetDatabase   func(context.Context, *pgx.Conn) error
	resourcePool    *puddle.Pool[*DatabaseResource]
	templateMu      sync.Mutex // Protects template creation
}

func New(config Config) (*Pool, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// If PoolName is specified, use shared pool
	if config.PoolName != "" {
		return newSharedPool(config)
	}

	// Create a non-shared pool
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

// newSharedPool creates or gets a shared pool based on PoolName
func newSharedPool(config Config) (*Pool, error) {
	ctx := context.Background()
	
	// Ensure database tables exist
	if err := ensureTablesExist(config.Conn); err != nil {
		return nil, fmt.Errorf("failed to ensure tables exist: %w", err)
	}

	// Register pool in database
	templateName := "testdb_template_" + config.PoolName
	if err := registerPoolInDB(config.Conn, config.PoolName, templateName, config.maxSize()); err != nil {
		return nil, fmt.Errorf("failed to register pool: %w", err)
	}

	// Check if we have this pool in memory (for same process)
	sharedPoolsMu.Lock()
	if shared, exists := sharedPools[config.PoolName]; exists {
		// Increment reference count for in-process tracking
		shared.refCount++
		sharedPoolsMu.Unlock()
		return &Pool{
			rootConn: config.Conn,
			poolName: config.PoolName,
			shared:   shared,
		}, nil
	}
	sharedPoolsMu.Unlock()

	// Create new shared pool
	shared := &sharedPool{
		templateName:  templateName,
		setupTemplate: config.SetupTemplate,
		resetDatabase: config.ResetDatabase,
		refCount:      1,
	}

	// Create pool wrapper
	pool := &Pool{
		rootConn: config.Conn,
		poolName: config.PoolName,
		shared:   shared,
	}

	// Setup template if needed
	shared.templateMu.Lock()
	if !shared.templateCreated {
		err := pool.createTemplateDatabase(ctx)
		if err != nil {
			shared.templateMu.Unlock()
			return nil, fmt.Errorf("failed to create template database: %w", err)
		}
		shared.templateCreated = true
	}
	shared.templateMu.Unlock()

	// Clean up dead processes
	if _, err := cleanupDeadProcesses(config.Conn); err != nil {
		// Log error but don't fail - cleanup is best effort
		fmt.Printf("Warning: failed to cleanup dead processes: %v\n", err)
	}

	// Create puddle pool configuration with DB-aware constructor
	puddleConfig := &puddle.Config[*DatabaseResource]{
		Constructor: func(ctx context.Context) (*DatabaseResource, error) {
			return pool.createDatabaseResourceWithDB(ctx)
		},
		Destructor: func(resource *DatabaseResource) {
			pool.destroyDatabaseResourceWithDB(resource)
		},
		MaxSize: int32(config.maxSize()),
	}

	// Create puddle pool
	puddlePool, err := puddle.NewPool(puddleConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create puddle pool: %w", err)
	}

	shared.resourcePool = puddlePool
	
	// Store in memory for this process
	sharedPoolsMu.Lock()
	sharedPools[config.PoolName] = shared
	sharedPoolsMu.Unlock()

	return pool, nil
}

// createRootConnection creates a new connection to the root database
func (p *Pool) createRootConnection() (*pgx.Conn, error) {
	config := p.rootConn.Config()
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		config.User,
		config.Password,
		config.Host,
		config.Port,
		config.Database,
	)
	
	// Add SSL mode if present
	if config.TLSConfig == nil {
		connStr += "?sslmode=disable"
	}
	
	return pgx.Connect(context.Background(), connStr)
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
	// Use shared pool fields if available
	var templateMu *sync.Mutex
	var templateCreated *bool
	var templateName string

	if p.shared != nil {
		templateMu = &p.shared.templateMu
		templateCreated = &p.shared.templateCreated
		templateName = p.shared.templateName
	} else {
		templateMu = &p.templateMu
		templateCreated = &p.templateCreated
		templateName = p.templateName
	}

	// Ensure template database is created with proper synchronization
	templateMu.Lock()
	if !*templateCreated {
		err := p.createTemplateDatabase(ctx)
		if err != nil {
			templateMu.Unlock()
			return nil, fmt.Errorf("failed to create template database: %w", err)
		}
	}
	templateMu.Unlock()

	// Generate unique database name
	dbName := "testdb_" + uuid.New().String()[:8]

	// Create the test database from template with connection protection
	p.rootConnMu.Lock()
	_, err := p.rootConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", dbName, templateName))
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

	// Use shared reset function if available
	resetFunc := p.resetDatabase
	if p.shared != nil {
		resetFunc = p.shared.resetDatabase
	}

	// Execute the user-provided reset function
	if err := resetFunc(ctx, conn.Conn()); err != nil {
		return fmt.Errorf("failed to reset database: %w", err)
	}

	return nil
}

// Acquire gets a database from the pool
// createDatabaseResourceWithDB creates a new database resource using DB-based allocation
func (p *Pool) createDatabaseResourceWithDB(ctx context.Context) (*DatabaseResource, error) {
	// Create a dedicated connection for this operation to avoid concurrent use
	rootConn, err := p.createRootConnection()
	if err != nil {
		return nil, fmt.Errorf("failed to get root connection: %w", err)
	}
	defer rootConn.Close(ctx)

	// Acquire advisory lock for this pool
	lockID := getPoolLockID(p.poolName)
	if err := acquirePoolLock(rootConn, lockID); err != nil {
		return nil, fmt.Errorf("failed to acquire pool lock: %w", err)
	}
	defer releasePoolLock(rootConn, lockID)

	// Try to acquire a database from DB
	dbInfo, err := acquireDatabaseFromDB(rootConn, p.poolName, os.Getpid())
	if err != nil {
		return nil, fmt.Errorf("failed to acquire database from DB: %w", err)
	}
	if dbInfo == nil {
		return nil, fmt.Errorf("no available database (max size reached)")
	}

	// Check if database exists, create if needed
	var exists bool
	err = rootConn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbInfo.databaseName).Scan(&exists)
	if err != nil {
		releaseDatabaseInDB(rootConn, dbInfo.databaseName)
		return nil, fmt.Errorf("failed to check if database exists: %w", err)
	}
	
	if !exists {
		// Create the test database from template
		templateName := p.templateName
		if p.shared != nil {
			templateName = p.shared.templateName
		}
		
		_, err = rootConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", dbInfo.databaseName, templateName))
		if err != nil {
			releaseDatabaseInDB(rootConn, dbInfo.databaseName)
			return nil, fmt.Errorf("failed to create test database: %w", err)
		}
	}

	// Connect to the database
	config := rootConn.Config()
	
	// Build connection string for the new database
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		config.User,
		config.Password,
		config.Host,
		config.Port,
		dbInfo.databaseName,
	)
	
	// Add SSL mode if present
	if config.TLSConfig == nil {
		connStr += "?sslmode=disable"
	}
	
	// Create pgxpool from the connection string
	poolConfig, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		releaseDatabaseInDB(rootConn, dbInfo.databaseName)
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		releaseDatabaseInDB(rootConn, dbInfo.databaseName)
		return nil, fmt.Errorf("failed to create pool: %w", err)
	}
	
	// Create sql.DB handle for compatibility (optional, can be nil)
	var db *sql.DB = nil

	// Test connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		db.Close()
		releaseDatabaseInDB(rootConn, dbInfo.databaseName)
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Reset the database
	conn, err := pool.Acquire(ctx)
	if err != nil {
		pool.Close()
		db.Close()
		releaseDatabaseInDB(rootConn, dbInfo.databaseName)
		return nil, fmt.Errorf("failed to acquire connection for reset: %w", err)
	}
	
	resetFunc := p.resetDatabase
	if p.shared != nil {
		resetFunc = p.shared.resetDatabase
	}
	
	if err := resetFunc(ctx, conn.Conn()); err != nil {
		conn.Release()
		pool.Close()
		db.Close()
		releaseDatabaseInDB(rootConn, dbInfo.databaseName)
		return nil, fmt.Errorf("failed to reset database: %w", err)
	}
	conn.Release()

	return &DatabaseResource{
		dbName: dbInfo.databaseName,
		pool:   pool,
		sqlDB:  db,
	}, nil
}

// destroyDatabaseResourceWithDB releases a database resource back to the DB pool
func (p *Pool) destroyDatabaseResourceWithDB(resource *DatabaseResource) {
	if resource.pool != nil {
		resource.pool.Close()
	}
	if resource.sqlDB != nil {
		resource.sqlDB.Close()
	}
	
	// Create a connection for the release operation
	rootConn, err := p.createRootConnection()
	if err != nil {
		fmt.Printf("Warning: failed to get connection to release database %s: %v\n", resource.dbName, err)
		return
	}
	defer rootConn.Close(context.Background())
	
	// Release in database
	if err := releaseDatabaseInDB(rootConn, resource.dbName); err != nil {
		fmt.Printf("Warning: failed to release database %s in DB: %v\n", resource.dbName, err)
	}
}

func (p *Pool) Acquire() (*AcquiredDatabase, error) {
	ctx := context.Background()

	// Use shared pool if available
	resourcePool := p.resourcePool
	if p.shared != nil {
		resourcePool = p.shared.resourcePool
	}

	// Acquire a resource from puddle pool
	puddleResource, err := resourcePool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire database resource: %w", err)
	}

	resource := puddleResource.Value()

	// Reset the database to ensure clean state
	if err := p.resetDatabaseResource(ctx, resource); err != nil {
		puddleResource.Release()
		return nil, fmt.Errorf("failed to reset database: %w", err)
	}

	// Return the acquired database with the puddle resource
	// The caller must call Release() when done
	return &AcquiredDatabase{
		Pool:           resource.pool,
		puddleResource: puddleResource,
	}, nil
}

// createTemplateDatabase creates a template database and runs the setup function
func (p *Pool) createTemplateDatabase(ctx context.Context) error {
	// Get template name and setup function
	var templateName string
	var setupFunc func(context.Context, *pgx.Conn) error

	if p.shared != nil {
		templateName = p.shared.templateName
		setupFunc = p.shared.setupTemplate
	} else {
		templateName = p.templateName
		setupFunc = p.setupTemplate
	}

	// Try to create template database
	p.rootConnMu.Lock()
	_, err := p.rootConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", templateName))
	if err != nil {
		p.rootConnMu.Unlock()
		// Check if it's a duplicate error
		// 42P04 is duplicate_database, 23505 is unique_violation
		if pgErr, ok := err.(*pgconn.PgError); ok && (pgErr.Code == "42P04" || pgErr.Code == "23505") {
			// Database already exists, that's fine
			return nil
		}
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
		templateName,
	)

	if config.TLSConfig == nil {
		connStr += "?sslmode=disable"
	}

	templateConn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		// Clean up template database on connection failure
		p.rootConnMu.Lock()
		p.rootConn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", templateName))
		p.rootConnMu.Unlock()
		return fmt.Errorf("failed to connect to template database: %w", err)
	}
	defer templateConn.Close(ctx)

	// Run setup function
	if err := setupFunc(ctx, templateConn); err != nil {
		// Clean up template database on setup failure
		p.rootConnMu.Lock()
		p.rootConn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", templateName))
		p.rootConnMu.Unlock()
		return fmt.Errorf("failed to setup template database: %w", err)
	}

	// Mark template as created
	if p.shared != nil {
		p.shared.templateCreated = true
	} else {
		p.templateCreated = true
	}
	return nil
}

// Close closes the pool and cleans up all resources
func (p *Pool) Close() {
	if p.shared != nil {
		// Decrement reference count for shared pool
		sharedPoolsMu.Lock()
		defer sharedPoolsMu.Unlock()

		p.shared.refCount--
		if p.shared.refCount == 0 {
			// Last reference, close the pool
			if p.shared.resourcePool != nil {
				p.shared.resourcePool.Close()
			}
			delete(sharedPools, p.poolName)
		}
	} else if p.resourcePool != nil {
		p.resourcePool.Close()
	}
}
