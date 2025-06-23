package testdbpool_test

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"testing"

	_ "github.com/lib/pq"
	"github.com/yuku/testdbpool"
)

// This example demonstrates how to use testdbpool in your tests

var examplePool *testdbpool.Pool

func TestMain(m *testing.M) {
	// Connect to PostgreSQL
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "localhost"
	}
	user := os.Getenv("PGUSER")
	if user == "" {
		user = "postgres"
	}
	password := os.Getenv("PGPASSWORD")
	if password == "" {
		password = "postgres"
	}

	connStr := fmt.Sprintf("postgres://%s:%s@%s/postgres?sslmode=disable", user, password, host)
	rootDB, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}
	defer func() { _ = rootDB.Close() }()

	// Always clean up any existing pool before tests
	if err := testdbpool.Cleanup(rootDB, "example_test"); err != nil {
		log.Printf("Failed to cleanup existing pool: %v", err)
	}

	// Create the pool
	examplePool, err = testdbpool.New(testdbpool.Configuration{
		RootConnection: rootDB,
		PoolID:         "example_test",
		MaxPoolSize:    5,
		TemplateCreator: func(ctx context.Context, db *sql.DB) error {
			// Create your schema
			queries := []string{
				`CREATE TABLE products (
					id SERIAL PRIMARY KEY,
					name VARCHAR(100) NOT NULL,
					price DECIMAL(10,2) NOT NULL,
					stock INTEGER NOT NULL DEFAULT 0
				)`,
				`CREATE TABLE orders (
					id SERIAL PRIMARY KEY,
					product_id INTEGER REFERENCES products(id),
					quantity INTEGER NOT NULL,
					total DECIMAL(10,2) NOT NULL,
					created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
				)`,
				// Add initial data
				`INSERT INTO products (name, price, stock) VALUES 
					('Widget', 9.99, 100),
					('Gadget', 19.99, 50),
					('Doohickey', 4.99, 200)`,
			}

			for _, query := range queries {
				if _, err := db.ExecContext(ctx, query); err != nil {
					return fmt.Errorf("failed to execute query: %w", err)
				}
			}
			return nil
		},
		ResetFunc: testdbpool.ResetByTruncate(
			[]string{}, // truncate all tables
			func(ctx context.Context, db *sql.DB) error {
				// Restore initial data
				_, err := db.ExecContext(ctx, `
					INSERT INTO products (id, name, price, stock) VALUES 
					(1, 'Widget', 9.99, 100),
					(2, 'Gadget', 19.99, 50),
					(3, 'Doohickey', 4.99, 200);
					SELECT setval('products_id_seq', 3);
				`)
				return err
			},
		),
	})
	if err != nil {
		log.Fatalf("Failed to create pool: %v", err)
	}

	// Run tests
	code := m.Run()

	// Always clean up after tests
	if err := testdbpool.Cleanup(rootDB, "example_test"); err != nil {
		log.Printf("Failed to cleanup pool after tests: %v", err)
	}

	os.Exit(code)
}

func ExamplePool_Acquire() {
	// This would normally be in a test function
	t := &testing.T{}

	// Acquire a database from the pool
	db, err := examplePool.Acquire(t)
	if err != nil {
		log.Fatalf("Failed to acquire database: %v", err)
	}

	// Use the database
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM products").Scan(&count)
	if err != nil {
		log.Fatalf("Failed to count products: %v", err)
	}

	fmt.Printf("Number of products: %d\n", count)
	// Output: Number of products: 3
}

func TestProductOperations(t *testing.T) {
	db, err := examplePool.Acquire(t)
	if err != nil {
		t.Fatalf("Failed to acquire database: %v", err)
	}

	// Test: Create an order
	t.Run("CreateOrder", func(t *testing.T) {
		var productID int
		var price float64
		err := db.QueryRow("SELECT id, price FROM products WHERE name = $1", "Widget").Scan(&productID, &price)
		if err != nil {
			t.Fatalf("Failed to get product: %v", err)
		}

		quantity := 5
		total := price * float64(quantity)

		_, err = db.Exec(`
			INSERT INTO orders (product_id, quantity, total) 
			VALUES ($1, $2, $3)`,
			productID, quantity, total)
		if err != nil {
			t.Fatalf("Failed to create order: %v", err)
		}

		// Verify order was created
		var orderCount int
		err = db.QueryRow("SELECT COUNT(*) FROM orders").Scan(&orderCount)
		if err != nil {
			t.Fatalf("Failed to count orders: %v", err)
		}
		if orderCount != 1 {
			t.Errorf("Expected 1 order, got %d", orderCount)
		}
	})
}

func TestIsolation(t *testing.T) {
	// Each test gets a clean database
	db, err := examplePool.Acquire(t)
	if err != nil {
		t.Fatalf("Failed to acquire database: %v", err)
	}

	// Verify no orders exist (cleaned from previous test)
	var orderCount int
	err = db.QueryRow("SELECT COUNT(*) FROM orders").Scan(&orderCount)
	if err != nil {
		t.Fatalf("Failed to count orders: %v", err)
	}
	if orderCount != 0 {
		t.Errorf("Expected 0 orders (clean database), got %d", orderCount)
	}

	// Verify products were restored
	var productCount int
	err = db.QueryRow("SELECT COUNT(*) FROM products").Scan(&productCount)
	if err != nil {
		t.Fatalf("Failed to count products: %v", err)
	}
	if productCount != 3 {
		t.Errorf("Expected 3 products, got %d", productCount)
	}
}
