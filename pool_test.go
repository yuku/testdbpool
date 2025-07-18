package testdbpool_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/yuku/testdbpool"
	"github.com/yuku/testdbpool/internal/testhelper"
)

func TestNew(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	pool := testhelper.GetTestDBPool(t)

	t.Run("ValidConfig", func(t *testing.T) {
		config := &testdbpool.Config{
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

		testPool, err := testdbpool.New(ctx, config)
		require.NoError(t, err, "failed to create test database pool")

		// Verify pool was created
		require.Same(t, testPool.Config(), config, "expected same config")
		require.NotNil(t, testPool.Numpool(), "expected numpool to be initialized")
		require.NotEmpty(t, testPool.TemplateDB(), "expected templateDB to be set")
		require.NotNil(t, testPool.DatabaseNames(), "expected databaseNames map to be initialized")
	})

	t.Run("returns error if nil is given", func(t *testing.T) {
		_, err := testdbpool.New(ctx, nil)
		require.Error(t, err, "expected error for nil config")
	})

	t.Run("returns error if invalid config is given", func(t *testing.T) {
		invalidConfig := &testdbpool.Config{
			PoolID: "test-new-invalid",
			// Missing required fields
		}
		_, err := testdbpool.New(ctx, invalidConfig)
		require.Error(t, err, "expected error for invalid config")
	})

	t.Run("TemplateNaming", func(t *testing.T) {
		poolID := "test-template-name"
		config := &testdbpool.Config{
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

		testPool, err := testdbpool.New(ctx, config)
		require.NoError(t, err, "failed to create test database pool")
		require.Equal(t, "testdb_template_"+poolID, testPool.TemplateDB())
	})
}
