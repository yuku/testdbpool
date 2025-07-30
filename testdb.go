package testdbpool

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuku/numpool"
)

type TestDB struct {
	// poolID is the ID of the pool that this TestDB belongs to.
	poolID string

	// pool is the pgxpool.Pool connected to the postgres database that db represents.
	pool *pgxpool.Pool

	// resource is the numpool.Resource that was acquired for this TestDB.
	resource *numpool.Resource

	// rootPool is the root connection pool for database operations
	rootPool *pgxpool.Pool

	// onRelease is called when this TestDB is released to clear it from the pool.
	onRelease func(int)
}

// Release releases the TestDB back to the pool.
// The database will be dropped to ensure complete cleanup.
func (db *TestDB) Release(ctx context.Context) error {
	// 1. First close the connection pool
	if db.pool != nil {
		db.pool.Close()
	}

	// 2. Drop the database to ensure complete cleanup
	var err error
	if db.rootPool != nil {
		dbName := db.Name()
		_, e := db.rootPool.Exec(ctx, fmt.Sprintf(
			"DROP DATABASE IF EXISTS %s",
			pgx.Identifier{dbName}.Sanitize(),
		))
		if e != nil {
			err = fmt.Errorf("failed to drop database %s: %w", dbName, e)
		}
	}

	// Clear this TestDB from the pool's testDBs array
	if db.onRelease != nil {
		db.onRelease(db.resource.Index())
	}

	// Release the resource back to the numpool
	if err := db.resource.Release(ctx); err != nil {
		return fmt.Errorf("failed to release resource: %w", err)
	}
	return err
}

// Pool returns the pgxpool.Pool connected to the postgres database that db represents.
func (db *TestDB) Pool() *pgxpool.Pool {
	return db.pool
}

func (db *TestDB) Name() string {
	// Extract database name from the pool configuration
	return db.pool.Config().ConnConfig.Database
}

func getTestDBName(poolID string, index int) string {
	// templatedb validates the length of the pool ID, and as long as it is valid,
	// the string returned by this method will be valid too.
	return fmt.Sprintf("testdbpool_%s_%d", poolID, index)
}
