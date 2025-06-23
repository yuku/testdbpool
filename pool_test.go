package testdbpool_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver (pgx)
	_ "github.com/lib/pq"              // PostgreSQL driver (lib/pq)
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
	defer func() { _ = rootDB.Close() }()

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
				ResetFunc:       testdbpool.ResetByTruncate([]string{}, nil), // truncate all tables
			},
			wantErr: false,
		},
		{
			name: "nil root connection",
			config: testdbpool.Configuration{
				RootConnection:  nil,
				PoolID:          "test_pool_2",
				TemplateCreator: createTestSchema,
				ResetFunc:       testdbpool.ResetByTruncate([]string{}, nil), // truncate all tables
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
				ResetFunc:       testdbpool.ResetByTruncate([]string{}, nil), // truncate all tables
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
				ResetFunc:       testdbpool.ResetByTruncate([]string{}, nil), // truncate all tables
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
				ResetFunc:       testdbpool.ResetByTruncate([]string{}, nil), // truncate all tables
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
				ResetFunc:       testdbpool.ResetByTruncate([]string{}, nil), // truncate all tables
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
				_ = testdbpool.Cleanup(rootDB, tt.config.PoolID)
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
				_ = testdbpool.Cleanup(rootDB, tt.config.PoolID)
			}
		})
	}
}

func TestAcquire(t *testing.T) {
	rootDB := getTestRootDB(t)
	defer func() { _ = rootDB.Close() }()

	poolID := "test_acquire_pool"

	// Clean up before test
	_ = testdbpool.Cleanup(rootDB, poolID)

	pool, err := testdbpool.New(testdbpool.Configuration{
		RootConnection:  rootDB,
		PoolID:          poolID,
		MaxPoolSize:     3,
		TemplateCreator: createTestSchema,
		ResetFunc: testdbpool.ResetByTruncate([]string{}, func(ctx context.Context, db *sql.DB) error {
			_, err := db.ExecContext(ctx, `INSERT INTO users (id, name, email) VALUES 
				(1, 'Test User 1', 'test1@example.com'),
				(2, 'Test User 2', 'test2@example.com');
			SELECT setval('users_id_seq', 2);`)
			return err
		}),
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}

	defer func() { _ = testdbpool.Cleanup(rootDB, poolID) }()

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
	defer func() { _ = rootDB.Close() }()

	poolID := "test_concurrent_pool"

	// Clean up before test
	_ = testdbpool.Cleanup(rootDB, poolID)

	pool, err := testdbpool.New(testdbpool.Configuration{
		RootConnection:  rootDB,
		PoolID:          poolID,
		MaxPoolSize:     5,
		TemplateCreator: createTestSchema,
		ResetFunc:       testdbpool.ResetByTruncate([]string{}, nil),
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}

	defer func() { _ = testdbpool.Cleanup(rootDB, poolID) }()

	// Run tests to verify concurrent access using goroutines
	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount := 0
	poolExhaustedCount := 0
	errors := []error{}

	// Create a parent context for all operations
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			// Create a helper testing.T that won't interfere with the parent
			success := t.Run(fmt.Sprintf("concurrent_%d", i), func(t *testing.T) {
				// Each sub-test gets its own connection
				db, err := pool.Acquire(t)
				if err != nil {
					mu.Lock()
					if containsString(err.Error(), "pool exhausted") {
						poolExhaustedCount++
					} else {
						errors = append(errors, fmt.Errorf("goroutine %d: %v", i, err))
					}
					mu.Unlock()
					return
				}

				mu.Lock()
				successCount++
				mu.Unlock()

				// Simulate some work
				select {
				case <-ctx.Done():
					return
				case <-time.After(500 * time.Millisecond):
				}

				// Verify we can use the database
				var res int
				err = db.QueryRowContext(ctx, "SELECT 1").Scan(&res)
				if err != nil {
					mu.Lock()
					errors = append(errors, fmt.Errorf("goroutine %d query failed: %v", i, err))
					mu.Unlock()
				}
			})

			if !success {
				mu.Lock()
				errors = append(errors, fmt.Errorf("goroutine %d test failed", i))
				mu.Unlock()
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Report any errors
	for _, err := range errors {
		t.Error(err)
	}

	// Verify results
	if successCount < 5 {
		t.Errorf("expected at least 5 successful acquisitions (pool size), got %d", successCount)
	}

	totalAttempts := successCount + poolExhaustedCount
	if totalAttempts != 10 {
		t.Errorf("expected 10 total attempts, got %d (success: %d, exhausted: %d)",
			totalAttempts, successCount, poolExhaustedCount)
	}
}

func TestPoolExhaustion(t *testing.T) {
	rootDB := getTestRootDB(t)
	defer func() { _ = rootDB.Close() }()

	poolID := "test_exhaustion_pool"

	// Clean up before test
	_ = testdbpool.Cleanup(rootDB, poolID)

	pool, err := testdbpool.New(testdbpool.Configuration{
		RootConnection:  rootDB,
		PoolID:          poolID,
		MaxPoolSize:     2,
		AcquireTimeout:  2 * time.Second,
		TemplateCreator: createTestSchema,
		ResetFunc:       testdbpool.ResetByTruncate([]string{}, nil),
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}

	defer func() { _ = testdbpool.Cleanup(rootDB, poolID) }()

	// Use a WaitGroup to ensure databases are held
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		db, err := pool.Acquire(t)
		if err != nil {
			t.Errorf("failed to acquire first database: %v", err)
			return
		}
		// Verify connection works
		var result int
		if err := db.QueryRow("SELECT 1").Scan(&result); err != nil {
			t.Errorf("failed to query first database: %v", err)
		}
		// Hold the database until test is done
		time.Sleep(1 * time.Second)
	}()

	go func() {
		defer wg.Done()
		db, err := pool.Acquire(t)
		if err != nil {
			t.Errorf("failed to acquire second database: %v", err)
			return
		}
		// Verify connection works
		var result int
		if err := db.QueryRow("SELECT 1").Scan(&result); err != nil {
			t.Errorf("failed to query second database: %v", err)
		}
		// Hold the database until test is done
		time.Sleep(1 * time.Second)
	}()

	// Wait a bit to ensure both databases are acquired
	time.Sleep(200 * time.Millisecond)

	// Now the pool should be exhausted
	// Try to acquire one more (should fail immediately)
	_, err = pool.Acquire(t)
	if err == nil {
		t.Error("expected error when pool exhausted, got nil")
	} else if !containsString(err.Error(), "pool exhausted") {
		t.Errorf("expected pool exhausted error, got: %v", err)
	}

	// Wait for goroutines to finish
	wg.Wait()
}

func TestCleanup(t *testing.T) {
	rootDB := getTestRootDB(t)
	defer func() { _ = rootDB.Close() }()

	poolID := "test_cleanup_pool"

	// Create a pool
	pool, err := testdbpool.New(testdbpool.Configuration{
		RootConnection:  rootDB,
		PoolID:          poolID,
		TemplateCreator: createTestSchema,
		ResetFunc:       testdbpool.ResetByTruncate([]string{}, nil),
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

// TestExcludeTablesDemo demonstrates the excludeTables functionality
func TestExcludeTablesDemo(t *testing.T) {
	rootDB := getTestRootDB(t)
	defer func() { _ = rootDB.Close() }()

	poolID := "exclude_demo_pool"
	_ = testdbpool.Cleanup(rootDB, poolID)

	pool, err := testdbpool.New(testdbpool.Configuration{
		RootConnection: rootDB,
		PoolID:         poolID,
		MaxPoolSize:    3,
		TemplateCreator: func(ctx context.Context, db *sql.DB) error {
			// Create tables including enum/static tables
			schema := `
				-- Static enum table (should be excluded from truncation)
				CREATE TABLE user_types (
					id SERIAL PRIMARY KEY,
					name VARCHAR(50) NOT NULL UNIQUE
				);

				-- Another static table
				CREATE TABLE categories (
					id SERIAL PRIMARY KEY,
					name VARCHAR(100) NOT NULL,
					description TEXT
				);

				-- Dynamic tables (will be truncated)
				CREATE TABLE users (
					id SERIAL PRIMARY KEY,
					name VARCHAR(100) NOT NULL,
					user_type_id INTEGER REFERENCES user_types(id)
				);

				CREATE TABLE posts (
					id SERIAL PRIMARY KEY,
					user_id INTEGER REFERENCES users(id),
					category_id INTEGER REFERENCES categories(id),
					title VARCHAR(200) NOT NULL,
					content TEXT
				);

				-- Insert static/enum data
				INSERT INTO user_types (name) VALUES ('admin'), ('user'), ('guest');
				INSERT INTO categories (name, description) VALUES 
					('Tech', 'Technology posts'),
					('News', 'News articles'),
					('Tutorial', 'How-to guides');

				-- Insert some test data
				INSERT INTO users (name, user_type_id) VALUES ('Alice', 1), ('Bob', 2);
				INSERT INTO posts (user_id, category_id, title, content) VALUES 
					(1, 1, 'Test Post', 'Test content'),
					(2, 2, 'Another Post', 'More content');
			`
			_, err := db.ExecContext(ctx, schema)
			return err
		},
		// Exclude static/enum tables from truncation
		ResetFunc: testdbpool.ResetByTruncate(
			[]string{"user_types", "categories"}, // exclude these static tables
			func(ctx context.Context, db *sql.DB) error {
				// Re-insert only dynamic data with explicit IDs and reset sequences
				_, err := db.ExecContext(ctx, `
					INSERT INTO users (id, name, user_type_id) VALUES (1, 'Alice', 1), (2, 'Bob', 2);
					INSERT INTO posts (id, user_id, category_id, title, content) VALUES 
						(1, 1, 1, 'Test Post', 'Test content'),
						(2, 2, 2, 'Another Post', 'More content');
					
					-- Reset sequences to continue from where we left off
					SELECT setval('users_id_seq', 2);
					SELECT setval('posts_id_seq', 2);
				`)
				return err
			},
		),
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}

	defer func() { _ = testdbpool.Cleanup(rootDB, poolID) }()

	t.Run("first use - verify static data exists", func(t *testing.T) {
		db, err := pool.Acquire(t)
		if err != nil {
			t.Fatal(err)
		}

		// Check that static tables have data
		var userTypeCount, categoryCount int
		err = db.QueryRow("SELECT COUNT(*) FROM user_types").Scan(&userTypeCount)
		if err != nil {
			t.Fatal(err)
		}
		err = db.QueryRow("SELECT COUNT(*) FROM categories").Scan(&categoryCount)
		if err != nil {
			t.Fatal(err)
		}

		if userTypeCount != 3 {
			t.Errorf("expected 3 user types, got %d", userTypeCount)
		}
		if categoryCount != 3 {
			t.Errorf("expected 3 categories, got %d", categoryCount)
		}

		// Add some data
		_, err = db.Exec("INSERT INTO users (name, user_type_id) VALUES ('Charlie', 3)")
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("second use - static data should still exist", func(t *testing.T) {
		db, err := pool.Acquire(t)
		if err != nil {
			t.Fatal(err)
		}

		// Static tables should still have their data (not truncated)
		var userTypeCount, categoryCount int
		err = db.QueryRow("SELECT COUNT(*) FROM user_types").Scan(&userTypeCount)
		if err != nil {
			t.Fatal(err)
		}
		err = db.QueryRow("SELECT COUNT(*) FROM categories").Scan(&categoryCount)
		if err != nil {
			t.Fatal(err)
		}

		// These should be preserved
		if userTypeCount != 3 {
			t.Errorf("expected 3 user types (preserved), got %d", userTypeCount)
		}
		if categoryCount != 3 {
			t.Errorf("expected 3 categories (preserved), got %d", categoryCount)
		}

		// Dynamic tables should be reset to seed data
		var userCount int
		err = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount)
		if err != nil {
			t.Fatal(err)
		}

		// Should be back to the seed data (2 users)
		if userCount != 2 {
			t.Errorf("expected 2 users (reset to seed), got %d", userCount)
		}
	})
}
