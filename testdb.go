package testdbpool

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/yuku/numpool"
)

// TestDB represents a test database acquired from the pool.
type TestDB struct {
	pool     *Pool
	resource *numpool.Resource
	conn     *pgx.Conn
	dbName   string
}

// Conn returns the database connection for this test database.
func (td *TestDB) Conn() *pgx.Conn {
	return td.conn
}

// DatabaseName returns the name of the test database.
func (td *TestDB) DatabaseName() string {
	return td.dbName
}

// Release releases the test database back to the pool after resetting it.
// This method should be called when the test is complete, typically using defer.
func (td *TestDB) Release(ctx context.Context) error {
	// TODO: Implement release logic
	return nil
}

// Close is an alias for Release that doesn't require a context.
// It's provided for convenience with defer statements.
func (td *TestDB) Close() error {
	return td.Release(context.Background())
}