package package2_test

import (
	"context"
	"testing"

	"github.com/yuku/testdbpool/examples/multiple-packages/shared"
)

func TestProductOperations(t *testing.T) {
	db, err := shared.Pool.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	t.Run("ListProducts", func(t *testing.T) {
		rows, err := db.QueryContext(ctx, "SELECT id, name, price, stock FROM products ORDER BY id")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		count := 0
		for rows.Next() {
			var id, stock int
			var name string
			var price float64
			if err := rows.Scan(&id, &name, &price, &stock); err != nil {
				t.Fatal(err)
			}
			count++
			t.Logf("Product %d: %s ($%.2f) - %d in stock", id, name, price, stock)
		}

		if count != 5 {
			t.Errorf("expected 5 products, got %d", count)
		}
	})

	t.Run("UpdateStock", func(t *testing.T) {
		// Reduce laptop stock by 1 (simulate purchase)
		result, err := db.ExecContext(ctx,
			"UPDATE products SET stock = stock - 1 WHERE id = 1 AND stock > 0")
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

		// Check new stock level
		var stock int
		err = db.QueryRowContext(ctx, "SELECT stock FROM products WHERE id = 1").Scan(&stock)
		if err != nil {
			t.Fatal(err)
		}
		if stock != 9 {
			t.Errorf("expected stock to be 9, got %d", stock)
		}
	})

	t.Run("AddProduct", func(t *testing.T) {
		_, err := db.ExecContext(ctx,
			"INSERT INTO products (name, price, stock) VALUES ($1, $2, $3)",
			"Keyboard", 79.99, 30)
		if err != nil {
			t.Fatal(err)
		}

		// Verify product count
		var count int
		err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM products").Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count != 6 {
			t.Errorf("expected 6 products after insert, got %d", count)
		}
	})
}

func TestProductCategories(t *testing.T) {
	db, err := shared.Pool.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	t.Run("VerifyCategoriesNotTruncated", func(t *testing.T) {
		// Categories should always have 5 entries (static table excluded from truncation)
		var count int
		err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM categories").Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count != 5 {
			t.Errorf("expected 5 categories (should be preserved), got %d", count)
		}
	})

	t.Run("ProductsByCategory", func(t *testing.T) {
		rows, err := db.QueryContext(ctx, `
			SELECT c.name, COUNT(pc.product_id) as product_count
			FROM categories c
			LEFT JOIN product_categories pc ON c.id = pc.category_id
			GROUP BY c.id, c.name
			ORDER BY c.name
		`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		electronics := 0
		for rows.Next() {
			var category string
			var count int
			if err := rows.Scan(&category, &count); err != nil {
				t.Fatal(err)
			}
			t.Logf("Category %s: %d products", category, count)
			
			if category == "Electronics" {
				electronics = count
			}
		}

		if electronics != 3 {
			t.Errorf("expected 3 electronics products, got %d", electronics)
		}
	})

	t.Run("AddProductToCategory", func(t *testing.T) {
		// Add a new product and assign it to a category
		var productID int
		err := db.QueryRowContext(ctx,
			"INSERT INTO products (name, price, stock) VALUES ($1, $2, $3) RETURNING id",
			"Mouse", 29.99, 100,
		).Scan(&productID)
		if err != nil {
			t.Fatal(err)
		}

		// Add to Electronics category (id=1)
		_, err = db.ExecContext(ctx,
			"INSERT INTO product_categories (product_id, category_id) VALUES ($1, $2)",
			productID, 1)
		if err != nil {
			t.Fatal(err)
		}

		// Verify electronics now has 4 products
		var count int
		err = db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM product_categories WHERE category_id = 1
		`).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count != 4 {
			t.Errorf("expected 4 electronics products after insert, got %d", count)
		}
	})
}

func TestConcurrentProductUpdates(t *testing.T) {
	db, err := shared.Pool.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Start transaction 1
	tx1, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx1.Rollback()

	// Start transaction 2  
	tx2, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx2.Rollback()

	// Both transactions read the same product
	var stock1, stock2 int
	err = tx1.QueryRowContext(ctx, "SELECT stock FROM products WHERE id = 2").Scan(&stock1)
	if err != nil {
		t.Fatal(err)
	}
	err = tx2.QueryRowContext(ctx, "SELECT stock FROM products WHERE id = 2").Scan(&stock2)
	if err != nil {
		t.Fatal(err)
	}

	if stock1 != stock2 {
		t.Errorf("both transactions should see the same stock: %d vs %d", stock1, stock2)
	}

	// Transaction 1 updates stock
	_, err = tx1.ExecContext(ctx, "UPDATE products SET stock = stock - 5 WHERE id = 2")
	if err != nil {
		t.Fatal(err)
	}

	// Commit transaction 1
	if err := tx1.Commit(); err != nil {
		t.Fatal(err)
	}

	// Transaction 2 tries to update stock (should still see old value)
	_, err = tx2.ExecContext(ctx, "UPDATE products SET stock = stock - 3 WHERE id = 2")
	if err != nil {
		t.Fatal(err)
	}

	// Commit transaction 2
	if err := tx2.Commit(); err != nil {
		t.Fatal(err)
	}

	// Check final stock
	var finalStock int
	err = db.QueryRowContext(ctx, "SELECT stock FROM products WHERE id = 2").Scan(&finalStock)
	if err != nil {
		t.Fatal(err)
	}

	// Stock should be original (25) - 5 - 3 = 17
	if finalStock != 17 {
		t.Errorf("expected final stock to be 17, got %d", finalStock)
	}
}