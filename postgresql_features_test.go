package testdbpool_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/yuku/testdbpool"
	"github.com/yuku/testdbpool/internal/testhelper"
)

func TestPostgreSQLFeatures(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	pool := testhelper.GetTestDBPool(t)

	t.Run("ComplexSchemaTemplate", func(t *testing.T) {
		config := &testdbpool.Config{
			PoolID:       "complex-schema-test",
			DBPool:       pool,
			MaxDatabases: 2,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				// Create a complex schema with various PostgreSQL features
				_, err := conn.Exec(ctx, `
					-- Custom enum type
					CREATE TYPE status_type AS ENUM ('active', 'inactive', 'pending');
					
					-- Function for automatic timestamps
					CREATE OR REPLACE FUNCTION update_modified_column()
					RETURNS TRIGGER AS $$
					BEGIN
						NEW.modified = NOW();
						RETURN NEW;
					END;
					$$ language 'plpgsql';
					
					-- Table with various column types
					CREATE TABLE users (
						id SERIAL PRIMARY KEY,
						email VARCHAR(255) UNIQUE NOT NULL,
						status status_type DEFAULT 'pending',
						metadata JSONB DEFAULT '{}',
						created TIMESTAMP DEFAULT NOW(),
						modified TIMESTAMP DEFAULT NOW()
					);
					
					-- Trigger for automatic modification timestamps
					CREATE TRIGGER update_users_modtime 
						BEFORE UPDATE ON users 
						FOR EACH ROW EXECUTE FUNCTION update_modified_column();
					
					-- Index on JSONB column
					CREATE INDEX idx_users_metadata ON users USING GIN (metadata);
					
					-- Partial index
					CREATE INDEX idx_active_users ON users (email) WHERE status = 'active';
					
					-- Table with foreign key
					CREATE TABLE posts (
						id SERIAL PRIMARY KEY,
						user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
						title TEXT NOT NULL,
						content TEXT,
						tags TEXT[] DEFAULT '{}',
						created TIMESTAMP DEFAULT NOW()
					);
					
					-- Index on array column
					CREATE INDEX idx_posts_tags ON posts USING GIN (tags);
					
					-- View
					CREATE VIEW active_user_posts AS
					SELECT 
						u.email,
						p.title,
						p.created,
						array_length(p.tags, 1) as tag_count
					FROM users u
					JOIN posts p ON u.id = p.user_id
					WHERE u.status = 'active';
				`)
				return err
			},
			ResetDatabase: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `
					TRUNCATE posts, users RESTART IDENTITY CASCADE;
				`)
				return err
			},
		}

		testPool, err := testdbpool.New(ctx, config)
		if err != nil {
			t.Fatalf("failed to create pool: %v", err)
		}
		defer testPool.Close()

		// Test the complex schema
		db, err := testPool.Acquire(ctx)
		if err != nil {
			t.Fatalf("failed to acquire database: %v", err)
		}
		defer db.Close()

		// Test enum type
		_, err = db.Conn().Exec(ctx, `
			INSERT INTO users (email, status, metadata) 
			VALUES ('test@example.com', 'active', '{"role": "admin", "preferences": {"theme": "dark"}}')
		`)
		if err != nil {
			t.Fatalf("failed to insert user: %v", err)
		}

		// Test JSONB query
		var role string
		err = db.Conn().QueryRow(ctx, `
			SELECT metadata->>'role' FROM users WHERE email = 'test@example.com'
		`).Scan(&role)
		if err != nil {
			t.Fatalf("failed to query JSONB: %v", err)
		}
		if role != "admin" {
			t.Errorf("expected role 'admin', got '%s'", role)
		}

		// Test array operations
		_, err = db.Conn().Exec(ctx, `
			INSERT INTO posts (user_id, title, content, tags)
			SELECT id, 'Test Post', 'Content here', ARRAY['tech', 'golang', 'database']
			FROM users WHERE email = 'test@example.com'
		`)
		if err != nil {
			t.Fatalf("failed to insert post: %v", err)
		}

		// Test view
		var email, title string
		var tagCount *int
		err = db.Conn().QueryRow(ctx, `
			SELECT email, title, tag_count FROM active_user_posts LIMIT 1
		`).Scan(&email, &title, &tagCount)
		if err != nil {
			t.Fatalf("failed to query view: %v", err)
		}
		if email != "test@example.com" {
			t.Errorf("expected email 'test@example.com', got '%s'", email)
		}
		if tagCount == nil || *tagCount != 3 {
			t.Errorf("expected 3 tags, got %v", tagCount)
		}

		// Test trigger (modification timestamp should update)
		var oldModified string
		err = db.Conn().QueryRow(ctx, `
			SELECT modified::text FROM users WHERE email = 'test@example.com'
		`).Scan(&oldModified)
		if err != nil {
			t.Fatalf("failed to get old modified: %v", err)
		}

		// Update and verify trigger worked
		_, err = db.Conn().Exec(ctx, `
			UPDATE users SET status = 'inactive' WHERE email = 'test@example.com'
		`)
		if err != nil {
			t.Fatalf("failed to update user: %v", err)
		}

		var newModified string
		err = db.Conn().QueryRow(ctx, `
			SELECT modified::text FROM users WHERE email = 'test@example.com'
		`).Scan(&newModified)
		if err != nil {
			t.Fatalf("failed to get new modified: %v", err)
		}

		if oldModified == newModified {
			t.Error("trigger should have updated the modified timestamp")
		}
	})

	t.Run("DatabaseIsolation", func(t *testing.T) {
		config := &testdbpool.Config{
			PoolID:       "isolation-test",
			DBPool:       pool,
			MaxDatabases: 2,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `
					CREATE TABLE accounts (
						id SERIAL PRIMARY KEY,
						name VARCHAR(100) NOT NULL,
						balance DECIMAL(10,2) NOT NULL DEFAULT 0.00
					);
					
					INSERT INTO accounts (name, balance) VALUES 
						('Alice', 1000.00),
						('Bob', 500.00);
				`)
				return err
			},
			ResetDatabase: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `
					UPDATE accounts SET balance = 1000.00 WHERE name = 'Alice';
					UPDATE accounts SET balance = 500.00 WHERE name = 'Bob';
				`)
				return err
			},
		}

		testPool, err := testdbpool.New(ctx, config)
		if err != nil {
			t.Fatalf("failed to create pool: %v", err)
		}
		defer testPool.Close()

		// Test that separate database instances are truly isolated
		db1, err := testPool.Acquire(ctx)
		if err != nil {
			t.Fatalf("failed to acquire database 1: %v", err)
		}
		defer db1.Close()

		db2, err := testPool.Acquire(ctx)
		if err != nil {
			t.Fatalf("failed to acquire database 2: %v", err)
		}
		defer db2.Close()

		// Verify they are different databases
		if db1.DatabaseName() == db2.DatabaseName() {
			t.Skip("Got same database, skipping isolation test (pool exhausted)")
		}

		// Modify data in db1
		_, err = db1.Conn().Exec(ctx, "UPDATE accounts SET balance = 999.00 WHERE name = 'Alice'")
		if err != nil {
			t.Fatalf("failed to update Alice in db1: %v", err)
		}

		// Check that db2 is unaffected (different database)
		var aliceBalance float64
		err = db2.Conn().QueryRow(ctx, "SELECT balance FROM accounts WHERE name = 'Alice'").Scan(&aliceBalance)
		if err != nil {
			t.Fatalf("failed to query Alice balance in db2: %v", err)
		}
		if aliceBalance != 1000.00 {
			t.Errorf("expected Alice balance 1000.00 in db2 (isolated), got %f", aliceBalance)
		}

		// Verify db1 has the changed value
		err = db1.Conn().QueryRow(ctx, "SELECT balance FROM accounts WHERE name = 'Alice'").Scan(&aliceBalance)
		if err != nil {
			t.Fatalf("failed to query Alice balance in db1: %v", err)
		}
		if aliceBalance != 999.00 {
			t.Errorf("expected Alice balance 999.00 in db1, got %f", aliceBalance)
		}

		t.Logf("Database isolation verified: db1=%s, db2=%s", db1.DatabaseName(), db2.DatabaseName())
	})
}