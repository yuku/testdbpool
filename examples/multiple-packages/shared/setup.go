package shared

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	_ "github.com/lib/pq"
	"github.com/yuku/testdbpool"
)

var Pool *testdbpool.Pool

// InitializePool sets up the test database pool for all packages
func InitializePool() error {
	// Get database connection parameters
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	user := os.Getenv("PGUSER")
	if user == "" {
		user = "postgres"
	}
	password := os.Getenv("PGPASSWORD")
	if password == "" {
		password = "postgres"
	}

	// Connect to PostgreSQL
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/postgres?sslmode=disable", user, password, host, port)
	rootDB, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	// Don't close rootDB here - it's needed by the pool

	// Clean up any existing pool
	if err := testdbpool.Cleanup(rootDB, "multi_pkg_example"); err != nil {
		// Log but don't fail - pool might not exist
		fmt.Printf("Warning: failed to cleanup existing pool: %v\n", err)
	}

	// Create the pool
	Pool, err = testdbpool.New(testdbpool.Configuration{
		RootConnection: rootDB,
		PoolID:         "multi_pkg_example",
		MaxPoolSize:    10,
		TemplateCreator: func(ctx context.Context, db *sql.DB) error {
			// Create schema
			schema := `
				-- Users table
				CREATE TABLE users (
					id SERIAL PRIMARY KEY,
					username VARCHAR(100) UNIQUE NOT NULL,
					email VARCHAR(100) UNIQUE NOT NULL,
					created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
				);

				-- Products table  
				CREATE TABLE products (
					id SERIAL PRIMARY KEY,
					name VARCHAR(200) NOT NULL,
					price DECIMAL(10,2) NOT NULL,
					stock INTEGER NOT NULL DEFAULT 0,
					created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
				);

				-- Orders table
				CREATE TABLE orders (
					id SERIAL PRIMARY KEY,
					user_id INTEGER REFERENCES users(id),
					total DECIMAL(10,2) NOT NULL,
					status VARCHAR(50) NOT NULL DEFAULT 'pending',
					created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
				);

				-- Order items table
				CREATE TABLE order_items (
					id SERIAL PRIMARY KEY,
					order_id INTEGER REFERENCES orders(id),
					product_id INTEGER REFERENCES products(id),
					quantity INTEGER NOT NULL,
					price DECIMAL(10,2) NOT NULL
				);

				-- Categories table (static/enum table)
				CREATE TABLE categories (
					id SERIAL PRIMARY KEY,
					name VARCHAR(100) UNIQUE NOT NULL,
					description TEXT
				);

				-- Product categories junction table
				CREATE TABLE product_categories (
					product_id INTEGER REFERENCES products(id),
					category_id INTEGER REFERENCES categories(id),
					PRIMARY KEY (product_id, category_id)
				);

				-- Insert static category data
				INSERT INTO categories (id, name, description) VALUES
					(1, 'Electronics', 'Electronic devices and accessories'),
					(2, 'Books', 'Physical and digital books'),
					(3, 'Clothing', 'Apparel and fashion items'),
					(4, 'Food', 'Food and beverages'),
					(5, 'Home', 'Home and garden products');

				-- Insert initial test data
				INSERT INTO users (id, username, email) VALUES
					(1, 'alice', 'alice@example.com'),
					(2, 'bob', 'bob@example.com'),
					(3, 'charlie', 'charlie@example.com');

				INSERT INTO products (id, name, price, stock) VALUES
					(1, 'Laptop', 999.99, 10),
					(2, 'Phone', 599.99, 25),
					(3, 'Headphones', 199.99, 50),
					(4, 'Book: Go Programming', 39.99, 100),
					(5, 'T-Shirt', 19.99, 200);

				INSERT INTO product_categories (product_id, category_id) VALUES
					(1, 1), -- Laptop -> Electronics
					(2, 1), -- Phone -> Electronics  
					(3, 1), -- Headphones -> Electronics
					(4, 2), -- Book -> Books
					(5, 3); -- T-Shirt -> Clothing

				-- Reset sequences
				SELECT setval('users_id_seq', 3);
				SELECT setval('products_id_seq', 5);
				SELECT setval('categories_id_seq', 5);
			`

			_, err := db.ExecContext(ctx, schema)
			return err
		},
		ResetFunc: testdbpool.ResetByTruncate(
			[]string{"categories"}, // Exclude static category table
			func(ctx context.Context, db *sql.DB) error {
				// Re-insert test data (not categories - they're preserved)
				seed := `
					INSERT INTO users (id, username, email) VALUES
						(1, 'alice', 'alice@example.com'),
						(2, 'bob', 'bob@example.com'),
						(3, 'charlie', 'charlie@example.com');

					INSERT INTO products (id, name, price, stock) VALUES
						(1, 'Laptop', 999.99, 10),
						(2, 'Phone', 599.99, 25),
						(3, 'Headphones', 199.99, 50),
						(4, 'Book: Go Programming', 39.99, 100),
						(5, 'T-Shirt', 19.99, 200);

					INSERT INTO product_categories (product_id, category_id) VALUES
						(1, 1), (2, 1), (3, 1), (4, 2), (5, 3);

					-- Reset sequences
					SELECT setval('users_id_seq', 3);
					SELECT setval('products_id_seq', 5);
					SELECT setval('orders_id_seq', 1, false);
					SELECT setval('order_items_id_seq', 1, false);
				`
				_, err := db.ExecContext(ctx, seed)
				return err
			},
		),
	})

	return err
}

// CleanupPool cleans up the test database pool
func CleanupPool() error {
	// Connect to PostgreSQL
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	user := os.Getenv("PGUSER")
	if user == "" {
		user = "postgres"
	}
	password := os.Getenv("PGPASSWORD")
	if password == "" {
		password = "postgres"
	}

	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/postgres?sslmode=disable", user, password, host, port)
	rootDB, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	defer rootDB.Close()

	return testdbpool.Cleanup(rootDB, "multi_pkg_example")
}