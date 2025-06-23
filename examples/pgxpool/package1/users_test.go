package package1_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yuku/testdbpool/examples/pgxpool/shared"
)

func TestUserOperations(t *testing.T) {
	wrapper, err := shared.GetPoolWrapper()
	if err != nil {
		t.Fatal(err)
	}

	pool, err := wrapper.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	t.Run("CreateUser", func(t *testing.T) {
		// Create a new user
		var userID int
		err := pool.QueryRow(ctx, `
			INSERT INTO users (name, email) 
			VALUES ($1, $2) 
			RETURNING id
		`, "Package1 User", "pkg1@example.com").Scan(&userID)
		if err != nil {
			t.Fatal(err)
		}

		// Verify user was created
		var name string
		err = pool.QueryRow(ctx, "SELECT name FROM users WHERE id = $1", userID).Scan(&name)
		if err != nil {
			t.Fatal(err)
		}
		if name != "Package1 User" {
			t.Errorf("expected 'Package1 User', got %s", name)
		}

		// Log activity to package1_data
		_, err = pool.Exec(ctx, "INSERT INTO package1_data (data) VALUES ($1)", 
			"Created user in package1 test")
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("BatchUserQueries", func(t *testing.T) {
		batch := &pgx.Batch{}
		
		// Queue multiple user queries
		batch.Queue("SELECT COUNT(*) FROM users")
		batch.Queue("SELECT name FROM users WHERE email = $1", "alice@example.com")
		batch.Queue("SELECT COUNT(*) FROM posts WHERE user_id = 1")
		
		results := pool.SendBatch(ctx, batch)
		defer results.Close()

		// Get user count
		var userCount int
		err := results.QueryRow().Scan(&userCount)
		if err != nil {
			t.Fatal(err)
		}
		if userCount < 3 {
			t.Errorf("expected at least 3 users, got %d", userCount)
		}

		// Get Alice's name
		var name string
		err = results.QueryRow().Scan(&name)
		if err != nil {
			t.Fatal(err)
		}
		if name != "Alice" {
			t.Errorf("expected Alice, got %s", name)
		}

		// Get Alice's post count
		var postCount int
		err = results.QueryRow().Scan(&postCount)
		if err != nil {
			t.Fatal(err)
		}
		if postCount != 2 {
			t.Errorf("expected 2 posts for Alice, got %d", postCount)
		}
	})
}

func TestConcurrentUserAccess(t *testing.T) {
	wrapper, err := shared.GetPoolWrapper()
	if err != nil {
		t.Fatal(err)
	}

	pool, err := wrapper.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Run 10 concurrent operations
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()

			// Each goroutine inserts data and reads it back
			data := fmt.Sprintf("Package1 concurrent test %d at %v", id, time.Now())
			_, err := pool.Exec(ctx, "INSERT INTO package1_data (data) VALUES ($1)", data)
			if err != nil {
				t.Errorf("goroutine %d: insert failed: %v", id, err)
				return
			}

			// Read back to verify
			var count int
			err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM package1_data WHERE data = $1", data).Scan(&count)
			if err != nil {
				t.Errorf("goroutine %d: query failed: %v", id, err)
				return
			}
			if count != 1 {
				t.Errorf("goroutine %d: expected 1 row, got %d", id, count)
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify total inserts
	var total int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM package1_data").Scan(&total)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Package1: Total rows in package1_data: %d", total)
}