package testdbpool

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Conn: getRootConnection(t),
				SetupTemplate: func(_ context.Context, _ *pgx.Conn) error {
					// Example setup function that does nothing
					return nil
				},
			},
			wantErr: false,
		},
		{
			name: "missing connection",
			config: Config{
				SetupTemplate: func(_ context.Context, _ *pgx.Conn) error {
					return nil
				},
			},
			wantErr: true,
		},
		{
			name: "missing setup function",
			config: Config{
				Conn: getRootConnection(t),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
