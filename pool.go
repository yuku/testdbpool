package testdbpool

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/yuku/testdbpool/internal"
)

// Pool manages database pools
type Pool struct {
	internal *internal.Pool
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
	// Convert to internal configuration
	internalConfig := internal.Configuration{
		RootConnection:  config.RootConnection,
		StateDatabase:   config.StateDatabase,
		PoolID:          config.PoolID,
		MaxPoolSize:     config.MaxPoolSize,
		AcquireTimeout:  config.AcquireTimeout,
		TemplateCreator: config.TemplateCreator,
		ResetFunc:       config.ResetFunc,
	}

	internalPool, err := internal.New(internalConfig)
	if err != nil {
		return nil, err
	}

	return &Pool{
		internal: internalPool,
	}, nil
}

// Acquire gets a database from the pool (automatically releases via testing.T.Cleanup)
func (p *Pool) Acquire(t *testing.T) (*sql.DB, error) {
	return p.internal.Acquire(t)
}