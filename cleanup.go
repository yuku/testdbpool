package testdbpool

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"github.com/yuku/testdbpool/internal"
)

// Cleanup manually cleans up all databases and state for a pool
func Cleanup(rootDB *sql.DB, poolID string) error {
	ctx := context.Background()

	// Validate pool ID
	if !internal.PoolIDRegex.MatchString(poolID) {
		return fmt.Errorf("invalid pool ID: %s", poolID)
	}

	// Connect to state database
	stateDB, err := sql.Open(internal.GetDriverName(rootDB), internal.GetConnectionString(rootDB, "postgres"))
	if err != nil {
		return fmt.Errorf("failed to connect to state database: %w", err)
	}
	defer stateDB.Close()

	// Get pool state
	tx, err := stateDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	state, err := internal.GetPoolState(ctx, tx, poolID)
	if err != nil {
		return fmt.Errorf("failed to get pool state: %w", err)
	}

	if state == nil {
		// Pool doesn't exist, nothing to clean up
		return nil
	}

	// Collect all databases to drop
	var databasesToDelete []string

	// Add template database
	databasesToDelete = append(databasesToDelete, state.TemplateDB)

	// Add all pool databases
	databasesToDelete = append(databasesToDelete, state.AvailableDBs...)
	databasesToDelete = append(databasesToDelete, state.InUseDBs...)
	databasesToDelete = append(databasesToDelete, state.FailedDBs...)

	// Delete pool state record
	deleteQuery := "DELETE FROM testdbpool_state WHERE pool_id = $1"
	if _, err := tx.ExecContext(ctx, deleteQuery, poolID); err != nil {
		return fmt.Errorf("failed to delete pool state: %w", err)
	}

	// Commit the transaction to release the lock
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Drop all databases
	var errors []error
	for _, dbName := range databasesToDelete {
		if err := internal.DropDatabase(ctx, rootDB, dbName); err != nil {
			log.Printf("failed to drop database %s: %v", dbName, err)
			errors = append(errors, fmt.Errorf("failed to drop database %s: %w", dbName, err))
		}
	}

	// Return combined errors if any
	if len(errors) > 0 {
		return fmt.Errorf("cleanup completed with %d errors: %v", len(errors), errors[0])
	}

	return nil
}
