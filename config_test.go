package testdbpool

import (
	"context"
	"runtime"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/yuku/numpool"
)

func TestConfig_Validate(t *testing.T) {
	// Helper functions for test setup
	validSetupTemplate := func(ctx context.Context, conn *pgx.Conn) error {
		return nil
	}

	tests := []struct {
		name      string
		config    Config
		wantErr   bool
		errMsg    string
		checkFunc func(*testing.T, *Config) // Additional checks after validation
	}{
		{
			name: "valid config",
			config: Config{
				ID:            "test-pool",
				Pool:          &pgxpool.Pool{}, // Mock pool for testing
				MaxDatabases:  5,
				SetupTemplate: validSetupTemplate,
			},
			wantErr: false,
		},
		{
			name: "empty ID",
			config: Config{
				ID:            "",
				Pool:          &pgxpool.Pool{},
				MaxDatabases:  5,
				SetupTemplate: validSetupTemplate,
			},
			wantErr: true,
			errMsg:  "ID is required",
		},
		{
			name: "nil pool",
			config: Config{
				ID:            "test-pool",
				Pool:          nil,
				MaxDatabases:  5,
				SetupTemplate: validSetupTemplate,
			},
			wantErr: true,
			errMsg:  "pool is required",
		},
		{
			name: "zero MaxDatabases applies default",
			config: Config{
				ID:            "test-pool",
				Pool:          &pgxpool.Pool{},
				MaxDatabases:  0, // Should be set to default
				SetupTemplate: validSetupTemplate,
			},
			wantErr: false,
			checkFunc: func(t *testing.T, c *Config) {
				expectedMax := min(runtime.GOMAXPROCS(0), numpool.MaxResourcesLimit)
				assert.Equal(t, expectedMax, c.MaxDatabases, "MaxDatabases should be set to default value")
			},
		},
		{
			name: "negative MaxDatabases",
			config: Config{
				ID:            "test-pool",
				Pool:          &pgxpool.Pool{},
				MaxDatabases:  -1,
				SetupTemplate: validSetupTemplate,
			},
			wantErr: true,
			errMsg:  "MaxDatabases must be between 1 and 64, got -1",
		},
		{
			name: "MaxDatabases exceeds limit",
			config: Config{
				ID:            "test-pool",
				Pool:          &pgxpool.Pool{},
				MaxDatabases:  numpool.MaxResourcesLimit + 1,
				SetupTemplate: validSetupTemplate,
			},
			wantErr: true,
			errMsg:  "MaxDatabases must be between 1 and 64, got 65",
		},
		{
			name: "MaxDatabases at maximum limit",
			config: Config{
				ID:            "test-pool",
				Pool:          &pgxpool.Pool{},
				MaxDatabases:  numpool.MaxResourcesLimit,
				SetupTemplate: validSetupTemplate,
			},
			wantErr: false,
		},
		{
			name: "MaxDatabases at minimum valid value",
			config: Config{
				ID:            "test-pool",
				Pool:          &pgxpool.Pool{},
				MaxDatabases:  1,
				SetupTemplate: validSetupTemplate,
			},
			wantErr: false,
		},
		{
			name: "nil SetupTemplate",
			config: Config{
				ID:            "test-pool",
				Pool:          &pgxpool.Pool{},
				MaxDatabases:  5,
				SetupTemplate: nil,
			},
			wantErr: true,
			errMsg:  "SetupTemplate function is required",
		},
		{
			name: "all fields nil except ID",
			config: Config{
				ID:            "test-pool",
				Pool:          nil,
				MaxDatabases:  0,
				SetupTemplate: nil,
			},
			wantErr: true,
			errMsg:  "pool is required", // First validation error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy to avoid modifying the original test case
			config := tt.config

			err := config.Validate()

			if tt.wantErr {
				assert.Error(t, err, "Expected validation to fail")
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Expected validation to pass")

				// Run additional checks if provided
				if tt.checkFunc != nil {
					tt.checkFunc(t, &config)
				}
			}
		})
	}
}

func TestConfig_Validate_DefaultMaxDatabases(t *testing.T) {
	// Test the default value calculation separately to ensure it's working correctly
	validSetupTemplate := func(ctx context.Context, conn *pgx.Conn) error {
		return nil
	}

	config := Config{
		ID:            "test-default",
		Pool:          &pgxpool.Pool{},
		MaxDatabases:  0, // Should trigger default calculation
		SetupTemplate: validSetupTemplate,
	}

	err := config.Validate()
	assert.NoError(t, err)

	// Verify the default was applied correctly
	expectedDefault := min(runtime.GOMAXPROCS(0), numpool.MaxResourcesLimit)
	assert.Equal(t, expectedDefault, config.MaxDatabases)
	assert.True(t, config.MaxDatabases >= 1, "Default MaxDatabases should be at least 1")
	assert.True(t, config.MaxDatabases <= numpool.MaxResourcesLimit, "Default MaxDatabases should not exceed limit")
}

// TestConfig_Validate_EdgeCases tests edge cases for MaxDatabases validation
func TestConfig_Validate_EdgeCases(t *testing.T) {
	validSetupTemplate := func(ctx context.Context, conn *pgx.Conn) error {
		return nil
	}

	testCases := []struct {
		name         string
		maxDatabases int
		expectError  bool
	}{
		{"zero value", 0, false}, // Should apply default
		{"minimum valid", 1, false},
		{"maximum valid", numpool.MaxResourcesLimit, false},
		{"just above maximum", numpool.MaxResourcesLimit + 1, true},
		{"large invalid value", 1000, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := Config{
				ID:            "test-edge-case",
				Pool:          &pgxpool.Pool{},
				MaxDatabases:  tc.maxDatabases,
				SetupTemplate: validSetupTemplate,
			}

			err := config.Validate()
			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
