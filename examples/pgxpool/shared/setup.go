package shared

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"sync"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/yuku/testdbpool"
	tpgxpool "github.com/yuku/testdbpool/pgxpool"
)

var (
	pool        *testdbpool.Pool
	poolWrapper *tpgxpool.Wrapper
	initOnce    sync.Once
	initErr     error
)

// GetPoolWrapper returns the shared pool wrapper, initializing it if needed
func GetPoolWrapper() (*tpgxpool.Wrapper, error) {
	initOnce.Do(func() {
		initErr = initializePool()
	})
	return poolWrapper, initErr
}

func initializePool() error {
	// Setup PostgreSQL connection for state management
	dbHost := os.Getenv("DB_HOST")
	if dbHost == "" {
		dbHost = "localhost"
	}
	dbPort := os.Getenv("DB_PORT")
	if dbPort == "" {
		dbPort = "5432"
	}
	dbUser := os.Getenv("DB_USER")
	if dbUser == "" {
		dbUser = "postgres"
	}
	dbPassword := os.Getenv("DB_PASSWORD")
	if dbPassword == "" {
		dbPassword = "postgres"
	}

	rootConnStr := fmt.Sprintf("postgres://%s:%s@%s:%s/postgres?sslmode=disable", dbUser, dbPassword, dbHost, dbPort)
	rootDB, err := sql.Open("pgx", rootConnStr)
	if err != nil {
		return fmt.Errorf("failed to open root connection: %w", err)
	}

	// Initialize test database pool
	pool, err = testdbpool.New(testdbpool.Configuration{
		RootConnection: rootDB,
		PoolID:         "pgxpool_multi_pkg",
		MaxPoolSize:    20, // Increased for parallel testing
		TemplateCreator: func(ctx context.Context, db *sql.DB) error {
			// Create schema for all packages
			schema := `
				-- Common tables used by all packages
				CREATE TABLE users (
					id SERIAL PRIMARY KEY,
					name VARCHAR(100) NOT NULL,
					email VARCHAR(100) UNIQUE NOT NULL,
					created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
				);

				CREATE TABLE posts (
					id SERIAL PRIMARY KEY,
					user_id INTEGER REFERENCES users(id),
					title VARCHAR(200) NOT NULL,
					content TEXT,
					created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
				);

				CREATE TABLE comments (
					id SERIAL PRIMARY KEY,
					post_id INTEGER REFERENCES posts(id),
					user_id INTEGER REFERENCES users(id),
					content TEXT NOT NULL,
					created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
				);

				-- Package-specific tables
				CREATE TABLE package1_data (
					id SERIAL PRIMARY KEY,
					data TEXT NOT NULL,
					created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
				);

				CREATE TABLE package2_data (
					id SERIAL PRIMARY KEY,
					value INTEGER NOT NULL,
					created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
				);

				CREATE TABLE package3_data (
					id SERIAL PRIMARY KEY,
					json_data JSONB NOT NULL,
					created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
				);

				-- Insert common test data
				INSERT INTO users (id, name, email) VALUES 
					(1, 'Alice', 'alice@example.com'),
					(2, 'Bob', 'bob@example.com'),
					(3, 'Charlie', 'charlie@example.com');
				
				INSERT INTO posts (id, user_id, title, content) VALUES
					(1, 1, 'First Post', 'Hello World'),
					(2, 2, 'Second Post', 'Another post'),
					(3, 1, 'Third Post', 'More content');

				-- Reset sequences
				SELECT setval('users_id_seq', 3);
				SELECT setval('posts_id_seq', 3);
			`
			_, err := db.ExecContext(ctx, schema)
			return err
		},
		ResetFunc: func(ctx context.Context, db *sql.DB) error {
			// Custom reset function that's more defensive about table existence
			tables := []string{
				"comments", "posts", "users",
				"package1_data", "package2_data", "package3_data",
			}
			
			// Truncate tables in order, ignoring missing tables
			for _, table := range tables {
				// Check if table exists first
				var exists bool
				err := db.QueryRowContext(ctx, 
					"SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = $1)",
					table).Scan(&exists)
				if err != nil {
					return fmt.Errorf("failed to check table existence for %s: %w", table, err)
				}
				
				if exists {
					_, err = db.ExecContext(ctx, fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table))
					if err != nil {
						return fmt.Errorf("failed to truncate table %s: %w", table, err)
					}
				}
			}
			
			// Re-insert common test data
			_, err := db.ExecContext(ctx, `
				INSERT INTO users (id, name, email) VALUES 
					(1, 'Alice', 'alice@example.com'),
					(2, 'Bob', 'bob@example.com'),
					(3, 'Charlie', 'charlie@example.com');
				
				INSERT INTO posts (id, user_id, title, content) VALUES
					(1, 1, 'First Post', 'Hello World'),
					(2, 2, 'Second Post', 'Another post'),
					(3, 1, 'Third Post', 'More content');

				SELECT setval('users_id_seq', 3);
				SELECT setval('posts_id_seq', 3);
			`)
			return err
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create pool: %w", err)
	}

	// Create wrapper
	poolWrapper = tpgxpool.New(pool)

	log.Printf("Initialized shared pool with ID: pgxpool_multi_pkg")
	return nil
}
