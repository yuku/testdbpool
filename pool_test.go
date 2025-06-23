package testdbpool_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/yuku/testdbpool"
)

// Helper function to get test database connection
func getTestRootDB(t *testing.T) *sql.DB {
	t.Helper()
	
	connStr := os.Getenv("TEST_DATABASE_URL")
	if connStr == "" {
		host := getEnvOrDefault("PGHOST", "localhost")
		port := getEnvOrDefault("PGPORT", "5432")
		user := getEnvOrDefault("PGUSER", "postgres")
		password := getEnvOrDefault("PGPASSWORD", "postgres")
		
		if password != "" {
			connStr = fmt.Sprintf("postgres://%s:%s@%s:%s/postgres?sslmode=disable",
				user, password, host, port)
		} else {
			connStr = fmt.Sprintf("postgres://%s@%s:%s/postgres?sslmode=disable",
				user, host, port)
		}
	}
	
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	
	if err := db.Ping(); err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	
	return db
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Template creator function for tests
func createTestSchema(ctx context.Context, db *sql.DB) error {
	queries := []string{
		`CREATE TABLE users (
			id SERIAL PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			email VARCHAR(100) UNIQUE NOT NULL
		)`,
		`CREATE TABLE posts (
			id SERIAL PRIMARY KEY,
			user_id INTEGER REFERENCES users(id),
			title VARCHAR(200) NOT NULL,
			content TEXT
		)`,
		`INSERT INTO users (name, email) VALUES 
			('Test User 1', 'test1@example.com'),
			('Test User 2', 'test2@example.com')`,
	}
	
	for _, query := range queries {
		if _, err := db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to execute query: %w", err)
		}
	}
	
	return nil
}

func TestNew(t *testing.T) {
	rootDB := getTestRootDB(t)
	defer rootDB.Close()
	
	tests := []struct {
		name    string
		config  testdbpool.Configuration
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid configuration",
			config: testdbpool.Configuration{
				RootConnection:  rootDB,
				PoolID:          "test_pool_1",
				TemplateCreator: createTestSchema,
				ResetFunc:       testdbpool.ResetByTruncate([]string{"posts", "users"}, nil),
			},
			wantErr: false,
		},
		{
			name: "nil root connection",
			config: testdbpool.Configuration{
				RootConnection:  nil,
				PoolID:          "test_pool_2",
				TemplateCreator: createTestSchema,
				ResetFunc:       testdbpool.ResetByTruncate([]string{"posts", "users"}, nil),
			},
			wantErr: true,
			errMsg:  "RootConnection must not be nil",
		},
		{
			name: "empty pool ID",
			config: testdbpool.Configuration{
				RootConnection:  rootDB,
				PoolID:          "",
				TemplateCreator: createTestSchema,
				ResetFunc:       testdbpool.ResetByTruncate([]string{"posts", "users"}, nil),
			},
			wantErr: true,
			errMsg:  "PoolID must not be empty",
		},
		{
			name: "invalid pool ID characters",
			config: testdbpool.Configuration{
				RootConnection:  rootDB,
				PoolID:          "test-pool-invalid",
				TemplateCreator: createTestSchema,
				ResetFunc:       testdbpool.ResetByTruncate([]string{"posts", "users"}, nil),
			},
			wantErr: true,
			errMsg:  "PoolID must contain only alphanumeric characters and underscores",
		},
		{
			name: "pool ID too long",
			config: testdbpool.Configuration{
				RootConnection:  rootDB,
				PoolID:          "this_is_a_very_long_pool_id_that_exceeds_fifty_characters_limit",
				TemplateCreator: createTestSchema,
				ResetFunc:       testdbpool.ResetByTruncate([]string{"posts", "users"}, nil),
			},
			wantErr: true,
			errMsg:  "PoolID must be 50 characters or less",
		},
		{
			name: "nil template creator",
			config: testdbpool.Configuration{
				RootConnection:  rootDB,
				PoolID:          "test_pool_3",
				TemplateCreator: nil,
				ResetFunc:       testdbpool.ResetByTruncate([]string{"posts", "users"}, nil),
			},
			wantErr: true,
			errMsg:  "TemplateCreator must not be nil",
		},
		{
			name: "nil reset function",
			config: testdbpool.Configuration{
				RootConnection:  rootDB,
				PoolID:          "test_pool_4",
				TemplateCreator: createTestSchema,
				ResetFunc:       nil,
			},
			wantErr: true,
			errMsg:  "ResetFunc must not be nil",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up before test
			if tt.config.PoolID != "" && !tt.wantErr {
				testdbpool.Cleanup(rootDB, tt.config.PoolID)
			}
			
			pool, err := testdbpool.New(tt.config)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("expected error %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if pool == nil {
					t.Error("expected pool to be non-nil")
				}
			}
			
			// Clean up after test
			if pool != nil && tt.config.PoolID != "" {
				testdbpool.Cleanup(rootDB, tt.config.PoolID)
			}
		})
	}
}

func TestAcquire(t *testing.T) {
	rootDB := getTestRootDB(t)
	defer rootDB.Close()
	
	poolID := "test_acquire_pool"
	
	// Clean up before test
	testdbpool.Cleanup(rootDB, poolID)
	
	pool, err := testdbpool.New(testdbpool.Configuration{
		RootConnection:  rootDB,
		PoolID:          poolID,
		MaxPoolSize:     3,
		TemplateCreator: createTestSchema,
		ResetFunc: testdbpool.ResetByTruncate([]string{"posts", "users"}, func(ctx context.Context, db *sql.DB) error {
			_, err := db.ExecContext(ctx, `INSERT INTO users (name, email) VALUES 
				('Test User 1', 'test1@example.com'),
				('Test User 2', 'test2@example.com')`)
			return err
		}),
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	
	defer testdbpool.Cleanup(rootDB, poolID)
	
	t.Run("single acquire and release", func(t *testing.T) {
		db, err := pool.Acquire(t)
		if err != nil {
			t.Fatalf("failed to acquire database: %v", err)
		}
		
		// Verify we can use the database
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
		if err != nil {
			t.Errorf("failed to query users: %v", err)
		}
		if count != 2 {
			t.Errorf("expected 2 users, got %d", count)
		}
		
		// Insert a new user
		_, err = db.Exec("INSERT INTO users (name, email) VALUES ($1, $2)",
			"New User", "new@example.com")
		if err != nil {
			t.Errorf("failed to insert user: %v", err)
		}
	})
	
	t.Run("verify reset worked", func(t *testing.T) {
		db, err := pool.Acquire(t)
		if err != nil {
			t.Fatalf("failed to acquire database: %v", err)
		}
		
		// Verify the database was reset
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
		if err != nil {
			t.Errorf("failed to query users: %v", err)
		}
		if count != 2 {
			t.Errorf("expected 2 users after reset, got %d", count)
		}
	})
}

func TestConcurrentAcquire(t *testing.T) {
	rootDB := getTestRootDB(t)
	defer rootDB.Close()
	
	poolID := "test_concurrent_pool"
	
	// Clean up before test
	testdbpool.Cleanup(rootDB, poolID)
	
	pool, err := testdbpool.New(testdbpool.Configuration{
		RootConnection:  rootDB,
		PoolID:          poolID,
		MaxPoolSize:     5,
		TemplateCreator: createTestSchema,
		ResetFunc:       testdbpool.ResetByTruncate([]string{"posts", "users"}, nil),
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	
	defer testdbpool.Cleanup(rootDB, poolID)
	
	// Run multiple tests concurrently
	var wg sync.WaitGroup
	errors := make(chan error, 10)
	
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			
			// Create a sub-test for each goroutine
			testName := fmt.Sprintf("concurrent_%d", i)
			t.Run(testName, func(t *testing.T) {
				db, err := pool.Acquire(t)
				if err != nil {
					errors <- fmt.Errorf("goroutine %d: failed to acquire: %w", i, err)
					return
				}
				
				// Simulate some work
				time.Sleep(100 * time.Millisecond)
				
				// Verify we can use the database
				var result int
				err = db.QueryRow("SELECT 1").Scan(&result)
				if err != nil {
					errors <- fmt.Errorf("goroutine %d: failed to query: %w", i, err)
					return
				}
			})
		}(i)
	}
	
	wg.Wait()
	close(errors)
	
	// Check for errors
	for err := range errors {
		t.Error(err)
	}
}

func TestPoolExhaustion(t *testing.T) {
	rootDB := getTestRootDB(t)
	defer rootDB.Close()
	
	poolID := "test_exhaustion_pool"
	
	// Clean up before test
	testdbpool.Cleanup(rootDB, poolID)
	
	pool, err := testdbpool.New(testdbpool.Configuration{
		RootConnection:  rootDB,
		PoolID:          poolID,
		MaxPoolSize:     2,
		AcquireTimeout:  2 * time.Second,
		TemplateCreator: createTestSchema,
		ResetFunc:       testdbpool.ResetByTruncate([]string{"posts", "users"}, nil),
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	
	defer testdbpool.Cleanup(rootDB, poolID)
	
	// Acquire all available databases
	t.Run("acquire_first", func(t *testing.T) {
		_, err := pool.Acquire(t)
		if err != nil {
			t.Errorf("failed to acquire first database: %v", err)
		}
	})
	
	t.Run("acquire_second", func(t *testing.T) {
		_, err := pool.Acquire(t)
		if err != nil {
			t.Errorf("failed to acquire second database: %v", err)
		}
	})
	
	// Try to acquire one more (should fail)
	t.Run("acquire_exhausted", func(t *testing.T) {
		// Set a short deadline for this test
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		
		done := make(chan struct{})
		go func() {
			_, err := pool.Acquire(t)
			if err == nil {
				t.Error("expected error when pool exhausted, got nil")
			} else if !containsString(err.Error(), "pool exhausted") {
				t.Errorf("expected pool exhausted error, got: %v", err)
			}
			close(done)
		}()
		
		select {
		case <-done:
			// Test completed
		case <-ctx.Done():
			t.Error("test timed out waiting for pool exhaustion error")
		}
	})
}

func TestCleanup(t *testing.T) {
	rootDB := getTestRootDB(t)
	defer rootDB.Close()
	
	poolID := "test_cleanup_pool"
	
	// Create a pool
	pool, err := testdbpool.New(testdbpool.Configuration{
		RootConnection:  rootDB,
		PoolID:          poolID,
		TemplateCreator: createTestSchema,
		ResetFunc:       testdbpool.ResetByTruncate([]string{"posts", "users"}, nil),
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	
	// Acquire a database to ensure some are created
	t.Run("acquire", func(t *testing.T) {
		_, err := pool.Acquire(t)
		if err != nil {
			t.Errorf("failed to acquire database: %v", err)
		}
	})
	
	// Clean up the pool
	err = testdbpool.Cleanup(rootDB, poolID)
	if err != nil {
		t.Errorf("failed to cleanup pool: %v", err)
	}
	
	// Verify template database was dropped
	var exists bool
	err = rootDB.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)",
		poolID+"_template",
	).Scan(&exists)
	if err != nil {
		t.Errorf("failed to check database existence: %v", err)
	}
	if exists {
		t.Error("template database still exists after cleanup")
	}
	
	// Verify pool state was removed
	var count int
	err = rootDB.QueryRow(
		"SELECT COUNT(*) FROM testdbpool_state WHERE pool_id = $1",
		poolID,
	).Scan(&count)
	if err != nil {
		t.Errorf("failed to check pool state: %v", err)
	}
	if count > 0 {
		t.Error("pool state still exists after cleanup")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && s[len(s)-len(substr):] == substr || 
	       len(s) >= len(substr) && s[:len(substr)] == substr ||
	       len(substr) < len(s) && containsString(s[1:], substr)
}