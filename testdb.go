package testdbpool

import (
	"context"
	"fmt"

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

	// databaseManager handles database lifecycle operations
	databaseManager databaseManager

	// onRelease is called when this TestDB is released to clear it from the pool.
	onRelease func(int)
}

// Release releases the TestDB back to the pool.
// The database will be reset according to the configured strategy.
func (db *TestDB) Release(ctx context.Context) error {
	// Delegate database cleanup to the strategy first
	if err := db.databaseManager.ReleaseDatabase(ctx, db.poolID, db.resource.Index(), db.pool); err != nil {
		return fmt.Errorf("failed to release database: %w", err)
	}

	// Clear this TestDB from the pool's testDBs array
	if db.onRelease != nil {
		db.onRelease(db.resource.Index())
	}

	// Release the resource back to the numpool
	return db.resource.Release(ctx)
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
