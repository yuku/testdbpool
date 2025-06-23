package testdbpool

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Pool manages database pools
type Pool struct {
	config         Configuration
	stateDB        *sql.DB
	templateExists bool
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
	if err := validateConfig(&config); err != nil {
		return nil, err
	}

	// Connect to state management database
	ctx := context.Background()
	stateConnStr := getConnectionString(config.RootConnection, config.StateDatabase)
	stateDB, err := sql.Open("postgres", stateConnStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to state database: %w", err)
	}

	// Create state management table
	if err := createStateTable(ctx, stateDB); err != nil {
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

	state, err := getPoolState(ctx, tx, config.PoolID)
	if err != nil {
		stateDB.Close()
		return nil, fmt.Errorf("failed to get pool state: %w", err)
	}

	// If new pool, insert configuration
	if state == nil {
		if err := insertPoolState(ctx, tx, config.PoolID, config.MaxPoolSize); err != nil {
			stateDB.Close()
			return nil, fmt.Errorf("failed to insert pool state: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		stateDB.Close()
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &Pool{
		config:         config,
		stateDB:        stateDB,
		templateExists: state != nil,
	}, nil
}

