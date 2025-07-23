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

	// reset is the function that resets the database to a clean state.
	reset func(context.Context, *pgxpool.Pool) error

	// onRelease is called when this TestDB is released to clear it from the pool.
	onRelease func(int)
}

// Release releases the TestDB back to the pool.
// It resets the database to a clean state using the ResetDatabase function
// defined in the Pool configuration.
func (db *TestDB) Release(ctx context.Context) error {
	if db.reset != nil {
		if err := db.reset(ctx, db.pool); err != nil {
			return fmt.Errorf("failed to reset database: %w", err)
		}
	}

	// Clear this TestDB from the pool's testDBs array
	if db.onRelease != nil {
		db.onRelease(db.resource.Index())
	}

	// Release the resource back to the numpool.
	return db.resource.Release(ctx)
}

// Pool returns the pgxpool.Pool connected to the postgres database that db represents.
func (db *TestDB) Pool() *pgxpool.Pool {
	return db.pool
}

func (db *TestDB) Name() string {
	return getTestDBName(db.poolID, db.resource.Index())
}

func getTestDBName(poolID string, index int) string {
	// templatedb validates the length of the pool ID, and as long as it is valid,
	// the string returned by this method will be valid too.
	return fmt.Sprintf("testdbpool_%s_%d", poolID, index)
}
