package package3_test

import (
	"context"
	"testing"
	"time"

	"github.com/yuku/testdbpool/examples/multiple-packages/shared"
)

func TestCreateOrder(t *testing.T) {
	db, err := shared.Pool.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Start a transaction for the order
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	// Create order for Bob (user_id=2)
	var orderID int
	err = tx.QueryRowContext(ctx,
		"INSERT INTO orders (user_id, total, status) VALUES ($1, $2, $3) RETURNING id",
		2, 819.97, "pending",
	).Scan(&orderID)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Created order %d", orderID)

	// Add items to the order
	items := []struct {
		productID int
		quantity  int
		price     float64
	}{
		{2, 1, 599.99}, // Phone
		{5, 11, 19.99}, // T-Shirts
	}

	for _, item := range items {
		_, err = tx.ExecContext(ctx,
			"INSERT INTO order_items (order_id, product_id, quantity, price) VALUES ($1, $2, $3, $4)",
			orderID, item.productID, item.quantity, item.price,
		)
		if err != nil {
			t.Fatal(err)
		}

		// Update product stock
		result, err := tx.ExecContext(ctx,
			"UPDATE products SET stock = stock - $1 WHERE id = $2 AND stock >= $1",
			item.quantity, item.productID,
		)
		if err != nil {
			t.Fatal(err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			t.Fatal(err)
		}
		if affected != 1 {
			t.Errorf("failed to update stock for product %d", item.productID)
		}
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// Verify order was created
	var orderCount int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM orders").Scan(&orderCount)
	if err != nil {
		t.Fatal(err)
	}
	if orderCount != 1 {
		t.Errorf("expected 1 order, got %d", orderCount)
	}

	// Verify order items
	var itemCount int
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM order_items WHERE order_id = $1",
		orderID,
	).Scan(&itemCount)
	if err != nil {
		t.Fatal(err)
	}
	if itemCount != 2 {
		t.Errorf("expected 2 order items, got %d", itemCount)
	}

	// Verify stock was updated
	var phoneStock, tshirtStock int
	err = db.QueryRowContext(ctx, "SELECT stock FROM products WHERE id = 2").Scan(&phoneStock)
	if err != nil {
		t.Fatal(err)
	}
	err = db.QueryRowContext(ctx, "SELECT stock FROM products WHERE id = 5").Scan(&tshirtStock)
	if err != nil {
		t.Fatal(err)
	}

	if phoneStock != 24 { // 25 - 1
		t.Errorf("expected phone stock to be 24, got %d", phoneStock)
	}
	if tshirtStock != 189 { // 200 - 11
		t.Errorf("expected t-shirt stock to be 189, got %d", tshirtStock)
	}
}

func TestOrderStatistics(t *testing.T) {
	db, err := shared.Pool.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Create multiple orders
	orders := []struct {
		userID int
		total  float64
		status string
	}{
		{1, 1199.98, "completed"},
		{1, 59.97, "completed"},
		{2, 999.99, "completed"},
		{3, 219.98, "pending"},
	}

	for _, order := range orders {
		var orderID int
		err := db.QueryRowContext(ctx,
			"INSERT INTO orders (user_id, total, status) VALUES ($1, $2, $3) RETURNING id",
			order.userID, order.total, order.status,
		).Scan(&orderID)
		if err != nil {
			t.Fatal(err)
		}
	}

	t.Run("TotalSales", func(t *testing.T) {
		var total float64
		err := db.QueryRowContext(ctx,
			"SELECT COALESCE(SUM(total), 0) FROM orders WHERE status = 'completed'",
		).Scan(&total)
		if err != nil {
			t.Fatal(err)
		}

		expected := 1199.98 + 59.97 + 999.99
		if total != expected {
			t.Errorf("expected total sales %.2f, got %.2f", expected, total)
		}
	})

	t.Run("OrdersByUser", func(t *testing.T) {
		rows, err := db.QueryContext(ctx, `
			SELECT u.username, COUNT(o.id) as order_count, COALESCE(SUM(o.total), 0) as total_spent
			FROM users u
			LEFT JOIN orders o ON u.id = o.user_id
			GROUP BY u.id, u.username
			ORDER BY u.username
		`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		users := make(map[string]int)
		for rows.Next() {
			var username string
			var orderCount int
			var totalSpent float64
			if err := rows.Scan(&username, &orderCount, &totalSpent); err != nil {
				t.Fatal(err)
			}
			users[username] = orderCount
			t.Logf("User %s: %d orders, $%.2f total", username, orderCount, totalSpent)
		}

		if users["alice"] != 2 {
			t.Errorf("expected alice to have 2 orders, got %d", users["alice"])
		}
		if users["bob"] != 1 {
			t.Errorf("expected bob to have 1 order, got %d", users["bob"])
		}
		if users["charlie"] != 1 {
			t.Errorf("expected charlie to have 1 order, got %d", users["charlie"])
		}
	})
}

func TestDataPersistenceAcrossPackages(t *testing.T) {
	db, err := shared.Pool.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// This test verifies that each test gets a clean database
	// Check that we have the original data

	t.Run("VerifyCleanData", func(t *testing.T) {
		// Users should be back to 3
		var userCount int
		err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&userCount)
		if err != nil {
			t.Fatal(err)
		}
		if userCount != 3 {
			t.Errorf("expected 3 users (clean state), got %d", userCount)
		}

		// Products should be back to 5
		var productCount int
		err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM products").Scan(&productCount)
		if err != nil {
			t.Fatal(err)
		}
		if productCount != 5 {
			t.Errorf("expected 5 products (clean state), got %d", productCount)
		}

		// Orders should be empty
		var orderCount int
		err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM orders").Scan(&orderCount)
		if err != nil {
			t.Fatal(err)
		}
		if orderCount != 0 {
			t.Errorf("expected 0 orders (clean state), got %d", orderCount)
		}

		// Categories should still be 5 (excluded from truncation)
		var categoryCount int
		err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM categories").Scan(&categoryCount)
		if err != nil {
			t.Fatal(err)
		}
		if categoryCount != 5 {
			t.Errorf("expected 5 categories (preserved), got %d", categoryCount)
		}
	})
}

func TestLongRunningOperation(t *testing.T) {
	db, err := shared.Pool.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Simulate a long-running operation
	start := time.Now()
	
	// Create many orders
	for i := 0; i < 100; i++ {
		userID := (i % 3) + 1
		total := float64(i+1) * 9.99
		
		_, err := db.ExecContext(ctx,
			"INSERT INTO orders (user_id, total, status) VALUES ($1, $2, $3)",
			userID, total, "completed",
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	duration := time.Since(start)
	t.Logf("Created 100 orders in %v", duration)

	// Verify count
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM orders").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 100 {
		t.Errorf("expected 100 orders, got %d", count)
	}
}