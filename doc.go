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
// The typical usage pattern involves creating a template database, setting up a shared pool
// in TestMain, and using it across multiple test functions:
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
//		// Create template database
//		templateName := "testdbpooltmpl_myapp-test"
//		_, _ = connPool.Exec(ctx, fmt.Sprintf("ALTER DATABASE %s IS_TEMPLATE false",
//			pgx.Identifier{templateName}.Sanitize()))
//		_, _ = connPool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s",
//			pgx.Identifier{templateName}.Sanitize()))
//		_, err = connPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s IS_TEMPLATE true",
//			pgx.Identifier{templateName}.Sanitize()))
//		if err != nil {
//			panic(err)
//		}
//
//		// Connect to template and set up schema
//		cfg := connPool.Config().ConnConfig.Copy()
//		cfg.Database = templateName
//		templateConn, err := pgx.ConnectConfig(ctx, cfg)
//		if err != nil {
//			panic(err)
//		}
//		_, err = templateConn.Exec(ctx, `CREATE TABLE users (id SERIAL PRIMARY KEY, name TEXT)`)
//		templateConn.Close(ctx)
//		if err != nil {
//			panic(err)
//		}
//
//		// Configure test database pool
//		config := &testdbpool.Config{
//			ID:   "myapp-test",
//			Pool: connPool,
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
