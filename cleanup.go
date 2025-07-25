package testdbpool

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ListPools returns a list of pool IDs that match the given prefix.
// This function is used to discover existing testdbpool instances for cleanup purposes.
func ListPools(ctx context.Context, pool *pgxpool.Pool, prefix string) ([]string, error) {
	return nil, errors.New("not implemented")
}

// CleanupPool removes a testdbpool instance and all its associated resources.
// This includes dropping all test databases and cleaning up the template database.
func CleanupPool(ctx context.Context, pool *pgxpool.Pool, poolID string) error {
	return errors.New("not implemented")
}
