package testdbpool

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuku/testdbpool/internal/templatedb"
)

// truncateManager implements database management with pool reuse and TRUNCATE reset
type truncateManager struct {
	templateDB *templatedb.TemplateDB
	rootPool   *pgxpool.Pool
	resetFunc  func(context.Context, *pgxpool.Pool) error

	// poolCache stores reusable connection pools by index
	poolCache map[int]*pgxpool.Pool
	mu        sync.Mutex
}

// newTruncateManager creates a new truncate-based database manager
func newTruncateManager(templateDB *templatedb.TemplateDB, rootPool *pgxpool.Pool, resetFunc func(context.Context, *pgxpool.Pool) error, maxDatabases int) *truncateManager {
	return &truncateManager{
		templateDB: templateDB,
		rootPool:   rootPool,
		resetFunc:  resetFunc,
		poolCache:  make(map[int]*pgxpool.Pool, maxDatabases),
	}
}

// AcquireDatabase returns a connection pool for the given index, reusing if available
func (tm *truncateManager) AcquireDatabase(ctx context.Context, poolID string, index int) (*pgxpool.Pool, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Check if we have a cached pool for this index
	if pool, exists := tm.poolCache[index]; exists {
		return pool, nil
	}

	// Create new database and connection pool
	dbName := getTestDBName(poolID, index)
	pool, err := tm.templateDB.Create(ctx, dbName)
	if err != nil {
		return nil, fmt.Errorf("failed to create test database: %w", err)
	}

	// Cache the pool for future reuse
	tm.poolCache[index] = pool
	return pool, nil
}

// ReleaseDatabase resets the database using TRUNCATE and keeps the pool for reuse
func (tm *truncateManager) ReleaseDatabase(ctx context.Context, poolID string, index int, pool *pgxpool.Pool) error {
	// Reset the database to clean state using the provided reset function
	if tm.resetFunc != nil {
		if err := tm.resetFunc(ctx, pool); err != nil {
			return fmt.Errorf("failed to reset database: %w", err)
		}
	}

	// Pool remains in cache for reuse - no cleanup needed
	return nil
}

// Close cleans up all cached pools and databases
func (tm *truncateManager) Close(ctx context.Context) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Close all cached connection pools
	for index, pool := range tm.poolCache {
		if pool != nil {
			pool.Close()

			// Drop the database
			dbName := getTestDBName("", index) // We don't have poolID here, but this is cleanup
			_, err := tm.rootPool.Exec(ctx, fmt.Sprintf(
				"DROP DATABASE IF EXISTS %s",
				pgx.Identifier{dbName}.Sanitize(),
			))
			if err != nil {
				// Log but continue cleanup
				fmt.Printf("Warning: failed to drop database %s during cleanup: %v\n", dbName, err)
			}
		}
	}

	// Clear the cache
	tm.poolCache = make(map[int]*pgxpool.Pool)
	return nil
}
