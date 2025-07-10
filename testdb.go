package testdbpool

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuku/numpool"
)

// TestDB represents a test database acquired from the pool.
type TestDB struct {
	pool     *Pool
	resource *numpool.Resource
	dbPool   *pgxpool.Pool
	dbName   string
}

// Pool returns the database connection pool for this test database.
func (td *TestDB) Pool() *pgxpool.Pool {
	return td.dbPool
}

// DatabaseName returns the name of the test database.
func (td *TestDB) DatabaseName() string {
	return td.dbName
}

// Release releases the test database back to the pool after resetting it.
// This method should be called when the test is complete, typically using defer.
func (td *TestDB) Release(ctx context.Context) error {
	// Reset and close the pool
	if td.dbPool != nil {
		// Get a connection from the pool for reset
		conn, err := td.dbPool.Acquire(ctx)
		if err == nil {
			// Reset database before releasing
			if err := td.pool.config.ResetDatabase(ctx, conn.Conn()); err != nil {
				// Log error but continue with release
				// In production, you might want to handle this differently
				_ = err // explicitly ignore error
			}
			conn.Release()
		}
		td.dbPool.Close()
		td.dbPool = nil
	}

	// Release the resource back to numpool
	if td.resource != nil {
		if err := td.resource.Release(ctx); err != nil {
			return err
		}
		td.resource = nil
	}

	return nil
}

// Close is an alias for Release that doesn't require a context.
// It's provided for convenience with defer statements.
func (td *TestDB) Close() error {
	return td.Release(context.Background())
}
