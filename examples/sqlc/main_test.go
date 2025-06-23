package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/yuku/testdbpool"
	"github.com/yuku/testdbpool/examples/sqlc/db"
)

var pool *testdbpool.Pool

func TestMain(m *testing.M) {
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
		dbPassword = "password"
	}
	
	rootConnStr := fmt.Sprintf("postgres://%s:%s@%s:%s/postgres?sslmode=disable", dbUser, dbPassword, dbHost, dbPort)
	rootDB, err := sql.Open("pgx", rootConnStr)
	if err != nil {
		log.Fatal(err)
	}
	defer rootDB.Close()

	// Initialize test database pool
	pool, err = testdbpool.New(testdbpool.Configuration{
		RootConnection: rootDB,
		PoolID:         "sqlc_example",
		MaxPoolSize:    10,
		TemplateCreator: func(ctx context.Context, db *sql.DB) error {
			// Read and execute schema
			schema, err := os.ReadFile("sql/schema.sql")
			if err != nil {
				return fmt.Errorf("failed to read schema.sql: %w", err)
			}
			if _, err := db.ExecContext(ctx, string(schema)); err != nil {
				return fmt.Errorf("failed to execute schema: %w", err)
			}

			// Read and execute seed data
			seed, err := os.ReadFile("sql/seed.sql")
			if err != nil {
				return fmt.Errorf("failed to read seed.sql: %w", err)
			}
			if _, err := db.ExecContext(ctx, string(seed)); err != nil {
				return fmt.Errorf("failed to execute seed data: %w", err)
			}

			return nil
		},
		ResetFunc: testdbpool.ResetByTruncate(
			[]string{}, // truncate all tables
			func(ctx context.Context, db *sql.DB) error {
				// Re-execute seed data
				seed, err := os.ReadFile("sql/seed.sql")
				if err != nil {
					return fmt.Errorf("failed to read seed.sql: %w", err)
				}
				if _, err := db.ExecContext(ctx, string(seed)); err != nil {
					return fmt.Errorf("failed to execute seed data: %w", err)
				}
				return nil
			},
		),
	})
	if err != nil {
		log.Fatal(err)
	}

	// Run tests
	os.Exit(m.Run())
}

func TestUserOperations(t *testing.T) {
	dbConn, err := pool.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	queries := db.New(dbConn)

	// Test CreateUser
	t.Run("CreateUser", func(t *testing.T) {
		user, err := queries.CreateUser(ctx, db.CreateUserParams{
			Username:     "newuser",
			Email:        "newuser@example.com",
			PasswordHash: "$2a$10$YKbD3l.8kFHKZMKNQRsvVOJ.WnQvvGd0mXvQKXn.0l7n5F9nONEWW",
		})
		if err != nil {
			t.Fatal(err)
		}
		if user.Username != "newuser" {
			t.Errorf("expected username newuser, got %s", user.Username)
		}
	})

	// Test GetUser
	t.Run("GetUser", func(t *testing.T) {
		user, err := queries.GetUser(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		if user.Username != "alice" {
			t.Errorf("expected username alice, got %s", user.Username)
		}
	})

	// Test GetUserByUsername
	t.Run("GetUserByUsername", func(t *testing.T) {
		user, err := queries.GetUserByUsername(ctx, "bob")
		if err != nil {
			t.Fatal(err)
		}
		if user.Email != "bob@example.com" {
			t.Errorf("expected email bob@example.com, got %s", user.Email)
		}
	})

	// Test ListUsers
	t.Run("ListUsers", func(t *testing.T) {
		users, err := queries.ListUsers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(users) < 3 {
			t.Errorf("expected at least 3 users, got %d", len(users))
		}
	})

	// Test CountUsers
	t.Run("CountUsers", func(t *testing.T) {
		count, err := queries.CountUsers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if count < 3 {
			t.Errorf("expected at least 3 users, got %d", count)
		}
	})
}

func TestPostOperations(t *testing.T) {
	dbConn, err := pool.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	queries := db.New(dbConn)

	// Test CreatePost
	t.Run("CreatePost", func(t *testing.T) {
		post, err := queries.CreatePost(ctx, db.CreatePostParams{
			UserID:      1,
			Title:       "New Post",
			Slug:        "new-post",
			Content:     "This is a new post content.",
			Published:   false,
			PublishedAt: sql.NullTime{},
		})
		if err != nil {
			t.Fatal(err)
		}
		if post.Title != "New Post" {
			t.Errorf("expected title New Post, got %s", post.Title)
		}
	})

	// Test GetPost
	t.Run("GetPost", func(t *testing.T) {
		post, err := queries.GetPost(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		if post.Slug != "getting-started-with-go" {
			t.Errorf("expected slug getting-started-with-go, got %s", post.Slug)
		}
	})

	// Test GetPostBySlug
	t.Run("GetPostBySlug", func(t *testing.T) {
		post, err := queries.GetPostBySlug(ctx, "understanding-goroutines")
		if err != nil {
			t.Fatal(err)
		}
		if post.ID != 2 {
			t.Errorf("expected post ID 2, got %d", post.ID)
		}
	})

	// Test ListPublishedPosts
	t.Run("ListPublishedPosts", func(t *testing.T) {
		posts, err := queries.ListPublishedPosts(ctx, db.ListPublishedPostsParams{
			Limit:  10,
			Offset: 0,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(posts) < 4 {
			t.Errorf("expected at least 4 published posts, got %d", len(posts))
		}
	})

	// Test PublishPost
	t.Run("PublishPost", func(t *testing.T) {
		// First get the unpublished post
		post, err := queries.GetPost(ctx, 4)
		if err != nil {
			t.Fatal(err)
		}
		if post.Published {
			t.Skip("Post already published")
		}

		// Publish it
		publishedPost, err := queries.PublishPost(ctx, 4)
		if err != nil {
			t.Fatal(err)
		}
		if !publishedPost.Published {
			t.Error("expected post to be published")
		}
		if !publishedPost.PublishedAt.Valid {
			t.Error("expected published_at to be set")
		}
	})

	// Test GetPostWithAuthor
	t.Run("GetPostWithAuthor", func(t *testing.T) {
		row, err := queries.GetPostWithAuthor(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		if row.AuthorUsername != "alice" {
			t.Errorf("expected author username alice, got %s", row.AuthorUsername)
		}
	})
}

func TestCommentOperations(t *testing.T) {
	dbConn, err := pool.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	queries := db.New(dbConn)

	// Test CreateComment
	t.Run("CreateComment", func(t *testing.T) {
		comment, err := queries.CreateComment(ctx, db.CreateCommentParams{
			PostID:  1,
			UserID:  2,
			Content: "New comment on the post!",
		})
		if err != nil {
			t.Fatal(err)
		}
		if comment.Content != "New comment on the post!" {
			t.Errorf("expected content 'New comment on the post!', got %s", comment.Content)
		}
	})

	// Test GetComment
	t.Run("GetComment", func(t *testing.T) {
		comment, err := queries.GetComment(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		if comment.UserID != 2 {
			t.Errorf("expected user_id 2, got %d", comment.UserID)
		}
	})

	// Test ListCommentsByPost
	t.Run("ListCommentsByPost", func(t *testing.T) {
		comments, err := queries.ListCommentsByPost(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(comments) < 2 {
			t.Errorf("expected at least 2 comments for post 1, got %d", len(comments))
		}
		// Check that author_username is populated
		for _, comment := range comments {
			if comment.AuthorUsername == "" {
				t.Error("expected author_username to be populated")
			}
		}
	})

	// Test CountCommentsByPost
	t.Run("CountCommentsByPost", func(t *testing.T) {
		count, err := queries.CountCommentsByPost(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		if count != 2 {
			t.Errorf("expected 2 comments for post 2, got %d", count)
		}
	})

	// Test GetPostsWithCommentCounts
	t.Run("GetPostsWithCommentCounts", func(t *testing.T) {
		posts, err := queries.GetPostsWithCommentCounts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(posts) == 0 {
			t.Error("expected at least one post with comment counts")
		}
		// Verify comment counts
		for _, post := range posts {
			if post.ID == 1 && post.CommentCount < 2 {
				t.Errorf("expected at least 2 comments for post 1, got %d", post.CommentCount)
			}
		}
	})
}

func TestTransactions(t *testing.T) {
	dbConn, err := pool.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	t.Run("CreatePostWithComments", func(t *testing.T) {
		// Start transaction
		tx, err := dbConn.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()

		queries := db.New(tx)

		// Create a new post
		post, err := queries.CreatePost(ctx, db.CreatePostParams{
			UserID:      1,
			Title:       "Transaction Test Post",
			Slug:        "transaction-test-post",
			Content:     "Testing transactions with sqlc",
			Published:   true,
			PublishedAt: sql.NullTime{Time: time.Now(), Valid: true},
		})
		if err != nil {
			t.Fatal(err)
		}

		// Add comments to the new post
		for i := 0; i < 3; i++ {
			_, err := queries.CreateComment(ctx, db.CreateCommentParams{
				PostID:  post.ID,
				UserID:  int64(i%3 + 1),
				Content: fmt.Sprintf("Transaction comment %d", i+1),
			})
			if err != nil {
				t.Fatal(err)
			}
		}

		// Verify within transaction
		count, err := queries.CountCommentsByPost(ctx, post.ID)
		if err != nil {
			t.Fatal(err)
		}
		if count != 3 {
			t.Errorf("expected 3 comments, got %d", count)
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}

		// Verify after commit with new connection
		queries = db.New(dbConn)
		count, err = queries.CountCommentsByPost(ctx, post.ID)
		if err != nil {
			t.Fatal(err)
		}
		if count != 3 {
			t.Errorf("expected 3 comments after commit, got %d", count)
		}
	})
}

func TestConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	// Run 10 concurrent operations
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(userNum int) {
			defer wg.Done()

			dbConn, err := pool.Acquire(t)
			if err != nil {
				errors <- fmt.Errorf("failed to acquire connection: %w", err)
				return
			}

			queries := db.New(dbConn)

			// Create a user
			user, err := queries.CreateUser(ctx, db.CreateUserParams{
				Username:     fmt.Sprintf("concurrent_user_%d", userNum),
				Email:        fmt.Sprintf("concurrent_%d@example.com", userNum),
				PasswordHash: "$2a$10$YKbD3l.8kFHKZMKNQRsvVOJ.WnQvvGd0mXvQKXn.0l7n5F9nOCONC",
			})
			if err != nil {
				errors <- fmt.Errorf("failed to create user: %w", err)
				return
			}

			// Create a post
			post, err := queries.CreatePost(ctx, db.CreatePostParams{
				UserID:      user.ID,
				Title:       fmt.Sprintf("Concurrent Post %d", userNum),
				Slug:        fmt.Sprintf("concurrent-post-%d", userNum),
				Content:     "Content created concurrently",
				Published:   true,
				PublishedAt: sql.NullTime{Time: time.Now(), Valid: true},
			})
			if err != nil {
				errors <- fmt.Errorf("failed to create post: %w", err)
				return
			}

			// Add a comment
			_, err = queries.CreateComment(ctx, db.CreateCommentParams{
				PostID:  post.ID,
				UserID:  user.ID,
				Content: "Self comment",
			})
			if err != nil {
				errors <- fmt.Errorf("failed to create comment: %w", err)
				return
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Error(err)
	}
}

func TestConnectionPooling(t *testing.T) {
	ctx := context.Background()

	// Acquire database connection from testdbpool
	dbConn, err := pool.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	queries := db.New(dbConn)

	// Run multiple queries concurrently
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(num int) {
			defer wg.Done()

			// Simulate some work
			users, err := queries.ListUsers(ctx)
			if err != nil {
				t.Errorf("query %d failed: %v", num, err)
				return
			}
			if len(users) == 0 {
				t.Errorf("query %d returned no users", num)
			}

			// Small delay to simulate processing
			time.Sleep(10 * time.Millisecond)
		}(i)
	}

	wg.Wait()
}