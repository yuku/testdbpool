# testdbpool

[![Go Reference](https://pkg.go.dev/badge/github.com/yuku/testdbpool.svg)](https://pkg.go.dev/github.com/yuku/testdbpool)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/yuku/testdbpool)

A Go library for managing a pool of test databases in PostgreSQL. Built on top of [numpool](https://github.com/yuku/numpool), testdbpool provides efficient test database management with automatic cleanup and concurrent access support.

## Features

- **Efficient Database Pooling**: Reuse test databases across test runs to significantly speed up integration tests
- **Template-based Setup**: Create test databases from a template for fast initialization using PostgreSQL's template database feature
- **Complete Data Isolation**: Databases are completely dropped and recreated between uses
- **Concurrent Support**: Safe concurrent access with fair queuing through numpool's bitmap-based resource tracking
- **Process Cleanup**: Automatic cleanup when processes terminate - no manual cleanup required
- **Smart Defaults**: Automatically scales pool size based on available CPU cores
- **Multi-instance Support**: Multiple test pools can share the same underlying resources using a common ID
- **Simple API**: Explicit template creation provides clear control and eliminates complex synchronization

## Installation

```bash
go get github.com/yuku/testdbpool
```

## Usage

### Realistic Example with TestMain

```go
package mytest

import (
    "context"
    "os"
    "testing"
    
    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/yuku/testdbpool"
)

var (
    testPool *testdbpool.Pool
    connPool *pgxpool.Pool
)

func TestMain(m *testing.M) {
    ctx := context.Background()

    // Create a connection pool to the PostgreSQL server
    var err error
    connPool, err = pgxpool.New(ctx, "postgres://user:pass@localhost/postgres")
    if err != nil {
        panic(err)
    }

    // Create the template database
    templateName := "testdbpooltmpl_myapp-test"

    // Clean up any existing template from previous runs
    _, _ = connPool.Exec(ctx, fmt.Sprintf("ALTER DATABASE %s IS_TEMPLATE false",
        pgx.Identifier{templateName}.Sanitize()))
    _, _ = connPool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s",
        pgx.Identifier{templateName}.Sanitize()))

    // Create new template database
    _, err = connPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s IS_TEMPLATE true",
        pgx.Identifier{templateName}.Sanitize()))
    if err != nil {
        panic(err)
    }

    // Connect to template database and set up schema
    cfg := connPool.Config().ConnConfig.Copy()
    cfg.Database = templateName
    templateConn, err := pgx.ConnectConfig(ctx, cfg)
    if err != nil {
        panic(err)
    }

    // Set up your database schema in the template
    _, err = templateConn.Exec(ctx, `
        CREATE TABLE users (
            id SERIAL PRIMARY KEY,
            name TEXT NOT NULL
        );
        CREATE TABLE posts (
            id SERIAL PRIMARY KEY,
            user_id INT REFERENCES users(id),
            title TEXT NOT NULL
        );
    `)
    templateConn.Close(ctx)
    if err != nil {
        panic(err)
    }

    // Configure the test database pool
    config := &testdbpool.Config{
        ID:           "myapp-test",
        Pool:         connPool,
        MaxDatabases: 0, // Defaults to min(GOMAXPROCS, 64)
    }

    // Create the test database pool
    testPool, err = testdbpool.New(ctx, config)
    if err != nil {
        panic(err)
    }

    // Run tests
    code := m.Run()

    // Cleanup
    testPool.Cleanup()
    connPool.Close()

    os.Exit(code)
}

func TestUserOperations(t *testing.T) {
    ctx := context.Background()
    
    // Acquire a test database from the shared pool
    db, err := testPool.Acquire(ctx)
    if err != nil {
        t.Fatal(err)
    }
    defer db.Release(ctx) // Database is reset and returned to pool
    
    // Test user creation
    _, err = db.Pool().Exec(ctx, "INSERT INTO users (name) VALUES ($1)", "Alice")
    if err != nil {
        t.Fatal(err)
    }
    
    var count int
    err = db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
    if err != nil {
        t.Fatal(err)
    }
    
    if count != 1 {
        t.Errorf("Expected 1 user, got %d", count)
    }
    
    t.Logf("Using test database: %s", db.Name())
}

func TestPostOperations(t *testing.T) {
    ctx := context.Background()
    
    // Acquire a test database from the shared pool
    db, err := testPool.Acquire(ctx)
    if err != nil {
        t.Fatal(err)
    }
    defer db.Release(ctx) // Database is reset and returned to pool
    
    // Insert a user first
    var userID int
    err = db.Pool().QueryRow(ctx, 
        "INSERT INTO users (name) VALUES ($1) RETURNING id", "Bob").Scan(&userID)
    if err != nil {
        t.Fatal(err)
    }
    
    // Insert a post
    _, err = db.Pool().Exec(ctx, 
        "INSERT INTO posts (user_id, title) VALUES ($1, $2)", userID, "Hello World")
    if err != nil {
        t.Fatal(err)
    }
    
    var count int
    err = db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM posts").Scan(&count)
    if err != nil {
        t.Fatal(err)
    }
    
    if count != 1 {
        t.Errorf("Expected 1 post, got %d", count)
    }
    
    t.Logf("Using test database: %s", db.Name())
}

func TestConcurrentOperations(t *testing.T) {
    ctx := context.Background()
    
    // Multiple subtests can run concurrently, each getting their own database
    t.Run("user_creation", func(t *testing.T) {
        t.Parallel()
        
        db, err := testPool.Acquire(ctx)
        if err != nil {
            t.Fatal(err)
        }
        defer db.Release(ctx)
        
        _, err = db.Pool().Exec(ctx, "INSERT INTO users (name) VALUES ($1)", "Charlie")
        if err != nil {
            t.Fatal(err)
        }
    })
    
    t.Run("post_creation", func(t *testing.T) {
        t.Parallel()
        
        db, err := testPool.Acquire(ctx)
        if err != nil {
            t.Fatal(err)
        }
        defer db.Release(ctx)
        
        // Insert user and post
        var userID int
        err = db.Pool().QueryRow(ctx, 
            "INSERT INTO users (name) VALUES ($1) RETURNING id", "David").Scan(&userID)
        if err != nil {
            t.Fatal(err)
        }
        
        _, err = db.Pool().Exec(ctx, 
            "INSERT INTO posts (user_id, title) VALUES ($1, $2)", userID, "Test Post")
        if err != nil {
            t.Fatal(err)
        }
    })
}
```

### Sharing Pools Across Test Packages

Different test packages can share the same pool by using the same ID. The template database must be created once before any tests run:

```go
// package1/user_test.go
func TestUserOperations(t *testing.T) {
    // Template database "testdbpooltmpl_myapp-shared-pool" must already exist
    config := &testdbpool.Config{
        ID:   "myapp-shared-pool", // Same ID across packages
        Pool: getDBPool(),
    }

    pool, _ := testdbpool.New(ctx, config)
    defer pool.Close(ctx) // Close this instance, but don't cleanup shared resources

    db, _ := pool.Acquire(ctx)
    defer db.Release(ctx)

    // Run user tests...
}

// package2/post_test.go
func TestPostOperations(t *testing.T) {
    // Uses the same template database "testdbpooltmpl_myapp-shared-pool"
    config := &testdbpool.Config{
        ID:   "myapp-shared-pool", // Same ID - shares resources
        Pool: getDBPool(),
    }

    pool, _ := testdbpool.New(ctx, config)
    defer pool.Close(ctx) // Close this instance, but don't cleanup shared resources

    db, _ := pool.Acquire(ctx)
    defer db.Release(ctx)

    // Run post tests...
}
```

**Note**: When sharing pools across packages, create the template database in a shared setup script or in the first package's `TestMain`.

### Schema Version Management and Cleanup

When working with evolving database schemas, you may want to ensure that test pools use the current schema version and clean up outdated pools. Here's how to implement this pattern:

#### Pool ID with Git Revision

```go
package mytest

import (
    "fmt"
    "log"

    "github.com/yuku/testdbpool/gitutil"
)

func TestMain(m *testing.M) {
    ctx := context.Background()
    connPool, _ := pgxpool.New(ctx, "postgres://localhost/postgres")

    // Create pool ID with schema version
    schemaFiles := []string{"db/schema.sql", "db/migrations.sql"} // Your schema files
    schemaVersion := gitutil.GetSchemaVersion(schemaFiles)
    poolID := fmt.Sprintf("myapp-test-%s", schemaVersion)

    // Create template database with version-specific name
    templateName := fmt.Sprintf("testdbpooltmpl_%s", poolID)
    // ... create template database (see main usage example) ...

    config := &testdbpool.Config{
        ID:   poolID, // e.g., "myapp-test-a1b2c3d4" or "myapp-test-f4e9a2b1" (random)
        Pool: connPool,
    }

    testPool, _ := testdbpool.New(ctx, config)

    code := m.Run()

    testPool.Cleanup()
    connPool.Close()
    os.Exit(code)
}
```

#### Cleanup Script for Old Pools

Create a standalone cleanup script to remove pools from previous schema versions:

```go
// scripts/cleanup-old-pools.go
package main

import (
    "context"
    "fmt"
    "log"
    
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/yuku/testdbpool"
)

func main() {
    ctx := context.Background()
    
    // Connect to PostgreSQL
    connPool, err := pgxpool.New(ctx, "postgres://localhost/postgres")
    if err != nil {
        log.Fatal(err)
    }
    defer connPool.Close()
    
    // List all pools with our prefix
    pools, err := testdbpool.ListPools(ctx, connPool, "myapp-test-")
    if err != nil {
        log.Fatal(err)
    }
    
    log.Printf("Found %d pools with prefix 'myapp-test-'", len(pools))
    log.Printf("Current pool ID: %s", currentPoolID)
    
    // Clean up old pools
    for _, poolID := range pools {
        if err := testdbpool.CleanupPool(ctx, connPool, poolID); err != nil {
            log.Printf("Warning: Failed to cleanup %s: %v", poolID, err)
        }
    }
    
    log.Printf("Cleanup complete. Current pool '%s' preserved.", currentPoolID)
}

// Import and use the same gitutil.GetSchemaVersion function
// This ensures consistency between tests and cleanup script
```

This approach provides:
- **Automatic schema versioning**: Pools are automatically isolated by schema version based on git commits
- **Development safety**: Unstaged changes force new database creation to avoid conflicts
- **Efficient resource usage**: Old pools don't consume database resources during development
- **Development workflow**: Developers can safely iterate on schema changes without manual pool management

### Integration Test Examples

See the [integration tests](integration_test.go) for comprehensive examples of:
- Sequential and concurrent database usage
- Multiple pool instances
- Resource sharing and cleanup
- Error handling patterns

## API Reference

### Configuration

```go
type Config struct {
    ID            string         // Required: Unique identifier for the pool
    Pool          *pgxpool.Pool  // Required: PostgreSQL connection pool to postgres database
    MaxDatabases  int            // Optional: Max databases (default: min(GOMAXPROCS, 64))
    DatabaseOwner string         // Optional: Database owner (default: connection user)
}
```

**Template Database Requirement**: Before calling `testdbpool.New()`, you must create a template database named `testdbpooltmpl_{ID}` where `{ID}` is your pool ID. The template database must have `IS_TEMPLATE true` set.

### Pool Operations

```go
// Create or connect to a test database pool
pool, err := testdbpool.New(ctx, config)

// Acquire a test database from the pool
db, err := pool.Acquire(ctx)

// Get the database name (for debugging/logging)
name := db.Name()

// Get the database connection pool for this test database
dbPool := db.Pool()

// Execute queries on the test database
_, err = db.Pool().Exec(ctx, "INSERT INTO users (name) VALUES ($1)", "Alice")

// Return the database to the pool (resets it first)
err := db.Release(ctx)

// Close this pool instance (doesn't affect shared resources)
err := pool.Close(ctx)

// Cleanup template and test databases (only call from one instance)
pool.Cleanup()
```

### Pool Management Functions

```go
// List pools matching a prefix (useful for cleanup scripts)
pools, err := testdbpool.ListPools(ctx, connPool, "myapp-test-")

// Clean up a specific pool and all its resources
err := testdbpool.CleanupPool(ctx, connPool, "myapp-test-old-hash")
```

### Git Utilities

The `gitutil` subpackage provides convenient functions for Git-based schema versioning:

```go
import "github.com/yuku/testdbpool/gitutil"

// Get git commit hash of the latest change to schema files
revision, err := gitutil.GetFilesRevision([]string{"db/schema.sql", "db/migrations.sql"})

// Check if schema files have unstaged changes
hasChanges, err := gitutil.HasUnstagedChanges([]string{"db/schema.sql"})

// Get schema version (git revision or random if unstaged changes exist)
version := gitutil.GetSchemaVersion([]string{"db/schema.sql", "db/migrations.sql"})
```

### TestDB Interface

Each acquired `TestDB` provides:

- **Connection Pool**: Full `*pgxpool.Pool` for the test database with multiple concurrent connections
- **Database Name**: Access to the unique database name for logging/debugging
- **Connection Reuse**: Connection pools are kept alive when released and reused when the same resource is acquired again, reducing connection establishment overhead

## Migration Guide (v0.1.x to v0.2.x)

Version 0.2.0 simplifies the API by removing the `SetupTemplate` callback. Template databases must now be created explicitly before calling `testdbpool.New()`.

### What Changed

**Before (v0.1.x):**
```go
config := &testdbpool.Config{
    ID:   "myapp-test",
    Pool: connPool,
    SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
        _, err := conn.Exec(ctx, `CREATE TABLE users (id SERIAL PRIMARY KEY, name TEXT)`)
        return err
    },
}
pool, _ := testdbpool.New(ctx, config)
```

**After (v0.2.x):**
```go
// 1. Create template database
templateName := "testdbpooltmpl_myapp-test"
_, _ = connPool.Exec(ctx, fmt.Sprintf("ALTER DATABASE %s IS_TEMPLATE false",
    pgx.Identifier{templateName}.Sanitize()))
_, _ = connPool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s",
    pgx.Identifier{templateName}.Sanitize()))
_, err := connPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s IS_TEMPLATE true",
    pgx.Identifier{templateName}.Sanitize()))

// 2. Connect to template and set up schema
cfg := connPool.Config().ConnConfig.Copy()
cfg.Database = templateName
templateConn, _ := pgx.ConnectConfig(ctx, cfg)
_, err = templateConn.Exec(ctx, `CREATE TABLE users (id SERIAL PRIMARY KEY, name TEXT)`)
templateConn.Close(ctx)

// 3. Create pool (no SetupTemplate)
config := &testdbpool.Config{
    ID:   "myapp-test",
    Pool: connPool,
}
pool, _ := testdbpool.New(ctx, config)
```

### Why This Change?

1. **Eliminates Deadlocks**: The old `SetupTemplate` callback used advisory locks for synchronization, which caused deadlocks when running `go test ./...` across multiple packages
2. **Simpler Code**: Removes ~100 lines of complex synchronization logic
3. **Clearer Separation**: Template creation is now explicitly under your control
4. **Better for CI**: More predictable behavior in parallel test environments

### Migration Steps

1. **Extract SetupTemplate logic**: Move the code from your `SetupTemplate` function to a separate template creation function
2. **Call before New()**: Create the template database before calling `testdbpool.New()`
3. **Remove SetupTemplate**: Remove the `SetupTemplate` field from your `Config`
4. **Update tests**: Ensure all tests that create pools also create templates first

See the usage examples above for complete code samples.

## How It Works

```mermaid
sequenceDiagram
    participant T as Test
    participant P as testdbpool.Pool
    participant N as numpool
    participant PG as PostgreSQL
    participant TD as TestDB

    Note over T,PG: Setup (before New())
    T->>PG: CREATE DATABASE testdbpooltmpl_myapp IS_TEMPLATE true
    T->>PG: Connect and set up schema

    Note over P,PG: First time setup
    T->>P: New(config)
    P->>PG: Verify template exists
    P->>N: Initialize resource pool

    Note over T,TD: Test execution
    T->>P: Acquire(ctx)
    P->>N: Acquire resource index
    alt Resource available
        N-->>P: Return index (e.g., 0)
        P->>PG: Get/create testdb_myapp_0
        P->>TD: Create connection pool
        P-->>T: Return TestDB
    else Resource busy
        N-->>P: Block until available
        P->>PG: Get existing testdb_myapp_X
        P->>TD: Reuse existing connection pool
        P-->>T: Return TestDB
    end

    T->>TD: Use database (queries, etc.)
    T->>TD: Release(ctx)
    TD->>PG: DROP DATABASE (complete cleanup)
    TD->>N: Release resource index
    Note over TD: Database completely removed for isolation

    Note over P,PG: Cleanup (TestMain end)
    T->>P: Cleanup()
    P->>PG: Drop all test databases
    P->>PG: Drop template database
```

### Process Flow

1. **Template Creation**: You create a template database with `IS_TEMPLATE true` and set up your schema before calling `New()`
2. **Pool Initialization**: `New()` verifies the template exists and initializes the resource pool
3. **Database Creation**: Test databases are created by cloning the template (fast `CREATE DATABASE ... TEMPLATE` operation)
4. **Resource Management**: Uses [numpool](https://github.com/yuku/numpool) with PostgreSQL advisory locks for efficient resource tracking
5. **Acquisition**: `Acquire()` returns an available test database or blocks until one becomes available
6. **Reset**: `Release()` drops the database completely for maximum isolation
7. **Recreation**: Subsequent `Acquire()` calls create fresh databases from the template

## Configuration Validation

The library validates configuration at startup:

- **ID**: Must be non-empty and result in valid PostgreSQL database names
- **Pool**: Must be a valid connection pool to a PostgreSQL server
- **MaxDatabases**: Must be between 1 and 64 (defaults to `min(GOMAXPROCS, 64)`)
- **DatabaseOwner**: Optional; must be a valid PostgreSQL identifier if specified
- **Template Database**: Must exist with name `testdbpooltmpl_{ID}` before calling `New()`

See [config_test.go](config_test.go) for comprehensive validation examples.

## Database Owner Configuration

The `DatabaseOwner` field allows you to specify a different owner for the template and test databases. This is useful for advanced permission scenarios where databases need to be owned by a specific role.

### Basic Usage

```go
// First create the template database with the desired owner
templateName := "testdbpooltmpl_myapp-test"
_, err := connPool.Exec(ctx, fmt.Sprintf(
    "CREATE DATABASE %s OWNER %s IS_TEMPLATE true",
    pgx.Identifier{templateName}.Sanitize(),
    pgx.Identifier{"app_user"}.Sanitize(),
))
// ... connect to template and set up schema ...

// Then configure the pool
config := &testdbpool.Config{
    ID:            "myapp-test",
    Pool:          connPool,
    DatabaseOwner: "app_user",  // Test databases will be owned by 'app_user'
}
```

### Permission Requirements

⚠️ **Important**: When specifying a `DatabaseOwner` different from the connection user, the connection user must have sufficient privileges:

1. **Be a member of the DatabaseOwner role:**
   ```sql
   GRANT app_user TO connection_user;
   ```
2. **Have superuser privileges:**
   ```sql
   ALTER USER connection_user SUPERUSER;
   ```
3. **Have CREATEDB privilege at minimum:**
   ```sql
   ALTER USER connection_user CREATEDB;
   ```

### Recommended Setup Patterns

#### Development/Testing (Simple)
```sql
-- Create a superuser for testing (simplest approach)
CREATE USER test_admin SUPERUSER LOGIN PASSWORD 'password';
```

```go
// Use superuser for connection
connPool, _ := pgxpool.New(ctx, "postgres://test_admin:password@localhost/postgres")

config := &testdbpool.Config{
    Pool:          connPool,
    DatabaseOwner: "app_user",  // Any valid user
    // ... other config
}
```

#### Production (Secure)
```sql
-- Create minimal privilege setup
CREATE USER db_manager CREATEDB LOGIN PASSWORD 'secure_password';
CREATE USER app_user LOGIN PASSWORD 'app_password';
GRANT app_user TO db_manager;
```

```go
// Use db_manager for connection
connPool, _ := pgxpool.New(ctx, "postgres://db_manager:secure_password@localhost/postgres")

config := &testdbpool.Config{
    Pool:          connPool,
    DatabaseOwner: "app_user",  // app_user will own databases
    // ... other config
}
```

#### Same User (Default Behavior)
```go
// Simplest case - connection user owns databases
config := &testdbpool.Config{
    Pool:          connPool,
    DatabaseOwner: "",  // Empty = use connection user
    // ... other config
}
```

### Common Errors

If you see permission errors like:

- `ERROR: must be member of role "target_user"`
- `ERROR: permission denied to create database`
- `ERROR: must have CREATEDB privilege`

Check that your connection user has the required privileges as outlined above.

## Best Practices

1. **Pool Per Schema**: Create one pool per distinct database schema to maximize reuse
2. **Connection Management**: Let `MaxDatabases` default to CPU cores for optimal performance
3. **Always Release**: Always defer `db.Release(ctx)` to ensure databases are returned to the pool
4. **Shared Pools**: Use the same `ID` across test packages that share the same schema
5. **Template Creation**: Create template databases once in TestMain or a setup script before running tests
6. **Cleanup Strategy**: Use `Cleanup()` in one place (e.g., TestMain) and `Close()` everywhere else
7. **Concurrent Testing**: Design tests to work reliably with the DROP DATABASE strategy's timing characteristics
8. **Multi-package Testing**: When running `go test ./...`, ensure template databases exist before tests start

## Performance Benefits

Using testdbpool provides several key performance and efficiency improvements:

- **Fast Database Creation**: Template-based cloning (`CREATE DATABASE ... TEMPLATE`) is significantly faster than running full schema migrations
- **Complete Isolation**: DROP DATABASE strategy ensures complete data isolation between test runs
- **Reliable Concurrency**: Excellent support for concurrent test execution without resource contention
- **Resource Efficiency**: Optimal resource utilization in CI environments through controlled database pooling
- **Concurrent Testing**: Multiple tests can run in parallel while sharing a limited pool of databases efficiently
- **Complex Schema Support**: Performs well with large, complex database schemas

## Requirements

- PostgreSQL 14 or higher (for reliable template database support)
- Go 1.22 or higher

## Dependencies

- [pgx/v5](https://github.com/jackc/pgx) - PostgreSQL driver and connection pooling
- [numpool](https://github.com/yuku/numpool) - Bitmap-based resource pool management
- [testify](https://github.com/stretchr/testify) - Testing utilities (dev dependency)

## Database Reset Strategy

**testdbpool uses the DROP DATABASE strategy exclusively** for database cleanup between test runs. This design decision was made after comprehensive benchmarking and analysis of different approaches:

### Strategy Comparison

We evaluated two primary strategies for resetting test databases:

| Metric | TRUNCATE Strategy | DROP DATABASE Strategy | Winner |
|--------|------------------|----------------------|---------|
| **Simple Operations** | ~25ms per cycle | ~121ms per cycle (4.8x slower) | TRUNCATE |
| **Data Operations** | ~30ms per operation | ~97ms per operation (3.2x slower) | TRUNCATE |
| **Large Schema (10+ tables)** | ~527ms per operation | ~214ms per operation (2.5x faster) | **DROP** |
| **Concurrency Support** | ❌ Resource contention issues | ✅ Excellent concurrent support | **DROP** |
| **Data Isolation** | ⚠️ Schema changes persist | ✅ Complete isolation | **DROP** |
| **Implementation Complexity** | High (dual strategies) | Low (single strategy) | **DROP** |

### Why DROP DATABASE Strategy?

1. **Reliability**: No resource contention issues when `MaxDatabases` < concurrent goroutines
2. **Complete Isolation**: Each test gets a completely fresh database
3. **Better Performance with Complex Schemas**: More efficient when dealing with many tables/indexes
4. **Simplified Maintenance**: Single strategy reduces codebase complexity
5. **Future-Proof**: Scales better as application schemas grow in complexity

### Performance Benchmarks

```bash
# Run benchmarks to see current performance characteristics
go test -bench=. -benchtime=2s

# Results on test hardware:
BenchmarkAcquireReleaseCycle-10         20    121ms per operation
BenchmarkWithDataOperations-10          15     97ms per operation  
BenchmarkConcurrentUsage-10             25     89ms per operation
BenchmarkLargeSchema-10                  5    214ms per operation
```

While DROP DATABASE is slower for simple schemas, the reliability and concurrent support benefits outweigh the performance cost in most real-world testing scenarios.

## License

MIT License - see LICENSE file for details
