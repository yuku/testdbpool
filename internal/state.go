package internal

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// PoolState represents a row in the testdbpool_state table
type PoolState struct {
	PoolID       string
	TemplateDB   string
	AvailableDBs []string
	InUseDBs     []string
	FailedDBs    []string
	MaxPoolSize  int
	CreatedAt    time.Time
	LastAccessed time.Time
}

// CreateStateTable creates the pool state management table if it doesn't exist
func CreateStateTable(ctx context.Context, db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS testdbpool_state (
		pool_id VARCHAR PRIMARY KEY,
		template_db VARCHAR NOT NULL,
		available_dbs TEXT[] DEFAULT '{}',
		in_use_dbs TEXT[] DEFAULT '{}',
		failed_dbs TEXT[] DEFAULT '{}',
		max_pool_size INTEGER NOT NULL,
		created_at TIMESTAMP DEFAULT NOW(),
		last_accessed TIMESTAMP DEFAULT NOW()
	)`

	_, err := db.ExecContext(ctx, query)
	return err
}

// GetPoolState retrieves the pool state for the given pool ID
func GetPoolState(ctx context.Context, tx *sql.Tx, poolID string) (*PoolState, error) {
	query := `
	SELECT pool_id, template_db, available_dbs, in_use_dbs, failed_dbs, 
	       max_pool_size, created_at, last_accessed
	FROM testdbpool_state
	WHERE pool_id = $1
	FOR UPDATE`

	var state PoolState
	var availableDBs, inUseDBs, failedDBs string

	err := tx.QueryRowContext(ctx, query, poolID).Scan(
		&state.PoolID,
		&state.TemplateDB,
		&availableDBs,
		&inUseDBs,
		&failedDBs,
		&state.MaxPoolSize,
		&state.CreatedAt,
		&state.LastAccessed,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Parse PostgreSQL arrays
	state.AvailableDBs = parsePostgresArray(availableDBs)
	state.InUseDBs = parsePostgresArray(inUseDBs)
	state.FailedDBs = parsePostgresArray(failedDBs)

	return &state, nil
}

// InsertPoolState creates a new pool state record
func InsertPoolState(ctx context.Context, tx *sql.Tx, poolID string, maxPoolSize int) error {
	templateDB := fmt.Sprintf("%s_template", poolID)
	query := `
	INSERT INTO testdbpool_state (pool_id, template_db, max_pool_size)
	VALUES ($1, $2, $3)`

	_, err := tx.ExecContext(ctx, query, poolID, templateDB, maxPoolSize)
	return err
}

// UpdatePoolState updates the pool state arrays
func UpdatePoolState(ctx context.Context, tx *sql.Tx, state *PoolState) error {
	query := `
	UPDATE testdbpool_state
	SET available_dbs = $1,
	    in_use_dbs = $2,
	    failed_dbs = $3,
	    last_accessed = NOW()
	WHERE pool_id = $4`

	availableDBs := formatPostgresArray(state.AvailableDBs)
	inUseDBs := formatPostgresArray(state.InUseDBs)
	failedDBs := formatPostgresArray(state.FailedDBs)

	_, err := tx.ExecContext(ctx, query, availableDBs, inUseDBs, failedDBs, state.PoolID)
	return err
}

// parsePostgresArray converts a PostgreSQL array string to a Go slice
func parsePostgresArray(s string) []string {
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	if s == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}

// formatPostgresArray converts a Go slice to a PostgreSQL array string
func formatPostgresArray(arr []string) string {
	if len(arr) == 0 {
		return "{}"
	}
	return "{" + strings.Join(arr, ",") + "}"
}

// RemoveFromSlice removes an element from a slice
func RemoveFromSlice(slice []string, elem string) []string {
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if s != elem {
			result = append(result, s)
		}
	}
	return result
}

// DatabaseExists checks if a database exists
func DatabaseExists(ctx context.Context, db *sql.DB, dbName string) (bool, error) {
	query := `SELECT 1 FROM pg_database WHERE datname = $1`
	var exists int
	err := db.QueryRowContext(ctx, query, dbName).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// CreateDatabase creates a new database from template
func CreateDatabase(ctx context.Context, db *sql.DB, dbName, templateName string) error {
	// SQL injection protection: validate database names
	if !PoolIDRegex.MatchString(dbName) || !PoolIDRegex.MatchString(templateName) {
		return fmt.Errorf("invalid database name")
	}

	// First, ensure no active connections to template database
	// Only terminate connections if the template database already exists
	var exists bool
	checkQuery := "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)"
	err := db.QueryRowContext(ctx, checkQuery, templateName).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check if template database exists: %w", err)
	}

	if exists {
		terminateQuery := fmt.Sprintf(`
			SELECT pg_terminate_backend(pid) 
			FROM pg_stat_activity 
			WHERE datname = '%s' AND pid <> pg_backend_pid()`, templateName)
		_, _ = db.ExecContext(ctx, terminateQuery)

		// Small delay to ensure connections are terminated
		time.Sleep(100 * time.Millisecond)
	}

	query := fmt.Sprintf("CREATE DATABASE %s WITH TEMPLATE %s", dbName, templateName)
	_, err = db.ExecContext(ctx, query)
	if err != nil && (strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "duplicate key")) {
		// Database already exists, this can happen in race conditions
		return nil
	}
	return err
}

// DropDatabase drops a database
func DropDatabase(ctx context.Context, db *sql.DB, dbName string) error {
	// SQL injection protection: validate database name
	if !PoolIDRegex.MatchString(dbName) {
		return fmt.Errorf("invalid database name")
	}

	// First, terminate all connections to the database
	terminateQuery := `
		SELECT pg_terminate_backend(pid) 
		FROM pg_stat_activity 
		WHERE datname = $1 AND pid <> pg_backend_pid()`
	_, _ = db.ExecContext(ctx, terminateQuery, dbName)

	// Small delay to ensure connections are terminated
	time.Sleep(100 * time.Millisecond)

	// Drop the database
	query := fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName)
	_, err := db.ExecContext(ctx, query)
	return err
}
