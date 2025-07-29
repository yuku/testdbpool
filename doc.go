// Package testdbpool provides efficient test database pooling for PostgreSQL integration tests.
//
// testdbpool manages a pool of test databases that can be shared across multiple test functions,
// significantly improving test performance by reusing databases instead of creating and destroying
// them for each test. Built on top of numpool for resource management, it uses PostgreSQL's
// template database feature for fast database creation and maintains connection pools for efficiency.
//
// # Key Features
//
//   - Template-based database creation using PostgreSQL's CREATE DATABASE ... TEMPLATE
//   - Connection pool reuse to eliminate connection establishment overhead
//   - Concurrent test support with fair resource allocation
//   - Automatic database reset between test uses
//   - Resource-efficient operation ideal for CI environments
//   - Cross-package pool sharing using common identifiers
//
// # Basic Usage
//
// The typical usage pattern involves setting up a shared pool in TestMain and using it
// across multiple test functions:
//
//	func TestMain(m *testing.M) {
//		ctx := context.Background()
//
//		// Create connection pool to PostgreSQL server
//		connPool, err := pgxpool.New(ctx, "postgres://user:pass@localhost/postgres")
//		if err != nil {
//			panic(err)
//		}
//
//		// Configure test database pool
//		config := &testdbpool.Config{
//			ID:           "myapp-test",
//			Pool:         connPool,
//			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
//				_, err := conn.Exec(ctx, `CREATE TABLE users (id SERIAL PRIMARY KEY, name TEXT)`)
//				return err
//			},
//			ResetDatabase: func(ctx context.Context, pool *pgxpool.Pool) error {
//				_, err := pool.Exec(ctx, `TRUNCATE TABLE users RESTART IDENTITY`)
//				return err
//			},
//		}
//
//		// Create the test pool
//		testPool, err = testdbpool.New(ctx, config)
//		if err != nil {
//			panic(err)
//		}
//
//		// Run tests
//		code := m.Run()
//
//		// Cleanup
//		testPool.Cleanup()
//		connPool.Close()
//		os.Exit(code)
//	}
//
//	func TestUserOperations(t *testing.T) {
//		ctx := context.Background()
//
//		// Acquire a test database
//		db, err := testPool.Acquire(ctx)
//		if err != nil {
//			t.Fatal(err)
//		}
//		defer db.Release(ctx) // Reset and return to pool
//
//		// Use the database
//		_, err = db.Pool().Exec(ctx, "INSERT INTO users (name) VALUES ($1)", "Alice")
//		if err != nil {
//			t.Fatal(err)
//		}
//	}
//
// # Performance Benefits
//
// testdbpool provides significant performance improvements over traditional approaches:
//
//   - Fast database creation through template cloning instead of schema migration
//   - Efficient cleanup using TRUNCATE instead of database recreation
//   - Connection pool reuse eliminates connection establishment overhead
//   - Optimal resource utilization in CI and resource-constrained environments
//   - Reduced load on PostgreSQL server through controlled database pooling
//
// # Resource Management
//
// The library uses numpool for bitmap-based resource tracking with PostgreSQL advisory locks.
// This ensures fair allocation of database resources across concurrent tests and prevents
// resource exhaustion. The MaxDatabases configuration controls the pool size, defaulting
// to min(GOMAXPROCS, 64) for optimal performance.
//
// # Schema Versioning and Cleanup
//
// For evolving database schemas, testdbpool supports cleanup operations to manage pools
// across different schema versions:
//
//	// List existing pools for cleanup
//	pools, err := testdbpool.ListPools(ctx, connPool, "myapp-test-")
//
//	// Remove outdated pools
//	err := testdbpool.CleanupPool(ctx, connPool, "myapp-test-old-hash")
//
// Users can implement automatic schema versioning by including schema hashes in pool IDs,
// ensuring that schema changes trigger new pool creation while old pools are cleaned up
// through dedicated cleanup scripts.
//
// # Requirements
//
//   - PostgreSQL 14 or higher (for reliable template database support)
//   - Go 1.22 or higher
package testdbpool
