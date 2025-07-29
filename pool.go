package testdbpool

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuku/numpool"
	"github.com/yuku/testdbpool/internal/templatedb"
)

type Pool struct {
	// cfg is the configuration for this TestDBPool instance.
	cfg *Config

	// manager is the numpool.Manager that manages the resources for this Pool.
	manager *numpool.Manager

	// numPool is the numpool instance that manages the resources for this Pool.
	numPool *numpool.Numpool

	// templateDB manages the template database used for creating test databases.
	templateDB *templatedb.TemplateDB

	// testDBs is a slice of TestDB instances that have been acquired from this Pool.
	// The length of this slice is equal to MaxDatabases and each index corresponds
	// to a resource index in the numpool.
	testDBs []*TestDB

	// dbPools is a slice of pgxpool.Pool instances that are used for the test databases.
	// The length of this slice is equal to MaxDatabases and each index corresponds
	// to a resource index in the numpool.
	// This is used to keep track of the pgxpool.Pool instances for each test database.
	dbPools []*pgxpool.Pool
}

type Config struct {
	// ID is a unique identifier for the TestDBPool instance.
	ID string

	// Pool is the pgxpool.Pool to use for root database connections.
	Pool *pgxpool.Pool

	// MaxDatabases is the maximum number of test databases in the pool.
	// Must be between 1 and numpool.MaxResourcesLimit.
	// If not set (0), defaults to min(runtime.GOMAXPROCS(0), numpool.MaxResourcesLimit).
	MaxDatabases int

	// SetupTemplate is called once to set up the template database.
	// The template database is used as a source for creating test databases.
	SetupTemplate func(context.Context, *pgx.Conn) error
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("ID is required")
	}

	if c.Pool == nil {
		return fmt.Errorf("pool is required")
	}

	// Apply default for MaxDatabases if not set
	if c.MaxDatabases == 0 {
		gomaxprocs := runtime.GOMAXPROCS(0)
		c.MaxDatabases = min(gomaxprocs, numpool.MaxResourcesLimit)
	}

	if c.MaxDatabases < 1 || c.MaxDatabases > numpool.MaxResourcesLimit {
		return fmt.Errorf("MaxDatabases must be between 1 and %d, got %d", numpool.MaxResourcesLimit, c.MaxDatabases)
	}

	if c.SetupTemplate == nil {
		return fmt.Errorf("SetupTemplate function is required")
	}

	return nil
}

// New creates a new TestDBPool instance with the provided configuration.
func New(ctx context.Context, cfg *Config) (*Pool, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Setup numpool database if needed
	manager, err := numpool.Setup(ctx, cfg.Pool)
	if err != nil {
		return nil, fmt.Errorf("failed to setup numpool: %w", err)
	}

	// Create or open numpool
	numPool, err := manager.GetOrCreate(ctx, numpool.Config{
		ID:                cfg.ID,
		MaxResourcesCount: int32(cfg.MaxDatabases),
	})
	if err != nil {
		manager.Close()
		return nil, fmt.Errorf("failed to create numpool: %w", err)
	}

	templateDB, err := templatedb.New(&templatedb.Config{
		PoolID:   cfg.ID,
		ConnPool: cfg.Pool,
		Setup:    cfg.SetupTemplate,
	})
	if err != nil {
		manager.Close() // Closing manager also closes the numpool
		return nil, fmt.Errorf("failed to create template database: %w", err)
	}

	return &Pool{
		cfg:        cfg,
		manager:    manager,
		numPool:    numPool,
		templateDB: templateDB,
		testDBs:    make([]*TestDB, cfg.MaxDatabases),
		dbPools:    make([]*pgxpool.Pool, cfg.MaxDatabases),
	}, nil
}

// Acquire acquires a test database from the pool.
func (p *Pool) Acquire(ctx context.Context) (*TestDB, error) {
	resource, err := p.numPool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire resource from numpool: %w", err)
	}
	if resource == nil {
		// should not happen, but just in case
		return nil, fmt.Errorf("acquired nil resource from numpool")
	}

	// There is a guarantee that only one goroutine can acquire a resource with
	// a given index at a time.
	dbIndex := resource.Index()
	if dbIndex >= len(p.testDBs) {
		// should not happen as long as numpool works correctly
		if err := resource.Release(ctx); err != nil {
			return nil, fmt.Errorf("failed to release resource: %w", err)
		}
		return nil, fmt.Errorf(
			"invalid resource index %d for pool with %d databases",
			dbIndex, len(p.testDBs),
		)
	}

	if testDB := p.testDBs[dbIndex]; testDB != nil {
		// should not happen, but just in case
		return nil, fmt.Errorf("test database at index %d is already acquired", dbIndex)
	}

	// Always create a new database from template for complete isolation
	dbName := getTestDBName(p.cfg.ID, dbIndex)
	
	// Drop existing database if it exists (connection should be closed by now)
	if p.dbPools[dbIndex] != nil {
		_, err := p.cfg.Pool.Exec(ctx, fmt.Sprintf(
			"DROP DATABASE IF EXISTS %s",
			pgx.Identifier{dbName}.Sanitize(),
		))
		if err != nil {
			if err2 := resource.Release(ctx); err2 != nil {
				return nil, fmt.Errorf("failed to release resource after drop error: %w", err2)
			}
			return nil, fmt.Errorf("failed to drop existing database: %w", err)
		}
	}
	
	// Create a new database from template
	pool, err := p.templateDB.Create(ctx, dbName)
	if err != nil {
		if err2 := resource.Release(ctx); err2 != nil {
			return nil, fmt.Errorf("failed to release resource after error: %w", err2)
		}
		return nil, fmt.Errorf("failed to create test database: %w", err)
	}
	p.dbPools[dbIndex] = pool

	p.testDBs[dbIndex] = &TestDB{
		poolID:   p.cfg.ID,
		pool:     p.dbPools[dbIndex],
		resource: resource,
		rootPool: p.cfg.Pool,
		onRelease: func(index int) {
			if index < len(p.testDBs) {
				p.testDBs[index] = nil
			}
			// Don't clear dbPools[index] here - we need it to track that a database exists to drop
		},
	}
	return p.testDBs[dbIndex], nil
}

// Close closes all resources generated by this Pool.
// It does not close the given root pgxpool.Pool since it is caller's
// responsibility to manage that connection pool.
func (p *Pool) Close(ctx context.Context) error {
	for _, testDB := range p.testDBs {
		if testDB != nil {
			if err := testDB.Release(ctx); err != nil {
				return fmt.Errorf("failed to release test database %s: %w", testDB.Name(), err)
			}
		}
	}
	for _, dbPool := range p.dbPools {
		if dbPool != nil {
			dbPool.Close()
		}
	}
	p.manager.Close()
	p.testDBs = nil
	return nil
}

// Cleanup all resources including the databases.
// It is mainly used in tests to ensure that all resources are cleaned up.
// So it ignores any errors that might occur during cleanup.
func (p *Pool) Cleanup() {
	ctx := context.Background()

	_ = p.templateDB.Cleanup(ctx)
	_ = p.Close(ctx)

	wg := sync.WaitGroup{}
	wg.Add(p.cfg.MaxDatabases)
	for i := range p.cfg.MaxDatabases {
		go func() {
			defer wg.Done()
			_, _ = p.cfg.Pool.Exec(ctx, fmt.Sprintf(
				"DROP DATABASE IF EXISTS %s",
				pgx.Identifier{getTestDBName(p.cfg.ID, i)}.Sanitize(),
			))
		}()
	}

	wg.Wait()
}

// TemplateDBName returns the name of the template database used by this Pool.
func (p *Pool) TemplateDBName() string {
	return p.templateDB.Name()
}
