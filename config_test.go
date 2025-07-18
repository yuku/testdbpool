package testdbpool

import (
	"context"
	"runtime"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/yuku/numpool"
)

func TestConfig_Validate(t *testing.T) {
	// Mock setup and reset functions
	mockSetup := func(ctx context.Context, pool *pgxpool.Pool) error { return nil }
	mockReset := func(ctx context.Context, pool *pgxpool.Pool) error { return nil }

	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: Config{
				PoolID:        "test-pool",
				DBPool:        &pgxpool.Pool{}, // Mock pool
				MaxDatabases:  10,
				SetupTemplate: mockSetup,
				ResetDatabase: mockReset,
			},
			wantErr: false,
		},
		{
			name: "missing PoolID",
			config: Config{
				DBPool:        &pgxpool.Pool{},
				MaxDatabases:  10,
				SetupTemplate: mockSetup,
				ResetDatabase: mockReset,
			},
			wantErr: true,
			errMsg:  "PoolID is required",
		},
		{
			name: "missing DBPool",
			config: Config{
				PoolID:        "test-pool",
				MaxDatabases:  10,
				SetupTemplate: mockSetup,
				ResetDatabase: mockReset,
			},
			wantErr: true,
			errMsg:  "DBPool is required",
		},
		{
			name: "MaxDatabases defaults to GOMAXPROCS",
			config: Config{
				PoolID:        "test-pool",
				DBPool:        &pgxpool.Pool{},
				MaxDatabases:  0, // Should default to min(GOMAXPROCS, 64)
				SetupTemplate: mockSetup,
				ResetDatabase: mockReset,
			},
			wantErr: false,
		},
		{
			name: "MaxDatabases too large",
			config: Config{
				PoolID:        "test-pool",
				DBPool:        &pgxpool.Pool{},
				MaxDatabases:  numpool.MaxResourcesLimit + 1,
				SetupTemplate: mockSetup,
				ResetDatabase: mockReset,
			},
			wantErr: true,
			errMsg:  "MaxDatabases must be between 1 and 64, got 65",
		},
		{
			name: "missing SetupTemplate",
			config: Config{
				PoolID:        "test-pool",
				DBPool:        &pgxpool.Pool{},
				MaxDatabases:  10,
				ResetDatabase: mockReset,
			},
			wantErr: true,
			errMsg:  "SetupTemplate function is required",
		},
		{
			name: "missing ResetDatabase",
			config: Config{
				PoolID:        "test-pool",
				DBPool:        &pgxpool.Pool{},
				MaxDatabases:  10,
				SetupTemplate: mockSetup,
			},
			wantErr: true,
			errMsg:  "ResetDatabase function is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				} else if err.Error() != tt.errMsg {
					t.Errorf("expected error %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestConfig_ValidateDefaultMaxDatabases(t *testing.T) {
	// Mock setup and reset functions
	mockSetup := func(ctx context.Context, pool *pgxpool.Pool) error { return nil }
	mockReset := func(ctx context.Context, pool *pgxpool.Pool) error { return nil }

	// Test that MaxDatabases defaults to min(GOMAXPROCS, 64)
	config := Config{
		PoolID:        "test-pool",
		DBPool:        &pgxpool.Pool{},
		MaxDatabases:  0, // Should be set to default
		SetupTemplate: mockSetup,
		ResetDatabase: mockReset,
	}

	err := config.Validate()
	if err != nil {
		require.NoError(t, err, "unexpected error")
	}

	// Check that MaxDatabases was set to the expected default
	expectedMax := runtime.GOMAXPROCS(0)
	if expectedMax > numpool.MaxResourcesLimit {
		expectedMax = numpool.MaxResourcesLimit
	}

	if config.MaxDatabases != expectedMax {
		t.Errorf("expected MaxDatabases to be %d, got %d", expectedMax, config.MaxDatabases)
	}

	// Test with a high GOMAXPROCS value
	// Save current GOMAXPROCS
	oldGOMAXPROCS := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(oldGOMAXPROCS)

	// Set GOMAXPROCS to a high value
	runtime.GOMAXPROCS(100)

	config2 := Config{
		PoolID:        "test-pool-2",
		DBPool:        &pgxpool.Pool{},
		MaxDatabases:  0,
		SetupTemplate: mockSetup,
		ResetDatabase: mockReset,
	}

	err = config2.Validate()
	if err != nil {
		require.NoError(t, err, "unexpected error")
	}

	// Should be capped at numpool.MaxResourcesLimit
	if config2.MaxDatabases != numpool.MaxResourcesLimit {
		t.Errorf("expected MaxDatabases to be capped at %d, got %d", numpool.MaxResourcesLimit, config2.MaxDatabases)
	}
}
