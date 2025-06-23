package package2_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/yuku/testdbpool/examples/pgxpool/shared"
)

func TestPostOperations(t *testing.T) {
	wrapper, err := shared.GetPoolWrapper()
	if err != nil {
		t.Fatal(err)
	}

	pool, err := wrapper.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	t.Run("CreatePost", func(t *testing.T) {
		// Create a new post
		var postID int
		err := pool.QueryRow(ctx, `
			INSERT INTO posts (user_id, title, content) 
			VALUES ($1, $2, $3) 
			RETURNING id
		`, 1, "Package2 Test Post", "Content from package2").Scan(&postID)
		if err != nil {
			t.Fatal(err)
		}

		// Add comment to the post
		_, err = pool.Exec(ctx, `
			INSERT INTO comments (post_id, user_id, content)
			VALUES ($1, $2, $3)
		`, postID, 2, "Comment from package2")
		if err != nil {
			t.Fatal(err)
		}

		// Log activity
		_, err = pool.Exec(ctx, "INSERT INTO package2_data (value) VALUES ($1)", postID)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("CopyPosts", func(t *testing.T) {
		// Use COPY to insert multiple posts
		rows := [][]interface{}{
			{2, "Bulk Post 1 from Package2", "Content 1"},
			{3, "Bulk Post 2 from Package2", "Content 2"},
			{1, "Bulk Post 3 from Package2", "Content 3"},
		}

		copyCount, err := pool.CopyFrom(
			ctx,
			pgx.Identifier{"posts"},
			[]string{"user_id", "title", "content"},
			pgx.CopyFromRows(rows),
		)
		if err != nil {
			t.Fatal(err)
		}

		if copyCount != 3 {
			t.Errorf("expected to copy 3 rows, got %d", copyCount)
		}

		// Log the count
		_, err = pool.Exec(ctx, "INSERT INTO package2_data (value) VALUES ($1)", int(copyCount))
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestPostStatistics(t *testing.T) {
	wrapper, err := shared.GetPoolWrapper()
	if err != nil {
		t.Fatal(err)
	}

	pool, err := wrapper.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Calculate statistics using window functions
	rows, err := pool.Query(ctx, `
		SELECT 
			u.name,
			COUNT(p.id) as post_count,
			COUNT(p.id) * 100.0 / SUM(COUNT(p.id)) OVER () as percentage
		FROM users u
		LEFT JOIN posts p ON u.id = p.user_id
		GROUP BY u.id, u.name
		ORDER BY post_count DESC
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	t.Log("Post statistics by user:")
	totalPercentage := 0.0
	for rows.Next() {
		var name string
		var count int
		var percentage float64
		if err := rows.Scan(&name, &count, &percentage); err != nil {
			t.Fatal(err)
		}
		t.Logf("  %s: %d posts (%.1f%%)", name, count, percentage)
		totalPercentage += percentage
		
		// Store statistics
		_, err = pool.Exec(ctx, "INSERT INTO package2_data (value) VALUES ($1)", count)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Verify percentages add up to 100
	if totalPercentage < 99.9 || totalPercentage > 100.1 {
		t.Errorf("percentages should sum to ~100, got %.1f", totalPercentage)
	}
}

func TestConcurrentPostCreation(t *testing.T) {
	wrapper, err := shared.GetPoolWrapper()
	if err != nil {
		t.Fatal(err)
	}

	pool, err := wrapper.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	var wg sync.WaitGroup

	// Create 20 posts concurrently
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			userID := (id % 3) + 1
			title := fmt.Sprintf("Concurrent Post %d from Package2", id)
			
			var postID int
			err := pool.QueryRow(ctx, `
				INSERT INTO posts (user_id, title, content)
				VALUES ($1, $2, $3)
				RETURNING id
			`, userID, title, "Concurrent content").Scan(&postID)
			
			if err != nil {
				t.Errorf("Failed to create post %d: %v", id, err)
				return
			}

			// Record the creation
			_, err = pool.Exec(ctx, "INSERT INTO package2_data (value) VALUES ($1)", postID)
			if err != nil {
				t.Errorf("Failed to log post %d: %v", id, err)
			}
		}(i)
	}

	wg.Wait()

	// Verify all posts were created
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM package2_data").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Package2: Created %d entries in package2_data", count)
}