package package1_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/yuku/testdbpool/examples/multiple-packages/shared"
)

func TestUserOperations(t *testing.T) {
	db, err := shared.Pool.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	t.Run("ListUsers", func(t *testing.T) {
		rows, err := db.QueryContext(ctx, "SELECT id, username, email FROM users ORDER BY id")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		count := 0
		for rows.Next() {
			var id int
			var username, email string
			if err := rows.Scan(&id, &username, &email); err != nil {
				t.Fatal(err)
			}
			count++
			t.Logf("User %d: %s (%s)", id, username, email)
		}

		if count != 3 {
			t.Errorf("expected 3 users, got %d", count)
		}
	})

	t.Run("CreateUser", func(t *testing.T) {
		result, err := db.ExecContext(ctx, 
			"INSERT INTO users (username, email) VALUES ($1, $2)",
			"testuser", "testuser@example.com")
		if err != nil {
			t.Fatal(err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			t.Fatal(err)
		}
		if affected != 1 {
			t.Errorf("expected 1 row affected, got %d", affected)
		}

		// Verify user was created
		var count int
		err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count != 4 {
			t.Errorf("expected 4 users after insert, got %d", count)
		}
	})
}

func TestUserOrderHistory(t *testing.T) {
	db, err := shared.Pool.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Create an order for alice
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	// Insert order
	var orderID int
	err = tx.QueryRowContext(ctx,
		"INSERT INTO orders (user_id, total, status) VALUES (1, 1199.98, 'completed') RETURNING id",
	).Scan(&orderID)
	if err != nil {
		t.Fatal(err)
	}

	// Insert order items
	_, err = tx.ExecContext(ctx,
		"INSERT INTO order_items (order_id, product_id, quantity, price) VALUES ($1, 1, 1, 999.99), ($1, 3, 1, 199.99)",
		orderID)
	if err != nil {
		t.Fatal(err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// Query user's order history
	rows, err := db.QueryContext(ctx, `
		SELECT o.id, o.total, o.status, COUNT(oi.id) as item_count
		FROM orders o
		LEFT JOIN order_items oi ON o.id = oi.order_id
		WHERE o.user_id = 1
		GROUP BY o.id, o.total, o.status
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	orderCount := 0
	for rows.Next() {
		var id int
		var total float64
		var status string
		var itemCount int
		if err := rows.Scan(&id, &total, &status, &itemCount); err != nil {
			t.Fatal(err)
		}
		orderCount++
		t.Logf("Order %d: $%.2f (%s) with %d items", id, total, status, itemCount)
		
		if itemCount != 2 {
			t.Errorf("expected 2 items in order, got %d", itemCount)
		}
	}

	if orderCount != 1 {
		t.Errorf("expected 1 order, got %d", orderCount)
	}
}

func TestDatabaseIsolation(t *testing.T) {
	// First test - modify data
	t.Run("ModifyData", func(t *testing.T) {
		db, err := shared.Pool.Acquire(t)
		if err != nil {
			t.Fatal(err)
		}

		ctx := context.Background()

		// Delete a user
		_, err = db.ExecContext(ctx, "DELETE FROM users WHERE username = 'charlie'")
		if err != nil {
			t.Fatal(err)
		}

		// Verify deletion
		var count int
		err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count != 2 {
			t.Errorf("expected 2 users after deletion, got %d", count)
		}
	})

	// Second test - verify data is restored
	t.Run("VerifyRestored", func(t *testing.T) {
		db, err := shared.Pool.Acquire(t)
		if err != nil {
			t.Fatal(err)
		}

		ctx := context.Background()

		// Check user count
		var count int
		err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count != 3 {
			t.Errorf("expected 3 users (data should be restored), got %d", count)
		}

		// Verify charlie exists
		var username string
		err = db.QueryRowContext(ctx, 
			"SELECT username FROM users WHERE username = 'charlie'",
		).Scan(&username)
		if err == sql.ErrNoRows {
			t.Error("charlie should exist after reset")
		} else if err != nil {
			t.Fatal(err)
		}
	})
}