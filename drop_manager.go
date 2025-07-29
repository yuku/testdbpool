package testdbpool

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuku/testdbpool/internal/templatedb"
)

// dropManager implements database management with complete recreation on each use
type dropManager struct {
	templateDB *templatedb.TemplateDB
	rootPool   *pgxpool.Pool
}

// newDropManager creates a new drop-based database manager
func newDropManager(templateDB *templatedb.TemplateDB, rootPool *pgxpool.Pool) *dropManager {
	return &dropManager{
		templateDB: templateDB,
		rootPool:   rootPool,
	}
}

// AcquireDatabase creates a fresh database and connection pool for the given index
func (dm *dropManager) AcquireDatabase(ctx context.Context, poolID string, index int) (*pgxpool.Pool, error) {
	// Always create a new database from template for complete isolation
	dbName := getTestDBName(poolID, index)

	// Create database from template
	pool, err := dm.templateDB.Create(ctx, dbName)
	if err != nil {
		return nil, fmt.Errorf("failed to create test database: %w", err)
	}

	return pool, nil
}

// ReleaseDatabase drops the database and closes the connection pool
func (dm *dropManager) ReleaseDatabase(ctx context.Context, poolID string, index int, pool *pgxpool.Pool) error {
	// 1. First close the connection pool
	if pool != nil {
		pool.Close()
		// Give a small moment for connections to fully close
		time.Sleep(5 * time.Millisecond)
	}

	// 2. Drop the database to ensure complete cleanup
	dbName := getTestDBName(poolID, index)
	_, err := dm.rootPool.Exec(ctx, fmt.Sprintf(
		"DROP DATABASE IF EXISTS %s",
		pgx.Identifier{dbName}.Sanitize(),
	))
	if err != nil {
		// Log error but don't fail the release - resource should still be freed
		fmt.Printf("Warning: failed to drop database %s: %v\n", dbName, err)
	}

	return nil
}

// Close cleans up any resources (no persistent state in drop manager)
func (dm *dropManager) Close(ctx context.Context) error {
	// No persistent state to clean up in drop manager
	return nil
}
