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

	pool := createBenchmarkPool(b, ctx, connPool, "basic_benchmark")
	defer pool.Cleanup()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
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

	pool := createBenchmarkPool(b, ctx, connPool, "data_benchmark")
	defer pool.Cleanup()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
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

	pool := createBenchmarkPool(b, ctx, connPool, "concurrent_benchmark")
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
		for i := 0; i < 10; i++ {
			_, err := conn.Exec(ctx, `
				CREATE TABLE bench_table_`+string(rune('0'+i))+` (
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
			_, err = conn.Exec(ctx, `CREATE INDEX idx_bench_table_`+string(rune('0'+i))+`_name ON bench_table_`+string(rune('0'+i))+` (name)`)
			if err != nil {
				return err
			}
		}
		return nil
	}

	pool, err := testdbpool.New(ctx, &testdbpool.Config{
		ID:            "large_schema_benchmark",
		Pool:          connPool,
		MaxDatabases:  4,
		SetupTemplate: createLargeSchemaSetup,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer pool.Cleanup()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db, err := pool.Acquire(ctx)
		if err != nil {
			b.Fatal(err)
		}

		// Insert into a few tables
		for j := 0; j < 3; j++ {
			_, err = db.Pool().Exec(ctx, `INSERT INTO bench_table_`+string(rune('0'+j))+` (name, value) VALUES ($1, $2)`, "test", i)
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

// createBenchmarkPool creates a test pool for benchmarking
func createBenchmarkPool(b *testing.B, ctx context.Context, connPool *pgxpool.Pool, id string) *testdbpool.Pool {
	pool, err := testdbpool.New(ctx, &testdbpool.Config{
		ID:           id,
		Pool:         connPool,
		MaxDatabases: 8, // Sufficient databases for concurrent benchmarking
		SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, `CREATE TABLE bench_items (id SERIAL PRIMARY KEY, name TEXT, value INTEGER)`)
			return err
		},
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
