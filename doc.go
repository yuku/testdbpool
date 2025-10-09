// Package testdbpool provides efficient test database pooling for PostgreSQL integration tests.
//
// testdbpool manages a pool of test databases that can be shared across multiple test functions,
// significantly improving test performance by reusing template-based database creation instead of
// running schema migrations for each test. Built on top of numpool for resource management, it uses
// PostgreSQL's template database feature for fast database creation and the DROP DATABASE strategy
// for complete isolation between test runs.
//
// # Key Features
//
//   - Template-based database creation using PostgreSQL's CREATE DATABASE ... TEMPLATE
//   - DROP DATABASE strategy for complete isolation between test runs
//   - Concurrent test support with efficient resource allocation
//   - Automatic database cleanup between test uses
//   - Resource-efficient operation ideal for CI environments
//   - Cross-package pool sharing using common identifiers
//
// # Basic Usage
//
// For multi-package testing with `go test ./...`, create template databases in a setup script
// that runs before tests, then use the pre-created template in each package's TestMain:
//
// Step 1: Create a setup script (scripts/setup-test-template.go):
//
//	package main
//
//	func main() {
//		ctx := context.Background()
//		connPool, _ := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
//		defer connPool.Close()
//
//		templateName := "testdbpooltmpl_myapp-test"
//		// Clean up existing template
//		_, _ = connPool.Exec(ctx, fmt.Sprintf("ALTER DATABASE %s IS_TEMPLATE false", ...))
//		_, _ = connPool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", ...))
//
//		// Create template database
//		_, _ = connPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s IS_TEMPLATE true", ...))
//
//		// Connect to template and set up schema
//		cfg := connPool.Config().ConnConfig.Copy()
//		cfg.Database = templateName
//		templateConn, _ := pgx.ConnectConfig(ctx, cfg)
//		defer templateConn.Close(ctx)
//		_, _ = templateConn.Exec(ctx, `CREATE TABLE users (id SERIAL PRIMARY KEY, name TEXT)`)
//	}
//
// Step 2: Run setup before tests (Makefile):
//
//	test:
//		go run scripts/setup-test-template.go
//		go test -v ./...
//
// Step 3: Use the pre-created template in each package's TestMain:
//
//	func TestMain(m *testing.M) {
//		ctx := context.Background()
//		connPool, _ := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
//
//		// Use pre-created template (no template creation here)
//		config := &testdbpool.Config{
//			ID:   "myapp-test",
//			Pool: connPool,
//		}
//		testPool, _ := testdbpool.New(ctx, config)
//
//		code := m.Run()
//
//		// Close this instance (don't cleanup - other packages may be using it)
//		testPool.Close(ctx)
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
//		defer db.Release(ctx) // Drop database and return resource to pool
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
//   - Complete isolation using DROP DATABASE strategy for reliable test separation
//   - Excellent concurrent test support with minimal resource contention
//   - Optimal resource utilization in CI and resource-constrained environments
//   - Superior performance with complex schemas (10+ tables)
//   - Simplified maintenance with single cleanup strategy
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
