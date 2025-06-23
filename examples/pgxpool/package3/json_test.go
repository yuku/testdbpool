package package3_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type EventData struct {
	Type      string    `json:"type"`
	UserID    int       `json:"user_id"`
	Timestamp time.Time `json:"timestamp"`
	Details   map[string]any `json:"details"`
}

func TestJSONOperations(t *testing.T) {
	pool, err := poolWrapper.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	t.Run("StoreJSONData", func(t *testing.T) {
		// Create test data
		event := EventData{
			Type:      "user_action",
			UserID:    1,
			Timestamp: time.Now(),
			Details: map[string]any{
				"action": "login",
				"ip":     "192.168.1.1",
				"device": "mobile",
			},
		}

		jsonData, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}

		// Insert JSON data
		var id int
		err = pool.QueryRow(ctx, `
			INSERT INTO package3_data (json_data)
			VALUES ($1)
			RETURNING id
		`, jsonData).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}

		// Query JSON data
		var result json.RawMessage
		err = pool.QueryRow(ctx, `
			SELECT json_data 
			FROM package3_data 
			WHERE id = $1
		`, id).Scan(&result)
		if err != nil {
			t.Fatal(err)
		}

		var retrieved EventData
		if err := json.Unmarshal(result, &retrieved); err != nil {
			t.Fatal(err)
		}

		if retrieved.Type != event.Type {
			t.Errorf("expected type %s, got %s", event.Type, retrieved.Type)
		}
	})

	t.Run("QueryJSONFields", func(t *testing.T) {
		// Insert multiple events
		events := []EventData{
			{Type: "login", UserID: 1, Timestamp: time.Now(), Details: map[string]interface{}{"success": true}},
			{Type: "logout", UserID: 2, Timestamp: time.Now(), Details: map[string]interface{}{"duration": 3600}},
			{Type: "login", UserID: 3, Timestamp: time.Now(), Details: map[string]interface{}{"success": false}},
		}

		for _, event := range events {
			jsonData, _ := json.Marshal(event)
			_, err := pool.Exec(ctx, "INSERT INTO package3_data (json_data) VALUES ($1)", jsonData)
			if err != nil {
				t.Fatal(err)
			}
		}

		// Query using JSON operators
		rows, err := pool.Query(ctx, `
			SELECT json_data->>'type' as event_type, COUNT(*) as count
			FROM package3_data
			WHERE json_data->>'type' IS NOT NULL
			GROUP BY event_type
			ORDER BY count DESC
		`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		t.Log("Event type statistics:")
		for rows.Next() {
			var eventType string
			var count int
			if err := rows.Scan(&eventType, &count); err != nil {
				t.Fatal(err)
			}
			t.Logf("  %s: %d occurrences", eventType, count)
		}
	})
}

func TestComplexJSONQueries(t *testing.T) {
	pool, err := poolWrapper.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Insert test data with complex JSON
	testData := []map[string]any{
		{
			"type": "order",
			"user_id": 1,
			"items": []map[string]any{
				{"product": "book", "price": 15.99, "quantity": 2},
				{"product": "pen", "price": 2.50, "quantity": 5},
			},
			"total": 43.98,
		},
		{
			"type": "order",
			"user_id": 2,
			"items": []map[string]any{
				{"product": "laptop", "price": 999.99, "quantity": 1},
			},
			"total": 999.99,
		},
	}

	for _, data := range testData {
		jsonData, _ := json.Marshal(data)
		_, err := pool.Exec(ctx, "INSERT INTO package3_data (json_data) VALUES ($1)", jsonData)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Complex JSON query using pgx
	var totalRevenue float64
	err = pool.QueryRow(ctx, `
		SELECT SUM((json_data->>'total')::numeric)
		FROM package3_data
		WHERE json_data->>'type' = 'order'
	`).Scan(&totalRevenue)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Total revenue from orders: $%.2f", totalRevenue)
	
	if totalRevenue < 1000 {
		t.Errorf("expected total revenue > 1000, got %.2f", totalRevenue)
	}
}

func TestParallelJSONOperations(t *testing.T) {
	pool, err := poolWrapper.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	done := make(chan bool, 15)

	// Run 15 parallel operations
	for i := 0; i < 15; i++ {
		go func(id int) {
			defer func() { done <- true }()

			// Create unique event data
			event := map[string]any{
				"type":       fmt.Sprintf("event_%d", id%3),
				"package":    "package3",
				"goroutine":  id,
				"timestamp":  time.Now().Format(time.RFC3339),
				"metrics": map[string]any{
					"cpu":    id * 10,
					"memory": id * 100,
				},
			}

			jsonData, err := json.Marshal(event)
			if err != nil {
				t.Errorf("goroutine %d: marshal error: %v", id, err)
				return
			}

			// Use transaction for atomic operations
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
			if err != nil {
				t.Errorf("goroutine %d: begin tx error: %v", id, err)
				return
			}
			defer tx.Rollback(ctx)

			// Insert and immediately query
			var insertedID int
			err = tx.QueryRow(ctx, `
				INSERT INTO package3_data (json_data)
				VALUES ($1)
				RETURNING id
			`, jsonData).Scan(&insertedID)
			if err != nil {
				t.Errorf("goroutine %d: insert error: %v", id, err)
				return
			}

			// Verify the insert
			var count int
			err = tx.QueryRow(ctx, `
				SELECT COUNT(*) 
				FROM package3_data 
				WHERE id = $1 AND json_data->>'goroutine' = $2
			`, insertedID, fmt.Sprintf("%d", id)).Scan(&count)
			if err != nil {
				t.Errorf("goroutine %d: verify error: %v", id, err)
				return
			}

			if count != 1 {
				t.Errorf("goroutine %d: expected 1 row, got %d", id, count)
				return
			}

			if err := tx.Commit(ctx); err != nil {
				t.Errorf("goroutine %d: commit error: %v", id, err)
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 15; i++ {
		<-done
	}

	// Final statistics
	var stats struct {
		TotalRows   int
		UniqueTypes int
	}

	err = pool.QueryRow(ctx, `
		SELECT 
			COUNT(*) as total_rows,
			COUNT(DISTINCT json_data->>'type') as unique_types
		FROM package3_data
	`).Scan(&stats.TotalRows, &stats.UniqueTypes)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Package3: Total JSON records: %d, Unique event types: %d", 
		stats.TotalRows, stats.UniqueTypes)
}