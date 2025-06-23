package internal

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"testing"
	"time"
)

// Pool manages database pools
type Pool struct {
	Config         Configuration
	StateDB        *sql.DB
	TemplateExists bool
}

// Configuration holds pool initialization settings
type Configuration struct {
	// Database connection settings (for state management table, required)
	RootConnection *sql.DB

	// State management database name (default: "postgres")
	StateDatabase string

	// Pool ID (identifier for multi-process & DB name prefix, required)
	// Max 50 characters, alphanumeric and underscore only
	PoolID string

	// Maximum pool size (default: runtime.GOMAXPROCS(0) * 2)
	MaxPoolSize int

	// Timeout settings (default: 30 seconds)
	AcquireTimeout time.Duration

	// Template DB creation function (schema + seed data, required)
	TemplateCreator func(ctx context.Context, db *sql.DB) error

	// Reset function (data reset on Release, required)
	ResetFunc func(ctx context.Context, db *sql.DB) error
}

// New creates a new database pool
func New(config Configuration) (*Pool, error) {
	// Validate configuration
	if err := ValidateConfig(&config); err != nil {
		return nil, err
	}

	// Connect to state management database
	ctx := context.Background()
	stateConnStr := GetConnectionString(config.RootConnection, config.StateDatabase)
	stateDB, err := sql.Open("postgres", stateConnStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to state database: %w", err)
	}

	// Create state management table
	if err := CreateStateTable(ctx, stateDB); err != nil {
		stateDB.Close()
		return nil, fmt.Errorf("failed to create state table: %w", err)
	}

	// Check for existing pool
	tx, err := stateDB.BeginTx(ctx, nil)
	if err != nil {
		stateDB.Close()
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	state, err := GetPoolState(ctx, tx, config.PoolID)
	if err != nil {
		stateDB.Close()
		return nil, fmt.Errorf("failed to get pool state: %w", err)
	}

	// If new pool, insert configuration
	if state == nil {
		if err := InsertPoolState(ctx, tx, config.PoolID, config.MaxPoolSize); err != nil {
			stateDB.Close()
			return nil, fmt.Errorf("failed to insert pool state: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		stateDB.Close()
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &Pool{
		Config:         config,
		StateDB:        stateDB,
		TemplateExists: state != nil,
	}, nil
}

// Acquire gets a database from the pool (automatically releases via testing.T.Cleanup)
func (p *Pool) Acquire(t *testing.T) (*sql.DB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), p.Config.AcquireTimeout)
	defer cancel()

	// Start transaction with timeout
	tx, err := p.StateDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Acquire pool state lock
	state, err := GetPoolState(ctx, tx, p.Config.PoolID)
	if err != nil {
		return nil, fmt.Errorf("failed to get pool state: %w", err)
	}

	if state == nil {
		return nil, fmt.Errorf("pool state not found for pool_id: %s", p.Config.PoolID)
	}

	// Create template database on first acquire
	if !p.TemplateExists {
		templateDB := state.TemplateDB
		exists, err := DatabaseExists(ctx, p.Config.RootConnection, templateDB)
		if err != nil {
			return nil, fmt.Errorf("failed to check template database existence: %w", err)
		}

		if !exists {
			// Create template database
			createQuery := fmt.Sprintf("CREATE DATABASE %s", templateDB)
			if _, err := p.Config.RootConnection.ExecContext(ctx, createQuery); err != nil {
				return nil, fmt.Errorf("failed to create template database: %w", err)
			}

			// Connect to template database and run template creator
			templateConnStr := GetConnectionString(p.Config.RootConnection, templateDB)
			templateConn, err := sql.Open("postgres", templateConnStr)
			if err != nil {
				return nil, fmt.Errorf("failed to connect to template database: %w", err)
			}

			if err := p.Config.TemplateCreator(ctx, templateConn); err != nil {
				templateConn.Close()
				return nil, fmt.Errorf("failed to execute template creator: %w", err)
			}
			
			// Close the connection to ensure template can be used
			if err := templateConn.Close(); err != nil {
				return nil, fmt.Errorf("failed to close template database connection: %w", err)
			}
		}

		p.TemplateExists = true
	}

	var dbName string

	// Check for available databases
	if len(state.AvailableDBs) > 0 {
		// Take from available pool
		dbName = state.AvailableDBs[0]
		state.AvailableDBs = state.AvailableDBs[1:]
		state.InUseDBs = append(state.InUseDBs, dbName)
	} else if len(state.InUseDBs)+len(state.FailedDBs) < state.MaxPoolSize {
		// Create new database
		dbNum := len(state.InUseDBs) + len(state.FailedDBs) + len(state.AvailableDBs) + 1
		dbName = fmt.Sprintf("%s_%d", p.Config.PoolID, dbNum)

		// Create database from template
		if err := CreateDatabase(ctx, p.Config.RootConnection, dbName, state.TemplateDB); err != nil {
			return nil, fmt.Errorf("failed to create database %s: %w", dbName, err)
		}

		state.InUseDBs = append(state.InUseDBs, dbName)
	} else {
		// Pool exhausted
		return nil, fmt.Errorf("pool exhausted: max size %d reached", state.MaxPoolSize)
	}

	// Update state
	if err := UpdatePoolState(ctx, tx, state); err != nil {
		return nil, fmt.Errorf("failed to update pool state: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Connect to the acquired database
	dbConnStr := GetConnectionString(p.Config.RootConnection, dbName)
	db, err := sql.Open("postgres", dbConnStr)
	if err != nil {
		// If we fail to connect, we should move the database back to available
		p.ReleaseDatabase(dbName, false)
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
			p.ReleaseDatabase(dbName, true)
			return
		}
		defer resetDB.Close()

		resetSuccess := false
		if err := p.Config.ResetFunc(resetCtx, resetDB); err != nil {
			t.Logf("reset function failed for database %s: %v", dbName, err)
		} else {
			resetSuccess = true
		}

		// Release the database back to pool
		p.ReleaseDatabase(dbName, !resetSuccess)
	})

	return db, nil
}

// ReleaseDatabase releases a database back to the pool
func (p *Pool) ReleaseDatabase(dbName string, failed bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tx, err := p.StateDB.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("failed to begin transaction for release: %v", err)
		return
	}
	defer tx.Rollback()

	state, err := GetPoolState(ctx, tx, p.Config.PoolID)
	if err != nil {
		log.Printf("failed to get pool state for release: %v", err)
		return
	}

	if state == nil {
		log.Printf("pool state not found for release")
		return
	}

	// Remove from in_use_dbs
	state.InUseDBs = RemoveFromSlice(state.InUseDBs, dbName)

	// Add to appropriate list
	if failed {
		state.FailedDBs = append(state.FailedDBs, dbName)
	} else {
		state.AvailableDBs = append(state.AvailableDBs, dbName)
	}

	if err := UpdatePoolState(ctx, tx, state); err != nil {
		log.Printf("failed to update pool state for release: %v", err)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("failed to commit release transaction: %v", err)
	}
}