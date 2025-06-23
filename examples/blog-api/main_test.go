package main_test

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/yuku/testdbpool"
)

var testPool *testdbpool.Pool

// TestMain sets up the test database pool for all tests
func TestMain(m *testing.M) {
	// Get database connection from environment or use defaults
	host := getEnvOrDefault("PGHOST", "localhost")
	port := getEnvOrDefault("PGPORT", "5432")
	user := getEnvOrDefault("PGUSER", "postgres")
	password := getEnvOrDefault("PGPASSWORD", "postgres")
	
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/postgres?sslmode=disable",
		user, password, host, port)
	
	rootDB, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}
	defer rootDB.Close()

	// Clean up any existing pool before starting
	if err := testdbpool.Cleanup(rootDB, "blog_api_test"); err != nil {
		log.Printf("Warning: Failed to cleanup existing pool: %v", err)
	}

	// Create the test pool
	testPool, err = testdbpool.New(testdbpool.Configuration{
		RootConnection:  rootDB,
		PoolID:          "blog_api_test",
		MaxPoolSize:     10,
		AcquireTimeout:  30 * time.Second,
		TemplateCreator: createBlogSchema,
		ResetFunc:       testdbpool.ResetByTruncate(
			// Order matters: child tables first
			[]string{"comments", "posts", "users"},
			seedTestData,
		),
	})
	if err != nil {
		log.Fatalf("Failed to create test pool: %v", err)
	}

	// Run tests
	code := m.Run()

	// Clean up after all tests
	if err := testdbpool.Cleanup(rootDB, "blog_api_test"); err != nil {
		log.Printf("Warning: Failed to cleanup pool after tests: %v", err)
	}

	os.Exit(code)
}

// createBlogSchema creates the database schema for our blog application
func createBlogSchema(ctx context.Context, db *sql.DB) error {
	schema := `
	-- Users table
	CREATE TABLE users (
		id SERIAL PRIMARY KEY,
		username VARCHAR(50) UNIQUE NOT NULL,
		email VARCHAR(100) UNIQUE NOT NULL,
		password_hash VARCHAR(255) NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Posts table
	CREATE TABLE posts (
		id SERIAL PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		title VARCHAR(200) NOT NULL,
		slug VARCHAR(200) UNIQUE NOT NULL,
		content TEXT NOT NULL,
		published BOOLEAN DEFAULT FALSE,
		published_at TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Comments table
	CREATE TABLE comments (
		id SERIAL PRIMARY KEY,
		post_id INTEGER NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		content TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Indexes for performance
	CREATE INDEX idx_posts_user_id ON posts(user_id);
	CREATE INDEX idx_posts_published ON posts(published);
	CREATE INDEX idx_posts_slug ON posts(slug);
	CREATE INDEX idx_comments_post_id ON comments(post_id);
	CREATE INDEX idx_comments_user_id ON comments(user_id);

	-- Update trigger for updated_at
	CREATE OR REPLACE FUNCTION update_updated_at_column()
	RETURNS TRIGGER AS $$
	BEGIN
		NEW.updated_at = CURRENT_TIMESTAMP;
		RETURN NEW;
	END;
	$$ language 'plpgsql';

	CREATE TRIGGER update_users_updated_at BEFORE UPDATE ON users
		FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
	
	CREATE TRIGGER update_posts_updated_at BEFORE UPDATE ON posts
		FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
	
	CREATE TRIGGER update_comments_updated_at BEFORE UPDATE ON comments
		FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
	`

	if _, err := db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Add initial seed data
	return seedTestData(ctx, db)
}

// seedTestData adds initial test data to the database
func seedTestData(ctx context.Context, db *sql.DB) error {
	// Start a transaction to ensure data consistency
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Insert test users and get their IDs
	var userIDs []int
	users := []struct {
		username string
		email    string
	}{
		{"alice", "alice@example.com"},
		{"bob", "bob@example.com"},
		{"charlie", "charlie@example.com"},
	}

	for _, u := range users {
		var id int
		err := tx.QueryRowContext(ctx, `
			INSERT INTO users (username, email, password_hash) 
			VALUES ($1, $2, '$2a$10$YK3QpmG1w6d3x3x3x3x3x3')
			RETURNING id`,
			u.username, u.email,
		).Scan(&id)
		if err != nil {
			return fmt.Errorf("failed to insert user %s: %w", u.username, err)
		}
		userIDs = append(userIDs, id)
	}

	// Insert test posts using actual user IDs
	posts := []struct {
		userIdx   int
		title     string
		slug      string
		published bool
	}{
		{0, "Getting Started with Go", "getting-started-with-go", true},
		{0, "Understanding Interfaces", "understanding-interfaces", true},
		{1, "My Draft Post", "my-draft-post", false},
	}

	var postIDs []int
	for _, p := range posts {
		var id int
		var publishedAt interface{}
		if p.published {
			publishedAt = time.Now()
		}
		err := tx.QueryRowContext(ctx, `
			INSERT INTO posts (user_id, title, slug, content, published, published_at) 
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id`,
			userIDs[p.userIdx], p.title, p.slug,
			"This is content for "+p.title, p.published, publishedAt,
		).Scan(&id)
		if err != nil {
			return fmt.Errorf("failed to insert post %s: %w", p.title, err)
		}
		postIDs = append(postIDs, id)
	}

	// Insert test comments using actual IDs
	comments := []struct {
		postIdx int
		userIdx int
		content string
	}{
		{0, 1, "Great article! Very helpful."},
		{0, 2, "Thanks for sharing this."},
		{1, 1, "Looking forward to the next part."},
	}

	for _, c := range comments {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO comments (post_id, user_id, content) 
			VALUES ($1, $2, $3)`,
			postIDs[c.postIdx], userIDs[c.userIdx], c.content,
		)
		if err != nil {
			return fmt.Errorf("failed to insert comment: %w", err)
		}
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit seed data: %w", err)
	}

	return nil
}

// Test creating a new user
func TestCreateUser(t *testing.T) {
	db, err := testPool.Acquire(t)
	if err != nil {
		t.Fatalf("Failed to acquire database: %v", err)
	}

	// Create a new user
	var userID int
	err = db.QueryRow(`
		INSERT INTO users (username, email, password_hash) 
		VALUES ($1, $2, $3) 
		RETURNING id`,
		"testuser", "test@example.com", "$2a$10$test",
	).Scan(&userID)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Verify user was created
	var username string
	err = db.QueryRow("SELECT username FROM users WHERE id = $1", userID).Scan(&username)
	if err != nil {
		t.Fatalf("Failed to query user: %v", err)
	}
	if username != "testuser" {
		t.Errorf("Expected username 'testuser', got '%s'", username)
	}

	// Verify total user count (3 seed + 1 new)
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count users: %v", err)
	}
	if count != 4 {
		t.Errorf("Expected 4 users, got %d", count)
	}
}

// Test that each test gets a clean database
func TestDatabaseIsolation(t *testing.T) {
	db, err := testPool.Acquire(t)
	if err != nil {
		t.Fatalf("Failed to acquire database: %v", err)
	}

	// Should have exactly 3 users from seed data
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count users: %v", err)
	}
	if count != 3 {
		t.Errorf("Expected 3 users (clean database), got %d", count)
	}
}

// Test creating a post with comments
func TestCreatePostWithComments(t *testing.T) {
	db, err := testPool.Acquire(t)
	if err != nil {
		t.Fatalf("Failed to acquire database: %v", err)
	}

	// First get a valid user ID
	var userID int
	err = db.QueryRow("SELECT id FROM users WHERE username = 'alice'").Scan(&userID)
	if err != nil {
		t.Fatalf("Failed to get user ID: %v", err)
	}

	// Start a transaction
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	// Create a new post
	var postID int
	err = tx.QueryRow(`
		INSERT INTO posts (user_id, title, slug, content, published, published_at) 
		VALUES ($1, $2, $3, $4, $5, $6) 
		RETURNING id`,
		userID, "Test Post", "test-post", "This is a test post", true, time.Now(),
	).Scan(&postID)
	if err != nil {
		t.Fatalf("Failed to create post: %v", err)
	}

	// Get all user IDs for comments
	rows, err := tx.Query("SELECT id FROM users ORDER BY username")
	if err != nil {
		t.Fatalf("Failed to query users: %v", err)
	}
	var userIDs []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("Failed to scan user ID: %v", err)
		}
		userIDs = append(userIDs, id)
	}
	rows.Close()

	// Add comments from each user
	for i, uid := range userIDs {
		_, err = tx.Exec(`
			INSERT INTO comments (post_id, user_id, content) 
			VALUES ($1, $2, $3)`,
			postID, uid, fmt.Sprintf("Test comment %d", i+1),
		)
		if err != nil {
			t.Fatalf("Failed to create comment: %v", err)
		}
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		t.Fatalf("Failed to commit transaction: %v", err)
	}

	// Verify comments were created
	var commentCount int
	err = db.QueryRow("SELECT COUNT(*) FROM comments WHERE post_id = $1", postID).Scan(&commentCount)
	if err != nil {
		t.Fatalf("Failed to count comments: %v", err)
	}
	if commentCount != 3 {
		t.Errorf("Expected 3 comments, got %d", commentCount)
	}
}

// Test querying posts with joins
func TestQueryPostsWithAuthor(t *testing.T) {
	db, err := testPool.Acquire(t)
	if err != nil {
		t.Fatalf("Failed to acquire database: %v", err)
	}

	// Query published posts with author information
	rows, err := db.Query(`
		SELECT p.title, p.slug, u.username, COUNT(c.id) as comment_count
		FROM posts p
		JOIN users u ON p.user_id = u.id
		LEFT JOIN comments c ON p.id = c.post_id
		WHERE p.published = true
		GROUP BY p.id, p.title, p.slug, u.username
		ORDER BY p.created_at DESC
	`)
	if err != nil {
		t.Fatalf("Failed to query posts: %v", err)
	}
	defer rows.Close()

	type postData struct {
		title        string
		slug         string
		author       string
		commentCount int
	}

	var posts []postData
	for rows.Next() {
		var p postData
		if err := rows.Scan(&p.title, &p.slug, &p.author, &p.commentCount); err != nil {
			t.Fatalf("Failed to scan row: %v", err)
		}
		posts = append(posts, p)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("Error iterating rows: %v", err)
	}

	// Verify results
	if len(posts) != 2 {
		t.Errorf("Expected 2 published posts, got %d", len(posts))
	}

	// Find the "Getting Started with Go" post and check its comments
	var gettingStartedPost *postData
	for i := range posts {
		if posts[i].slug == "getting-started-with-go" {
			gettingStartedPost = &posts[i]
			break
		}
	}

	if gettingStartedPost == nil {
		t.Error("Could not find 'Getting Started with Go' post")
	} else if gettingStartedPost.commentCount != 2 {
		t.Errorf("Expected 'Getting Started with Go' to have 2 comments, got %d", gettingStartedPost.commentCount)
	}
}

// Test concurrent access to the database pool
func TestConcurrentAccess(t *testing.T) {
	// Run 5 concurrent operations
	done := make(chan bool, 5)
	
	for i := 0; i < 5; i++ {
		go func(id int) {
			// Each goroutine gets its own test context
			subtest := fmt.Sprintf("concurrent_%d", id)
			t.Run(subtest, func(t *testing.T) {
				db, err := testPool.Acquire(t)
				if err != nil {
					t.Errorf("Worker %d: Failed to acquire database: %v", id, err)
					done <- false
					return
				}

				// Perform some database operation
				var count int
				err = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
				if err != nil {
					t.Errorf("Worker %d: Failed to query: %v", id, err)
					done <- false
					return
				}

				if count != 3 {
					t.Errorf("Worker %d: Expected 3 users, got %d", id, count)
					done <- false
					return
				}

				// Simulate some work
				time.Sleep(100 * time.Millisecond)
				
				done <- true
			})
		}(i)
	}

	// Wait for all goroutines
	successCount := 0
	for i := 0; i < 5; i++ {
		if <-done {
			successCount++
		}
	}

	if successCount != 5 {
		t.Errorf("Expected all 5 workers to succeed, got %d", successCount)
	}
}

// Helper function to get environment variable with default
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}