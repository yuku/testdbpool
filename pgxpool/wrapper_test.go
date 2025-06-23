package pgxpool_test

import (
	"context"
	"database/sql"
	"fmt"
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

var (
	testPool    *testdbpool.Pool
	poolWrapper *tpgxpool.Wrapper
)

func TestMain(m *testing.M) {
	// Setup PostgreSQL connection
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
		panic(err)
	}
	defer rootDB.Close()

	// Initialize test database pool
	testPool, err = testdbpool.New(testdbpool.Configuration{
		RootConnection: rootDB,
		PoolID:         "pgxpool_wrapper_test",
		MaxPoolSize:    10,
		TemplateCreator: func(ctx context.Context, db *sql.DB) error {
			schema := `
				CREATE TABLE test_data (
					id SERIAL PRIMARY KEY,
					name VARCHAR(100) NOT NULL,
					value INTEGER,
					created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
				);
				
				INSERT INTO test_data (name, value) VALUES
					('test1', 100),
					('test2', 200),
					('test3', 300);
			`
			_, err := db.ExecContext(ctx, schema)
			return err
		},
		ResetFunc: testdbpool.ResetByTruncate(
			[]string{"test_data"},
			func(ctx context.Context, db *sql.DB) error {
				_, err := db.ExecContext(ctx, `
					INSERT INTO test_data (name, value) VALUES
						('test1', 100),
						('test2', 200),
						('test3', 300)
				`)
				return err
			},
		),
	})
	if err != nil {
		panic(err)
	}

	// Create wrapper
	poolWrapper = tpgxpool.New(testPool)

	os.Exit(m.Run())
}

func TestBasicAcquire(t *testing.T) {
	pool, err := poolWrapper.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Test basic query
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM test_data").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != 3 {
		t.Errorf("expected 3 rows, got %d", count)
	}

	// Test pgx-specific feature
	var name string
	var value int
	err = pool.QueryRow(ctx, "SELECT name, value FROM test_data WHERE name = $1", "test1").Scan(&name, &value)
	if err != nil {
		t.Fatal(err)
	}

	if name != "test1" || value != 100 {
		t.Errorf("expected test1/100, got %s/%d", name, value)
	}
}

func TestAcquireWithConfig(t *testing.T) {
	called := false
	
	pool, err := poolWrapper.AcquireWithConfig(t, func(config *pgxpool.Config) {
		called = true
		config.MaxConns = 5
		config.MinConns = 1
		config.MaxConnLifetime = 5 * time.Minute
	})
	if err != nil {
		t.Fatal(err)
	}

	if !called {
		t.Error("config function was not called")
	}

	// Verify configuration was applied
	stats := pool.Stat()
	if stats.MaxConns() != 5 {
		t.Errorf("expected MaxConns 5, got %d", stats.MaxConns())
	}
}

func TestAcquireBoth(t *testing.T) {
	sqlDB, pgxPool, err := poolWrapper.AcquireBoth(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Test sql.DB interface
	var countSQL int
	err = sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_data").Scan(&countSQL)
	if err != nil {
		t.Fatal(err)
	}

	// Test pgxpool interface
	var countPgx int
	err = pgxPool.QueryRow(ctx, "SELECT COUNT(*) FROM test_data").Scan(&countPgx)
	if err != nil {
		t.Fatal(err)
	}

	if countSQL != countPgx {
		t.Errorf("counts don't match: sql=%d, pgx=%d", countSQL, countPgx)
	}
}

func TestPasswordSources(t *testing.T) {
	t.Run("DefaultPasswordSource", func(t *testing.T) {
		// Save current env
		oldPassword := os.Getenv("DB_PASSWORD")
		defer os.Setenv("DB_PASSWORD", oldPassword)

		// Test with DB_PASSWORD
		os.Setenv("DB_PASSWORD", "testpass123")
		password, err := tpgxpool.DefaultPasswordSource()
		if err != nil {
			t.Fatal(err)
		}
		if password != "testpass123" {
			t.Errorf("expected testpass123, got %s", password)
		}

		// Test with no password
		os.Unsetenv("DB_PASSWORD")
		os.Unsetenv("PGPASSWORD")
		os.Unsetenv("POSTGRES_PASSWORD")
		password, err = tpgxpool.DefaultPasswordSource()
		if err != nil {
			t.Fatal(err)
		}
		if password != "" {
			t.Errorf("expected empty password, got %s", password)
		}
	})

	t.Run("EnvPasswordSource", func(t *testing.T) {
		os.Setenv("MY_CUSTOM_PASSWORD", "custom123")
		defer os.Unsetenv("MY_CUSTOM_PASSWORD")

		source := tpgxpool.EnvPasswordSource("MY_CUSTOM_PASSWORD")
		password, err := source()
		if err != nil {
			t.Fatal(err)
		}
		if password != "custom123" {
			t.Errorf("expected custom123, got %s", password)
		}

		// Test missing env var
		source = tpgxpool.EnvPasswordSource("NONEXISTENT_VAR")
		_, err = source()
		if err == nil {
			t.Error("expected error for missing env var")
		}
	})

	t.Run("StaticPasswordSource", func(t *testing.T) {
		source := tpgxpool.StaticPasswordSource("static123")
		password, err := source()
		if err != nil {
			t.Fatal(err)
		}
		if password != "static123" {
			t.Errorf("expected static123, got %s", password)
		}
	})
}

func TestHostSources(t *testing.T) {
	t.Run("EnvHostSource", func(t *testing.T) {
		os.Setenv("MY_HOST", "myhost.example.com")
		os.Setenv("MY_PORT", "5433")
		defer os.Unsetenv("MY_HOST")
		defer os.Unsetenv("MY_PORT")

		source := tpgxpool.EnvHostSource("MY_HOST", "MY_PORT")
		host, port, err := source(nil)
		if err != nil {
			t.Fatal(err)
		}
		if host != "myhost.example.com" {
			t.Errorf("expected myhost.example.com, got %s", host)
		}
		if port != "5433" {
			t.Errorf("expected 5433, got %s", port)
		}

		// Test missing host
		source = tpgxpool.EnvHostSource("NONEXISTENT_HOST", "MY_PORT")
		_, _, err = source(nil)
		if err == nil {
			t.Error("expected error for missing host env var")
		}
	})
}

func TestCustomConfiguration(t *testing.T) {
	// Get proper host from environment
	dbHost := os.Getenv("DB_HOST")
	if dbHost == "" {
		dbHost = "localhost"
	}
	dbPort := os.Getenv("DB_PORT")
	if dbPort == "" {
		dbPort = "5432"
	}
	dbPassword := os.Getenv("DB_PASSWORD")
	if dbPassword == "" {
		dbPassword = "postgres"
	}
	
	// Create wrapper with custom configuration
	customWrapper := tpgxpool.NewWithConfig(testPool, tpgxpool.Config{
		PasswordSource: tpgxpool.StaticPasswordSource(dbPassword),
		HostSource: func(db *sql.DB) (string, string, error) {
			// Return proper host/port from environment
			return dbHost, dbPort, nil
		},
		AdditionalParams: "application_name=test_app&statement_timeout=30000",
	})

	pool, err := customWrapper.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Verify the connection works
	var appName string
	err = pool.QueryRow(ctx, "SELECT current_setting('application_name')").Scan(&appName)
	if err != nil {
		t.Fatal(err)
	}

	if appName != "test_app" {
		t.Errorf("expected application_name=test_app, got %s", appName)
	}
}

func TestConcurrentAcquire(t *testing.T) {
	var wg sync.WaitGroup
	errors := make(chan error, 5)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			pool, err := poolWrapper.Acquire(t)
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: acquire failed: %w", id, err)
				return
			}

			ctx := context.Background()

			// Insert unique data
			_, err = pool.Exec(ctx, "INSERT INTO test_data (name, value) VALUES ($1, $2)",
				fmt.Sprintf("concurrent_%d", id), id*100)
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: insert failed: %w", id, err)
				return
			}

			// Verify insert
			var value int
			err = pool.QueryRow(ctx, "SELECT value FROM test_data WHERE name = $1",
				fmt.Sprintf("concurrent_%d", id)).Scan(&value)
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: select failed: %w", id, err)
				return
			}

			if value != id*100 {
				errors <- fmt.Errorf("goroutine %d: expected %d, got %d", id, id*100, value)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}

func TestPgxSpecificFeatures(t *testing.T) {
	pool, err := poolWrapper.Acquire(t)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	t.Run("Batch", func(t *testing.T) {
		batch := &pgx.Batch{}
		batch.Queue("INSERT INTO test_data (name, value) VALUES ($1, $2)", "batch1", 1000)
		batch.Queue("INSERT INTO test_data (name, value) VALUES ($1, $2)", "batch2", 2000)
		batch.Queue("SELECT COUNT(*) FROM test_data WHERE name LIKE 'batch%'")

		results := pool.SendBatch(ctx, batch)
		defer results.Close()

		// Process results
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
			t.Errorf("expected 2 batch inserts, got %d", count)
		}
	})

	t.Run("CopyFrom", func(t *testing.T) {
		rows := [][]any{
			{"copy1", 10000},
			{"copy2", 20000},
			{"copy3", 30000},
		}

		copyCount, err := pool.CopyFrom(
			ctx,
			pgx.Identifier{"test_data"},
			[]string{"name", "value"},
			pgx.CopyFromRows(rows),
		)
		if err != nil {
			t.Fatal(err)
		}

		if copyCount != 3 {
			t.Errorf("expected to copy 3 rows, got %d", copyCount)
		}
	})

	t.Run("Transaction", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)

		// Insert in transaction
		_, err = tx.Exec(ctx, "INSERT INTO test_data (name, value) VALUES ($1, $2)", "tx_test", 99999)
		if err != nil {
			t.Fatal(err)
		}

		// Verify within transaction
		var value int
		err = tx.QueryRow(ctx, "SELECT value FROM test_data WHERE name = $1", "tx_test").Scan(&value)
		if err != nil {
			t.Fatal(err)
		}

		if value != 99999 {
			t.Errorf("expected 99999, got %d", value)
		}

		// Rollback and verify it's gone
		tx.Rollback(ctx)

		err = pool.QueryRow(ctx, "SELECT value FROM test_data WHERE name = $1", "tx_test").Scan(&value)
		if err != pgx.ErrNoRows {
			t.Error("expected no rows after rollback")
		}
	})
}