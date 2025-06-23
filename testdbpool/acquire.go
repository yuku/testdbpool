package testdbpool

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"testing"
	"time"
)

// Acquire gets a database from the pool (automatically releases via testing.T.Cleanup)
func (p *Pool) Acquire(t *testing.T) (*sql.DB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), p.config.AcquireTimeout)
	defer cancel()

	// Start transaction with timeout
	tx, err := p.stateDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Acquire pool state lock
	state, err := getPoolState(ctx, tx, p.config.PoolID)
	if err != nil {
		return nil, fmt.Errorf("failed to get pool state: %w", err)
	}

	if state == nil {
		return nil, fmt.Errorf("pool state not found for pool_id: %s", p.config.PoolID)
	}

	// Create template database on first acquire
	if !p.templateExists {
		templateDB := state.templateDB
		exists, err := databaseExists(ctx, p.config.RootConnection, templateDB)
		if err != nil {
			return nil, fmt.Errorf("failed to check template database existence: %w", err)
		}

		if !exists {
			// Create template database
			createQuery := fmt.Sprintf("CREATE DATABASE %s", templateDB)
			if _, err := p.config.RootConnection.ExecContext(ctx, createQuery); err != nil {
				return nil, fmt.Errorf("failed to create template database: %w", err)
			}

			// Connect to template database and run template creator
			templateConnStr := getConnectionString(p.config.RootConnection, templateDB)
			templateDB, err := sql.Open("postgres", templateConnStr)
			if err != nil {
				return nil, fmt.Errorf("failed to connect to template database: %w", err)
			}
			defer templateDB.Close()

			if err := p.config.TemplateCreator(ctx, templateDB); err != nil {
				return nil, fmt.Errorf("failed to execute template creator: %w", err)
			}
		}

		p.templateExists = true
	}

	var dbName string

	// Check for available databases
	if len(state.availableDBs) > 0 {
		// Take from available pool
		dbName = state.availableDBs[0]
		state.availableDBs = state.availableDBs[1:]
		state.inUseDBs = append(state.inUseDBs, dbName)
	} else if len(state.inUseDBs)+len(state.failedDBs) < state.maxPoolSize {
		// Create new database
		dbNum := len(state.inUseDBs) + len(state.failedDBs) + len(state.availableDBs) + 1
		dbName = fmt.Sprintf("%s_%d", p.config.PoolID, dbNum)

		// Create database from template
		if err := createDatabase(ctx, p.config.RootConnection, dbName, state.templateDB); err != nil {
			return nil, fmt.Errorf("failed to create database %s: %w", dbName, err)
		}

		state.inUseDBs = append(state.inUseDBs, dbName)
	} else {
		// Pool exhausted
		return nil, fmt.Errorf("pool exhausted: max size %d reached", state.maxPoolSize)
	}

	// Update state
	if err := updatePoolState(ctx, tx, state); err != nil {
		return nil, fmt.Errorf("failed to update pool state: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Connect to the acquired database
	dbConnStr := getConnectionString(p.config.RootConnection, dbName)
	db, err := sql.Open("postgres", dbConnStr)
	if err != nil {
		// If we fail to connect, we should move the database back to available
		p.releaseDatabase(dbName, false)
		return nil, fmt.Errorf("failed to connect to database %s: %w", dbName, err)
	}

	// Register cleanup
	t.Cleanup(func() {
		// Close the database connection
		db.Close()

		// Execute reset function
		resetCtx := context.Background()
		resetDB, err := sql.Open("postgres", dbConnStr)
		if err != nil {
			t.Logf("failed to reconnect for reset: %v", err)
			p.releaseDatabase(dbName, true)
			return
		}
		defer resetDB.Close()

		resetSuccess := false
		if err := p.config.ResetFunc(resetCtx, resetDB); err != nil {
			t.Logf("reset function failed for database %s: %v", dbName, err)
		} else {
			resetSuccess = true
		}

		// Release the database back to pool
		p.releaseDatabase(dbName, !resetSuccess)
	})

	return db, nil
}

// releaseDatabase releases a database back to the pool
func (p *Pool) releaseDatabase(dbName string, failed bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tx, err := p.stateDB.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("failed to begin transaction for release: %v", err)
		return
	}
	defer tx.Rollback()

	state, err := getPoolState(ctx, tx, p.config.PoolID)
	if err != nil {
		log.Printf("failed to get pool state for release: %v", err)
		return
	}

	if state == nil {
		log.Printf("pool state not found for release")
		return
	}

	// Remove from in_use_dbs
	state.inUseDBs = removeFromSlice(state.inUseDBs, dbName)

	// Add to appropriate list
	if failed {
		state.failedDBs = append(state.failedDBs, dbName)
	} else {
		state.availableDBs = append(state.availableDBs, dbName)
	}

	if err := updatePoolState(ctx, tx, state); err != nil {
		log.Printf("failed to update pool state for release: %v", err)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("failed to commit release transaction: %v", err)
	}
}