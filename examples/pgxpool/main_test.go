package pgxpool_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/yuku/testdbpool/examples/pgxpool/shared"
	tpgxpool "github.com/yuku/testdbpool/pgxpool"
)

var poolWrapper *tpgxpool.Wrapper

func TestMain(m *testing.M) {
	// Use shared pool setup to ensure consistency across all packages
	var err error
	poolWrapper, err = shared.GetPoolWrapper()
	if err != nil {
		panic(err)
	}

	// Run tests
	os.Exit(m.Run())
}

func TestBasicPgxPoolUsage(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	pool, err := poolWrapper.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Get initial count
	var initialCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM posts").Scan(&initialCount)
	if err != nil {
		t.Fatal(err)
	}

	// Use pgx batch feature
	batch := &pgx.Batch{}
	batch.Queue("INSERT INTO posts (user_id, title, content) VALUES ($1, $2, $3)", 1, "Batch Post 1", "Hello World")
	batch.Queue("INSERT INTO posts (user_id, title, content) VALUES ($1, $2, $3)", 2, "Batch Post 2", "Another post")
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

	var finalCount int
	err = results.QueryRow().Scan(&finalCount)
	if err != nil {
		t.Fatal(err)
	}

	addedPosts := finalCount - initialCount
	if addedPosts != 2 {
		t.Errorf("expected to add 2 posts, added %d (initial: %d, final: %d)", addedPosts, initialCount, finalCount)
	}
}

func TestPgxCopyFrom(t *testing.T) {
	t.Parallel()
	pool, err := poolWrapper.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Get initial count
	var initialCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM posts").Scan(&initialCount)
	if err != nil {
		t.Fatal(err)
	}

	// Use pgx CopyFrom for bulk inserts
	rows := [][]any{
		{1, "Copy Post 1", "Content 1"},
		{2, "Copy Post 2", "Content 2"},
		{3, "Copy Post 3", "Content 3"},
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

	// Verify final count increased correctly
	var finalCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM posts").Scan(&finalCount)
	if err != nil {
		t.Fatal(err)
	}
	
	addedPosts := finalCount - initialCount
	if addedPosts != 3 {
		t.Errorf("expected to add 3 posts, added %d (initial: %d, final: %d)", addedPosts, initialCount, finalCount)
	}
}

func TestCustomPoolConfiguration(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
