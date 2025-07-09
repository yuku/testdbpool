package testdbpool

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/yuku/testdbpool/internal/testhelper"
)

func TestNew(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	pool := testhelper.GetTestDBPool(t)

	t.Run("ValidConfig", func(t *testing.T) {
		config := &Config{
			PoolID:       "test-new-valid",
			DBPool:       pool,
			MaxDatabases: 2,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				return nil
			},
			ResetDatabase: func(ctx context.Context, conn *pgx.Conn) error {
				return nil
			},
		}

		testPool, err := New(ctx, config)
		if err != nil {
			t.Fatalf("failed to create pool: %v", err)
		}

		// Verify pool was created
		if testPool.config != config {
			t.Error("config mismatch")
		}
		if testPool.numpool == nil {
			t.Error("numpool should be initialized")
		}
		if testPool.templateDB == "" {
			t.Error("templateDB should be set")
		}
		if testPool.databaseNames == nil {
			t.Error("databaseNames map should be initialized")
		}
	})

	t.Run("InvalidConfig", func(t *testing.T) {
		// Test with nil config
		_, err := New(ctx, nil)
		if err == nil {
			t.Error("expected error for nil config")
		}

		// Test with invalid config
		invalidConfig := &Config{
			PoolID: "test-new-invalid",
			// Missing required fields
		}
		_, err = New(ctx, invalidConfig)
		if err == nil {
			t.Error("expected error for invalid config")
		}
	})

	t.Run("TemplateNaming", func(t *testing.T) {
		poolID := "test-template-name"
		config := &Config{
			PoolID:       poolID,
			DBPool:       pool,
			MaxDatabases: 1,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				return nil
			},
			ResetDatabase: func(ctx context.Context, conn *pgx.Conn) error {
				return nil
			},
		}

		testPool, err := New(ctx, config)
		if err != nil {
			t.Fatalf("failed to create pool: %v", err)
		}

		expectedTemplate := "testdb_template_" + poolID
		if testPool.templateDB != expectedTemplate {
			t.Errorf("expected template name %q, got %q", expectedTemplate, testPool.templateDB)
		}
	})
}

