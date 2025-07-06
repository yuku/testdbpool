package testdbpool

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

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