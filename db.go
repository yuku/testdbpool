package testdbpool

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// poolInfo represents pool information stored in database
type poolInfo struct {
	poolName         string
	templateDatabase string
	maxSize          int
}

// dbInfo represents database information stored in database
type dbInfo struct {
	poolName     string
	databaseName string
	inUse        bool
	processID    int
}

// ensureTablesExist creates the necessary tables for testdbpool if they don't exist
func ensureTablesExist(conn *pgx.Conn) error {
	ctx := context.Background()

	// Create testdbpool_registry table
	_, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS testdbpool_registry (
			pool_name TEXT PRIMARY KEY,
			template_database TEXT NOT NULL,
			max_size INTEGER NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create testdbpool_registry table: %w", err)
	}

	// Create testdbpool_databases table
	_, err = conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS testdbpool_databases (
			id SERIAL PRIMARY KEY,
			pool_name TEXT REFERENCES testdbpool_registry(pool_name),
			database_name TEXT UNIQUE NOT NULL,
			in_use BOOLEAN DEFAULT FALSE,
			process_id INTEGER,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			last_used_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create testdbpool_databases table: %w", err)
	}

	return nil
}

// registerPoolInDB registers a pool in the database registry
func registerPoolInDB(conn *pgx.Conn, poolName, templateDatabase string, maxSize int) error {
	ctx := context.Background()

	// Check if pool already exists
	existing, err := getPoolInfoFromDB(conn, poolName)
	if err != nil {
		return fmt.Errorf("failed to check existing pool: %w", err)
	}

	if existing != nil {
		// Pool exists, verify configuration matches
		if existing.templateDatabase != templateDatabase || existing.maxSize != maxSize {
			return fmt.Errorf("pool configuration mismatch for %s: existing(template=%s, maxSize=%d) vs new(template=%s, maxSize=%d)",
				poolName, existing.templateDatabase, existing.maxSize, templateDatabase, maxSize)
		}
		// Configuration matches, nothing to do
		return nil
	}

	// Insert new pool
	_, err = conn.Exec(ctx, `
		INSERT INTO testdbpool_registry (pool_name, template_database, max_size)
		VALUES ($1, $2, $3)
	`, poolName, templateDatabase, maxSize)
	if err != nil {
		return fmt.Errorf("failed to register pool: %w", err)
	}

	return nil
}

// getPoolInfoFromDB retrieves pool information from the database
func getPoolInfoFromDB(conn *pgx.Conn, poolName string) (*poolInfo, error) {
	ctx := context.Background()

	var info poolInfo
	err := conn.QueryRow(ctx, `
		SELECT pool_name, template_database, max_size
		FROM testdbpool_registry
		WHERE pool_name = $1
	`, poolName).Scan(&info.poolName, &info.templateDatabase, &info.maxSize)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil // Pool doesn't exist
		}
		return nil, fmt.Errorf("failed to query pool info: %w", err)
	}

	return &info, nil
}

// acquireDatabaseFromDB acquires an available database from the pool
func acquireDatabaseFromDB(conn *pgx.Conn, poolName string, processID int) (*dbInfo, error) {
	ctx := context.Background()

	// Start transaction for atomic operation
	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// First, try to find an existing available database
	var dbName string
	err = tx.QueryRow(ctx, `
		SELECT database_name 
		FROM testdbpool_databases 
		WHERE pool_name = $1 AND in_use = false
		LIMIT 1
		FOR UPDATE
	`, poolName).Scan(&dbName)

	if err == nil {
		// Found an available database, mark it as in use
		_, err = tx.Exec(ctx, `
			UPDATE testdbpool_databases 
			SET in_use = true, process_id = $1, last_used_at = CURRENT_TIMESTAMP
			WHERE database_name = $2
		`, processID, dbName)
		if err != nil {
			return nil, fmt.Errorf("failed to update database status: %w", err)
		}
	} else if err == pgx.ErrNoRows {
		// No available database, check if we can create a new one
		var poolInfo poolInfo
		err = tx.QueryRow(ctx, `
			SELECT pool_name, template_database, max_size 
			FROM testdbpool_registry 
			WHERE pool_name = $1
		`, poolName).Scan(&poolInfo.poolName, &poolInfo.templateDatabase, &poolInfo.maxSize)
		if err != nil {
			return nil, fmt.Errorf("failed to get pool info: %w", err)
		}

		// Count current databases
		var count int
		err = tx.QueryRow(ctx, `
			SELECT COUNT(*) 
			FROM testdbpool_databases 
			WHERE pool_name = $1
		`, poolName).Scan(&count)
		if err != nil {
			return nil, fmt.Errorf("failed to count databases: %w", err)
		}

		if count >= poolInfo.maxSize {
			// Max size reached, cannot create new database
			return nil, nil
		}

		// Generate new database name
		dbName = "testdb_" + generateID()

		// Insert new database entry
		_, err = tx.Exec(ctx, `
			INSERT INTO testdbpool_databases (pool_name, database_name, in_use, process_id)
			VALUES ($1, $2, true, $3)
		`, poolName, dbName, processID)
		if err != nil {
			return nil, fmt.Errorf("failed to insert database entry: %w", err)
		}
	} else {
		return nil, fmt.Errorf("failed to query available database: %w", err)
	}

	// Commit transaction
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &dbInfo{
		poolName:     poolName,
		databaseName: dbName,
		inUse:        true,
		processID:    processID,
	}, nil
}

// releaseDatabaseInDB releases a database back to the pool
func releaseDatabaseInDB(conn *pgx.Conn, databaseName string) error {
	ctx := context.Background()

	_, err := conn.Exec(ctx, `
		UPDATE testdbpool_databases 
		SET in_use = false, process_id = NULL
		WHERE database_name = $1
	`, databaseName)
	if err != nil {
		return fmt.Errorf("failed to release database: %w", err)
	}

	return nil
}

// generateID generates a unique ID for database names
func generateID() string {
	return uuid.New().String()[:8]
}