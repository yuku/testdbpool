package pgxpool_test

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/yuku/testdbpool"
	tpgxpool "github.com/yuku/testdbpool/pgxpool"
)

var poolWrapper *tpgxpool.Wrapper

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
		dbPassword = "postgres"
	}

	rootConnStr := fmt.Sprintf("postgres://%s:%s@%s:%s/postgres?sslmode=disable", dbUser, dbPassword, dbHost, dbPort)
	rootDB, err := sql.Open("pgx", rootConnStr)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = rootDB.Close() }()

	// Initialize test database pool
	pool, err := testdbpool.New(testdbpool.Configuration{
		RootConnection: rootDB,
		PoolID:         "pgxpool_example",
		MaxPoolSize:    10,
		TemplateCreator: func(ctx context.Context, db *sql.DB) error {
			// Create a simple test schema
			schema := `
				CREATE TABLE users (
					id SERIAL PRIMARY KEY,
					name VARCHAR(100) NOT NULL,
					email VARCHAR(100) UNIQUE NOT NULL,
					created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
				);

				CREATE TABLE posts (
					id SERIAL PRIMARY KEY,
					user_id INTEGER REFERENCES users(id),
					title VARCHAR(200) NOT NULL,
					content TEXT,
					created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
				);

				-- Insert some test data with explicit IDs
				INSERT INTO users (id, name, email) VALUES 
					(1, 'Alice', 'alice@example.com'),
					(2, 'Bob', 'bob@example.com'),
					(3, 'Charlie', 'charlie@example.com');
				
				-- Reset sequence
				SELECT setval('users_id_seq', 3);
			`
			_, err := db.ExecContext(ctx, schema)
			return err
		},
		ResetFunc: testdbpool.ResetByTruncate(
			[]string{"posts", "users"},
			func(ctx context.Context, db *sql.DB) error {
				// Re-insert test data with explicit IDs
				_, err := db.ExecContext(ctx, `
					INSERT INTO users (id, name, email) VALUES 
						(1, 'Alice', 'alice@example.com'),
						(2, 'Bob', 'bob@example.com'),
						(3, 'Charlie', 'charlie@example.com');
					SELECT setval('users_id_seq', 3);
				`)
				return err
			},
		),
	})
	if err != nil {
		log.Fatal(err)
	}

	// Create wrapper
	poolWrapper = tpgxpool.New(pool)

	// Run tests
	os.Exit(m.Run())
}

func TestBasicPgxPoolUsage(t *testing.T) {
	// Acquire a pgxpool
	pool, err := poolWrapper.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Test basic query
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("expected 3 users, got %d", count)
	}

	// Test pgx-specific features like named parameters
	var name string
	err = pool.QueryRow(ctx, "SELECT name FROM users WHERE email = $1", "alice@example.com").Scan(&name)
	if err != nil {
		t.Fatal(err)
	}
	if name != "Alice" {
		t.Errorf("expected Alice, got %s", name)
	}
}

func TestPgxBatchQueries(t *testing.T) {
	pool, err := poolWrapper.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Use pgx batch feature
	batch := &pgx.Batch{}
	batch.Queue("INSERT INTO posts (user_id, title, content) VALUES ($1, $2, $3)", 1, "First Post", "Hello World")
	batch.Queue("INSERT INTO posts (user_id, title, content) VALUES ($1, $2, $3)", 2, "Second Post", "Another post")
	batch.Queue("SELECT COUNT(*) FROM posts")

	results := pool.SendBatch(ctx, batch)
	defer func() { _ = results.Close() }()

	// Process batch results
	for i := 0; i < 2; i++ {
		_, err := results.Exec()
		if err != nil {
			t.Fatal(err)
		}
	}

	var count int
	err = results.QueryRow().Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != 2 {
		t.Errorf("expected 2 posts, got %d", count)
	}
}

func TestPgxCopyFrom(t *testing.T) {
	pool, err := poolWrapper.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Use pgx CopyFrom for bulk inserts
	rows := [][]any{
		{1, "Bulk Post 1", "Content 1"},
		{2, "Bulk Post 2", "Content 2"},
		{3, "Bulk Post 3", "Content 3"},
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

	// Verify
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM posts").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("expected 3 posts, got %d", count)
	}
}

func TestCustomPoolConfiguration(t *testing.T) {
	// Acquire pool with custom configuration
	pool, err := poolWrapper.AcquireWithConfig(t, func(config *pgxpool.Config) {
		config.MaxConns = 5
		config.MinConns = 1
		config.MaxConnLifetime = 5 * time.Minute
		config.MaxConnIdleTime = 1 * time.Minute

		// Add custom query tracer
		config.ConnConfig.Tracer = &testTracer{t: t}
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Test that our custom configuration is applied
	stats := pool.Stat()
	if stats.MaxConns() != 5 {
		t.Errorf("expected MaxConns 5, got %d", stats.MaxConns())
	}

	// Execute a query that will be traced
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
}

// testTracer is a simple tracer for testing
type testTracer struct {
	t *testing.T
}

func (tt *testTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	tt.t.Logf("Query started: %s", data.SQL)
	return ctx
}

func (tt *testTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	tt.t.Logf("Query ended: %s", data.CommandTag)
}

func TestBothInterfaces(t *testing.T) {
	// Get both sql.DB and pgxpool.Pool
	sqlDB, pgxPool, err := poolWrapper.AcquireBoth(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Use sql.DB interface
	var countSQL int
	err = sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&countSQL)
	if err != nil {
		t.Fatal(err)
	}

	// Use pgxpool interface
	var countPgx int
	err = pgxPool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&countPgx)
	if err != nil {
		t.Fatal(err)
	}

	if countSQL != countPgx {
		t.Errorf("counts don't match: sql=%d, pgx=%d", countSQL, countPgx)
	}
}

func TestConcurrentPgxPoolAccess(t *testing.T) {
	pool, err := poolWrapper.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	var wg sync.WaitGroup

	// Run concurrent queries
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Each goroutine performs multiple queries
			for j := 0; j < 5; j++ {
				var email string
				err := pool.QueryRow(ctx, "SELECT email FROM users WHERE id = $1", (id%3)+1).Scan(&email)
				if err != nil {
					t.Errorf("goroutine %d: %v", id, err)
					return
				}
			}
		}(i)
	}

	wg.Wait()

	// Check pool statistics
	stats := pool.Stat()
	t.Logf("Pool stats - Total: %d, Idle: %d, Acquired: %d",
		stats.TotalConns(), stats.IdleConns(), stats.AcquiredConns())
}

func TestPgxNotifications(t *testing.T) {
	pool, err := poolWrapper.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Acquire a connection for listening
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()

	// Listen for notifications
	_, err = conn.Exec(ctx, "LISTEN test_channel")
	if err != nil {
		t.Fatal(err)
	}

	// Send notification from another connection
	_, err = pool.Exec(ctx, "NOTIFY test_channel, 'Hello from pgx!'")
	if err != nil {
		t.Fatal(err)
	}

	// Wait for notification
	notification, err := conn.Conn().WaitForNotification(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if notification.Channel != "test_channel" {
		t.Errorf("expected channel test_channel, got %s", notification.Channel)
	}
	if notification.Payload != "Hello from pgx!" {
		t.Errorf("expected payload 'Hello from pgx!', got %s", notification.Payload)
	}
}
