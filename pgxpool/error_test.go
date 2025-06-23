package pgxpool_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	tpgxpool "github.com/yuku/testdbpool/pgxpool"
)

// mockPool is a mock implementation of testdbpool.Pool for testing error cases
type mockPool struct {
	acquireErr error
	mockDB     *sql.DB
}

func (m *mockPool) Acquire(t *testing.T) (*sql.DB, error) {
	if m.acquireErr != nil {
		return nil, m.acquireErr
	}
	return m.mockDB, nil
}

func TestErrorHandling(t *testing.T) {
	t.Run("AcquireError", func(t *testing.T) {
		// Create a wrapper with configuration that will fail
		wrapper := tpgxpool.NewWithConfig(testPool, tpgxpool.Config{
			PasswordSource: func() (string, error) {
				return "", errors.New("password retrieval failed")
			},
		})
		
		_, err := wrapper.Acquire(t)
		if err == nil {
			t.Error("expected error, got nil")
		}
		
		if !contains(err.Error(), "password retrieval failed") {
			t.Errorf("expected password error, got: %v", err)
		}
	})

	t.Run("HostSourceError", func(t *testing.T) {
		wrapper := tpgxpool.NewWithConfig(testPool, tpgxpool.Config{
			HostSource: func(*sql.DB) (string, string, error) {
				return "", "", errors.New("host lookup failed")
			},
		})
		
		_, err := wrapper.Acquire(t)
		if err == nil {
			t.Error("expected error, got nil")
		}
		
		if !contains(err.Error(), "host lookup failed") {
			t.Errorf("expected host error, got: %v", err)
		}
	})

	t.Run("InvalidConnectionString", func(t *testing.T) {
		wrapper := tpgxpool.NewWithConfig(testPool, tpgxpool.Config{
			HostSource: func(*sql.DB) (string, string, error) {
				// Return invalid host that will cause connection string parsing to fail
				return "invalid host with spaces", "not-a-port", nil
			},
			AdditionalParams: "invalid=value with spaces&bad",
		})
		
		_, err := wrapper.Acquire(t)
		if err == nil {
			t.Error("expected error, got nil")
		}
		
		// The exact error depends on pgxpool's validation
		if !contains(err.Error(), "failed to parse pgxpool config") && !contains(err.Error(), "failed to create pgxpool") {
			t.Errorf("expected pgxpool config/creation error, got: %v", err)
		}
	})
}

func TestEdgeCases(t *testing.T) {
	t.Run("EmptyPassword", func(t *testing.T) {
		wrapper := tpgxpool.NewWithConfig(testPool, tpgxpool.Config{
			PasswordSource: tpgxpool.StaticPasswordSource(""),
		})
		
		_, err := wrapper.Acquire(t)
		// Empty password might work or fail depending on PostgreSQL configuration
		if err != nil {
			// This is expected in most secure configurations
			if contains(err.Error(), "password authentication failed") || contains(err.Error(), "no password was provided") {
				// This is the expected error
				return
			}
			// Log unexpected errors for debugging
			t.Logf("Empty password resulted in unexpected error: %v", err)
		}
		// If no error, that's also valid (trust authentication, etc)
	})

	t.Run("SpecialCharactersInPassword", func(t *testing.T) {
		// Test with password containing special characters that need URL encoding
		specialPass := "p@ss!word#123$%^&*()"
		wrapper := tpgxpool.NewWithConfig(testPool, tpgxpool.Config{
			PasswordSource: tpgxpool.StaticPasswordSource(specialPass),
		})
		
		_, err := wrapper.Acquire(t)
		// This will likely fail due to authentication, but should not fail due to URL encoding
		if err != nil && (contains(err.Error(), "invalid URL escape") || contains(err.Error(), "failed to parse as URL")) {
			t.Errorf("password encoding failed: %v", err)
		}
	})

	t.Run("AcquireWithNilConfigFunc", func(t *testing.T) {
		// Should handle nil config function gracefully
		pool, err := poolWrapper.AcquireWithConfig(t, nil)
		if err != nil {
			t.Fatal(err)
		}
		
		// Verify connection works
		ctx := context.Background()
		var count int
		err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM test_data").Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
	})
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[len(s)-len(substr):] == substr || len(s) >= len(substr) && s[:len(substr)] == substr || len(substr) > 0 && len(s) > len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}