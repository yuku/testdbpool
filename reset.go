package testdbpool

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// ResetByTransaction returns a reset function that uses transaction rollback
// Note: This doesn't support nested transactions
func ResetByTransaction() func(ctx context.Context, db *sql.DB) error {
	return func(ctx context.Context, db *sql.DB) error {
		// Since we can't rollback to a savepoint across connections,
		// this is a no-op. The database will be reset by truncating
		// or recreating in the next test.
		return fmt.Errorf("ResetByTransaction is not supported - use ResetByTruncate or ResetByRecreation instead")
	}
}

// ResetByTruncate returns a reset function that truncates specified tables and restores initial data
func ResetByTruncate(tables []string, seedFunc func(ctx context.Context, db *sql.DB) error) func(ctx context.Context, db *sql.DB) error {
	return func(ctx context.Context, db *sql.DB) error {
		// Validate table names to prevent SQL injection
		for _, table := range tables {
			if !isValidTableName(table) {
				return fmt.Errorf("invalid table name: %s", table)
			}
		}

		// Start transaction
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}
		defer func() { _ = tx.Rollback() }()

		// Disable foreign key checks temporarily
		if _, err := tx.ExecContext(ctx, "SET session_replication_role = 'replica'"); err != nil {
			return fmt.Errorf("failed to disable foreign key checks: %w", err)
		}

		// Truncate tables
		for _, table := range tables {
			query := fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table)
			if _, err := tx.ExecContext(ctx, query); err != nil {
				return fmt.Errorf("failed to truncate table %s: %w", table, err)
			}
		}

		// Re-enable foreign key checks
		if _, err := tx.ExecContext(ctx, "SET session_replication_role = 'origin'"); err != nil {
			return fmt.Errorf("failed to re-enable foreign key checks: %w", err)
		}

		// Commit truncation
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit truncation: %w", err)
		}

		// Restore seed data if provided
		if seedFunc != nil {
			if err := seedFunc(ctx, db); err != nil {
				return fmt.Errorf("failed to restore seed data: %w", err)
			}
		}

		return nil
	}
}

// ResetByRecreation returns a reset function that recreates the database
// This is the most reliable but time-consuming method
func ResetByRecreation(templateCreator func(ctx context.Context, db *sql.DB) error) func(ctx context.Context, db *sql.DB) error {
	return func(ctx context.Context, db *sql.DB) error {
		// Since we're already using template databases, we just need to ensure
		// the database is in a clean state. The actual recreation happens
		// at the pool level by discarding this database and creating a new one.
		// For now, we'll attempt to clean all user-created objects.

		// Get all user-created tables
		query := `
		SELECT tablename 
		FROM pg_tables 
		WHERE schemaname = 'public'
		`

		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return fmt.Errorf("failed to query tables: %w", err)
		}
		defer func() { _ = rows.Close() }()

		var tables []string
		for rows.Next() {
			var table string
			if err := rows.Scan(&table); err != nil {
				return fmt.Errorf("failed to scan table name: %w", err)
			}
			tables = append(tables, table)
		}

		if err := rows.Err(); err != nil {
			return fmt.Errorf("failed to iterate tables: %w", err)
		}

		// Drop all tables
		for _, table := range tables {
			if !isValidTableName(table) {
				continue
			}
			dropQuery := fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table)
			if _, err := db.ExecContext(ctx, dropQuery); err != nil {
				return fmt.Errorf("failed to drop table %s: %w", table, err)
			}
		}

		// Recreate schema using template creator
		if templateCreator != nil {
			if err := templateCreator(ctx, db); err != nil {
				return fmt.Errorf("failed to recreate schema: %w", err)
			}
		}

		return nil
	}
}

// ResetBySQL returns a reset function that executes custom SQL
func ResetBySQL(resetSQL string) func(ctx context.Context, db *sql.DB) error {
	return func(ctx context.Context, db *sql.DB) error {
		_, err := db.ExecContext(ctx, resetSQL)
		if err != nil {
			return fmt.Errorf("failed to execute reset SQL: %w", err)
		}
		return nil
	}
}

// isValidTableName checks if a table name is valid (alphanumeric, underscore, and dot for schema)
func isValidTableName(name string) bool {
	// Allow schema.table format
	parts := strings.Split(name, ".")
	if len(parts) > 2 {
		return false
	}

	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, ch := range part {
			if ch < 'a' || ch > 'z' {
				if ch < 'A' || ch > 'Z' {
					if ch < '0' || ch > '9' {
						if ch != '_' {
							return false
						}
					}
				}
			}
		}
	}
	return true
}
