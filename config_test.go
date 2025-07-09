package testdbpool

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConfig_Validate(t *testing.T) {
	// Mock setup and reset functions
	mockSetup := func(ctx context.Context, conn *pgx.Conn) error { return nil }
	mockReset := func(ctx context.Context, conn *pgx.Conn) error { return nil }

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
			name: "MaxDatabases too small",
			config: Config{
				PoolID:        "test-pool",
				DBPool:        &pgxpool.Pool{},
				MaxDatabases:  0,
				SetupTemplate: mockSetup,
				ResetDatabase: mockReset,
			},
			wantErr: true,
			errMsg:  "MaxDatabases must be between 1 and 64, got 0",
		},
		{
			name: "MaxDatabases too large",
			config: Config{
				PoolID:        "test-pool",
				DBPool:        &pgxpool.Pool{},
				MaxDatabases:  65,
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