package testdbpool

import (
	"context"
	"fmt"

	"github.com/yuku/numpool"
)

// Pool manages a pool of test databases.
type Pool struct {
	config       *Config
	numpool      *numpool.Pool
	templateDB   string
	databaseNames map[int]string // maps resource index to database name
}

// New creates a new test database pool.
func New(ctx context.Context, config *Config) (*Pool, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// TODO: Implement pool creation
	return nil, fmt.Errorf("not implemented")
}