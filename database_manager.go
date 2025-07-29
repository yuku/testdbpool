package testdbpool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuku/testdbpool/internal/templatedb"
)

// databaseManager handles complete database lifecycle management
type databaseManager interface {
	// AcquireDatabase returns a connection pool for the given index
	// May reuse existing pool or create new database as needed
	AcquireDatabase(ctx context.Context, poolID string, index int) (*pgxpool.Pool, error)

	// ReleaseDatabase handles cleanup when database is released
	// May reset data or drop database depending on strategy
	ReleaseDatabase(ctx context.Context, poolID string, index int, pool *pgxpool.Pool) error

	// Close cleans up all resources managed by this strategy
	Close(ctx context.Context) error
}

// strategyMetadata defines the metadata structure for strategy persistence
type strategyMetadata struct {
	DatabaseStrategy string `json:"databaseStrategy"` // "truncate" or "drop"
}

// createDatabaseManager creates the appropriate strategy based on configuration
func createDatabaseManager(templateDB *templatedb.TemplateDB, rootPool *pgxpool.Pool, resetFunc func(context.Context, *pgxpool.Pool) error, maxDatabases int) databaseManager {
	if resetFunc != nil {
		return newTruncateManager(templateDB, rootPool, resetFunc, maxDatabases)
	}
	return newDropManager(templateDB, rootPool)
}

// getStrategyType returns the strategy type for metadata
func getStrategyType(resetFunc func(context.Context, *pgxpool.Pool) error) string {
	if resetFunc != nil {
		return "truncate"
	}
	return "drop"
}

// validateStrategyConsistency checks if the strategy matches stored metadata
func validateStrategyConsistency(storedMetadata json.RawMessage, expectedStrategy string) error {
	if len(storedMetadata) == 0 {
		// No existing metadata, this is the first pool with this ID
		return nil
	}

	var metadata strategyMetadata
	if err := json.Unmarshal(storedMetadata, &metadata); err != nil {
		return fmt.Errorf("failed to parse pool metadata: %w", err)
	}

	if metadata.DatabaseStrategy != expectedStrategy {
		return fmt.Errorf("strategy conflict: pool was created with '%s' strategy, but current configuration specifies '%s' strategy", metadata.DatabaseStrategy, expectedStrategy)
	}

	return nil
}

// createStrategyMetadata creates metadata for the given strategy
func createStrategyMetadata(strategy string) (json.RawMessage, error) {
	metadata := strategyMetadata{
		DatabaseStrategy: strategy,
	}
	return json.Marshal(metadata)
}
