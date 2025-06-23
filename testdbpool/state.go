package testdbpool

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// poolState represents a row in the testdbpool_state table
type poolState struct {
	poolID        string
	templateDB    string
	availableDBs  []string
	inUseDBs      []string
	failedDBs     []string
	maxPoolSize   int
	createdAt     time.Time
	lastAccessed  time.Time
}

// createStateTable creates the pool state management table if it doesn't exist
func createStateTable(ctx context.Context, db *sql.DB) error {
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

// getPoolState retrieves the pool state for the given pool ID
func getPoolState(ctx context.Context, tx *sql.Tx, poolID string) (*poolState, error) {
	query := `
	SELECT pool_id, template_db, available_dbs, in_use_dbs, failed_dbs, 
	       max_pool_size, created_at, last_accessed
	FROM testdbpool_state
	WHERE pool_id = $1
	FOR UPDATE`
	
	var state poolState
	var availableDBs, inUseDBs, failedDBs string
	
	err := tx.QueryRowContext(ctx, query, poolID).Scan(
		&state.poolID,
		&state.templateDB,
		&availableDBs,
		&inUseDBs,
		&failedDBs,
		&state.maxPoolSize,
		&state.createdAt,
		&state.lastAccessed,
	)
	
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	
	// Parse PostgreSQL arrays
	state.availableDBs = parsePostgresArray(availableDBs)
	state.inUseDBs = parsePostgresArray(inUseDBs)
	state.failedDBs = parsePostgresArray(failedDBs)
	
	return &state, nil
}

// insertPoolState creates a new pool state record
func insertPoolState(ctx context.Context, tx *sql.Tx, poolID string, maxPoolSize int) error {
	templateDB := fmt.Sprintf("%s_template", poolID)
	query := `
	INSERT INTO testdbpool_state (pool_id, template_db, max_pool_size)
	VALUES ($1, $2, $3)`
	
	_, err := tx.ExecContext(ctx, query, poolID, templateDB, maxPoolSize)
	return err
}

// updatePoolState updates the pool state arrays
func updatePoolState(ctx context.Context, tx *sql.Tx, state *poolState) error {
	query := `
	UPDATE testdbpool_state
	SET available_dbs = $1,
	    in_use_dbs = $2,
	    failed_dbs = $3,
	    last_accessed = NOW()
	WHERE pool_id = $4`
	
	availableDBs := formatPostgresArray(state.availableDBs)
	inUseDBs := formatPostgresArray(state.inUseDBs)
	failedDBs := formatPostgresArray(state.failedDBs)
	
	_, err := tx.ExecContext(ctx, query, availableDBs, inUseDBs, failedDBs, state.poolID)
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

// removeFromSlice removes an element from a slice
func removeFromSlice(slice []string, elem string) []string {
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if s != elem {
			result = append(result, s)
		}
	}
	return result
}

// databaseExists checks if a database exists
func databaseExists(ctx context.Context, db *sql.DB, dbName string) (bool, error) {
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

// createDatabase creates a new database from template
func createDatabase(ctx context.Context, db *sql.DB, dbName, templateName string) error {
	// SQL injection protection: validate database names
	if !poolIDRegex.MatchString(dbName) || !poolIDRegex.MatchString(templateName) {
		return fmt.Errorf("invalid database name")
	}
	
	query := fmt.Sprintf("CREATE DATABASE %s WITH TEMPLATE %s", dbName, templateName)
	_, err := db.ExecContext(ctx, query)
	return err
}

// dropDatabase drops a database
func dropDatabase(ctx context.Context, db *sql.DB, dbName string) error {
	// SQL injection protection: validate database name
	if !poolIDRegex.MatchString(dbName) {
		return fmt.Errorf("invalid database name")
	}
	
	// Check for active connections
	var count int
	checkQuery := `SELECT COUNT(*) FROM pg_stat_activity WHERE datname = $1`
	err := db.QueryRowContext(ctx, checkQuery, dbName).Scan(&count)
	if err != nil {
		return err
	}
	
	if count > 0 {
		return fmt.Errorf("database %s has active connections", dbName)
	}
	
	query := fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName)
	_, err = db.ExecContext(ctx, query)
	return err
}