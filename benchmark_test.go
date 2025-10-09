// Package testdbpool_test contains performance benchmarks for the DROP DATABASE strategy.
//
// Performance Summary (based on benchmark results):
//
// 1. Simple Acquire/Release Cycle:
//   - DROP strategy: ~121ms per cycle (consistent performance)
//
// 2. Data Operations:
//   - DROP strategy: ~97ms per operation (with concurrent execution)
//
// 3. Large Schema (10 tables with indexes):
//   - DROP strategy: ~214ms per operation (efficient with complex schemas)
//
// Strategy Selection:
// This library uses the DROP DATABASE strategy exclusively for the following reasons:
// - Reliable concurrency support (no resource contention issues)
// - Complete data isolation between test runs
// - Better performance with complex database schemas
// - Simplified maintenance (single strategy to support)
package testdbpool_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuku/numpool"
	"github.com/yuku/testdbpool"
)

// BenchmarkAcquireReleaseCycle benchmarks the basic acquire/release cycle
func BenchmarkAcquireReleaseCycle(b *testing.B) {
	ctx := context.Background()
	connPool := getBenchmarkDBPool(b)
	defer cleanupBenchmarkNumpool(connPool)

	poolID := "basic_benchmark"
	createBenchmarkTemplateDB(b, ctx, connPool, poolID)
	pool := createBenchmarkPool(b, ctx, connPool, poolID)
	defer pool.Cleanup()

	for range b.N {
		db, err := pool.Acquire(ctx)
		if err != nil {
			b.Fatal(err)
		}

		err = db.Release(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWithDataOperations benchmarks acquire/release with actual data operations
func BenchmarkWithDataOperations(b *testing.B) {
	ctx := context.Background()
	connPool := getBenchmarkDBPool(b)
	defer cleanupBenchmarkNumpool(connPool)

	poolID := "data_benchmark"
	createBenchmarkTemplateDB(b, ctx, connPool, poolID)
	pool := createBenchmarkPool(b, ctx, connPool, poolID)
	defer pool.Cleanup()

	for i := range b.N {
		db, err := pool.Acquire(ctx)
		if err != nil {
			b.Fatal(err)
		}

		// Insert some data
		_, err = db.Pool().Exec(ctx, `INSERT INTO bench_items (name, value) VALUES ($1, $2)`, "test", i)
		if err != nil {
			b.Fatal(err)
		}

		// Query the data
		var count int
		err = db.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM bench_items`).Scan(&count)
		if err != nil {
			b.Fatal(err)
		}

		err = db.Release(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkConcurrentUsage benchmarks concurrent database usage
func BenchmarkConcurrentUsage(b *testing.B) {
	ctx := context.Background()
	connPool := getBenchmarkDBPool(b)
	defer cleanupBenchmarkNumpool(connPool)

	poolID := "concurrent_benchmark"
	createBenchmarkTemplateDB(b, ctx, connPool, poolID)
	pool := createBenchmarkPool(b, ctx, connPool, poolID)
	defer pool.Cleanup()

	b.ResetTimer()
	b.SetParallelism(4) // Test with moderate concurrency
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Use timeout context to prevent hangs
			acquireCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			db, err := pool.Acquire(acquireCtx)
			cancel()
			if err != nil {
				b.Error(err)
				continue
			}

			// Quick operation
			_, err = db.Pool().Exec(ctx, `INSERT INTO bench_items (name, value) VALUES ($1, $2)`, "concurrent", 1)
			if err != nil {
				b.Error(err)
			}

			err = db.Release(ctx)
			if err != nil {
				b.Error(err)
			}
		}
	})
}

// BenchmarkLargeSchema benchmarks performance with complex database schemas
func BenchmarkLargeSchema(b *testing.B) {
	ctx := context.Background()
	connPool := getBenchmarkDBPool(b)
	defer cleanupBenchmarkNumpool(connPool)

	createLargeSchemaSetup := func(ctx context.Context, conn *pgx.Conn) error {
		// Create multiple tables with indexes and constraints
		for i := range 10 {
			_, err := conn.Exec(ctx, `
				CREATE TABLE `+fmt.Sprintf("bench_table_%d", i)+` (
					id SERIAL PRIMARY KEY,
					name TEXT NOT NULL,
					value INTEGER DEFAULT 0,
					created_at TIMESTAMP DEFAULT NOW()
				)
			`)
			if err != nil {
				return err
			}

			// Add indexes
			_, err = conn.Exec(ctx, `CREATE INDEX idx_`+fmt.Sprintf("bench_table_%d", i)+`_name ON `+fmt.Sprintf("bench_table_%d", i)+` (name)`)
			if err != nil {
				return err
			}
		}
		return nil
	}

	poolID := "large_schema_benchmark"
	createBenchmarkTemplateDatabaseCustom(b, ctx, connPool, poolID, createLargeSchemaSetup)

	pool, err := testdbpool.New(ctx, &testdbpool.Config{
		ID:           poolID,
		Pool:         connPool,
		MaxDatabases: 4,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer pool.Cleanup()

	for i := range b.N {
		db, err := pool.Acquire(ctx)
		if err != nil {
			b.Fatal(err)
		}

		// Insert into a few tables
		for j := range 3 {
			_, err = db.Pool().Exec(ctx, fmt.Sprintf("INSERT INTO bench_table_%d (name, value) VALUES ($1, $2)", j), "test", i)
			if err != nil {
				b.Fatal(err)
			}
		}

		err = db.Release(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// createBenchmarkTemplateDB creates a template database for benchmarking
func createBenchmarkTemplateDB(b *testing.B, ctx context.Context, connPool *pgxpool.Pool, poolID string) {
	b.Helper()
	createBenchmarkTemplateDatabaseCustom(b, ctx, connPool, poolID, func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, `CREATE TABLE bench_items (id SERIAL PRIMARY KEY, name TEXT, value INTEGER)`)
		return err
	})
}

// createBenchmarkTemplateDatabaseCustom creates a template database with custom setup for benchmarking
func createBenchmarkTemplateDatabaseCustom(b *testing.B, ctx context.Context, connPool *pgxpool.Pool, poolID string, setup func(context.Context, *pgx.Conn) error) {
	b.Helper()

	templateName := fmt.Sprintf("testdbpooltmpl_%s", poolID)

	// Drop template database if it exists (cleanup from previous test runs)
	_, _ = connPool.Exec(ctx, fmt.Sprintf(
		"ALTER DATABASE %s IS_TEMPLATE false",
		pgx.Identifier{templateName}.Sanitize(),
	))
	_, _ = connPool.Exec(ctx, fmt.Sprintf(
		"DROP DATABASE IF EXISTS %s",
		pgx.Identifier{templateName}.Sanitize(),
	))

	// Create template database
	_, err := connPool.Exec(ctx, fmt.Sprintf(
		"CREATE DATABASE %s IS_TEMPLATE true",
		pgx.Identifier{templateName}.Sanitize(),
	))
	if err != nil {
		b.Fatalf("failed to create template database: %v", err)
	}

	// Connect to template database
	cfg := connPool.Config().ConnConfig.Copy()
	cfg.Database = templateName
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		b.Fatalf("failed to connect to template database: %v", err)
	}
	defer func() {
		_ = conn.Close(ctx)
	}()

	// Run setup function
	err = setup(ctx, conn)
	if err != nil {
		b.Fatalf("failed to setup template database: %v", err)
	}
}

// createBenchmarkPool creates a test pool for benchmarking
func createBenchmarkPool(b *testing.B, ctx context.Context, connPool *pgxpool.Pool, id string) *testdbpool.Pool {
	pool, err := testdbpool.New(ctx, &testdbpool.Config{
		ID:           id,
		Pool:         connPool,
		MaxDatabases: 8, // Sufficient databases for concurrent benchmarking
	})
	if err != nil {
		b.Fatal(err)
	}
	return pool
}

// getBenchmarkDBPool returns a pgxpool.Pool for benchmarking
func getBenchmarkDBPool(b *testing.B) *pgxpool.Pool {
	b.Helper()

	connString := os.Getenv("DATABASE_URL")
	if connString == "" {
		connString = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
	}

	pool, err := pgxpool.New(context.Background(), connString)
	if err != nil {
		b.Fatalf("failed to create connection pool: %v", err)
	}

	return pool
}

// cleanupBenchmarkNumpool cleans up numpool for benchmarks
func cleanupBenchmarkNumpool(pool *pgxpool.Pool) {
	_ = numpool.Cleanup(context.Background(), pool)
	pool.Close()
}
