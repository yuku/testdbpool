package testdbpool

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuku/numpool"
)

// ListPools returns a list of pool IDs that match the given prefix.
// This function is used to discover existing testdbpool instances for cleanup purposes.
func ListPools(ctx context.Context, pool *pgxpool.Pool, prefix string) ([]string, error) {
	manager, err := numpool.Setup(ctx, pool)
	if err != nil {
		return nil, err
	}
	defer manager.Close()
	return manager.ListPools(ctx, prefix)
}

// CleanupPool removes a testdbpool instance and all its associated resources.
// This includes dropping all test databases and cleaning up the template database.
func CleanupPool(ctx context.Context, pool *pgxpool.Pool, poolID string) error {
	manager, err := numpool.Setup(ctx, pool)
	if err != nil {
		return err
	}
	defer manager.Close()
	return manager.DeletePool(ctx, poolID)
}
