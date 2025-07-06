package testdbpool

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// poolInfo represents pool information stored in database
type poolInfo struct {
	poolName         string
	templateDatabase string
	maxSize          int
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