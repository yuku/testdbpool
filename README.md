# testdbpool

A Go library for managing a pool of test databases in PostgreSQL. Built on top of [numpool](https://github.com/yuku/numpool), testdbpool provides efficient test database management with automatic cleanup and concurrent access support.

## Features

- **Efficient Database Pooling**: Reuse test databases across test runs to significantly speed up integration tests
- **Template-based Setup**: Create test databases from a template for fast initialization using PostgreSQL's template database feature
- **Automatic Cleanup**: Databases are automatically reset between uses via customizable reset function
- **Concurrent Support**: Safe concurrent access with fair queuing through numpool's bitmap-based resource tracking
- **Process Cleanup**: Automatic cleanup when processes terminate - no manual cleanup required
- **Smart Defaults**: Automatically scales pool size based on available CPU cores
- **Multi-instance Support**: Multiple test pools can share the same underlying resources using a common ID

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
    
    // Configure the test database pool
    config := &testdbpool.Config{
        ID:           "myapp-test",
        Pool:         connPool,
        MaxDatabases: 0, // Defaults to min(GOMAXPROCS, 64)
        SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
            // Set up your database schema in the template
            _, err := conn.Exec(ctx, `
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
            return err
        },
        ResetDatabase: func(ctx context.Context, pool *pgxpool.Pool) error {
            // Reset the database to a clean state
            _, err := pool.Exec(ctx, `
                TRUNCATE users, posts RESTART IDENTITY CASCADE;
            `)
            return err
        },
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

Different test packages can share the same pool by using the same ID:

```go
// package1/user_test.go
func TestUserOperations(t *testing.T) {
    config := &testdbpool.Config{
        ID:            "myapp-shared-pool", // Same ID across packages
        Pool:          getDBPool(),
        SetupTemplate: setupSchema,
        ResetDatabase: resetData,
    }
    
    pool, _ := testdbpool.New(ctx, config)
    defer pool.Close(ctx) // Close this instance, but don't cleanup shared resources
    
    db, _ := pool.Acquire(ctx)
    defer db.Release(ctx)
    
    // Run user tests...
}

// package2/post_test.go  
func TestPostOperations(t *testing.T) {
    config := &testdbpool.Config{
        ID:            "myapp-shared-pool", // Same ID - shares resources
        Pool:          getDBPool(),
        SetupTemplate: setupSchema,
        ResetDatabase: resetData,
    }
    
    pool, _ := testdbpool.New(ctx, config)
    defer pool.Close(ctx) // Close this instance, but don't cleanup shared resources
    
    db, _ := pool.Acquire(ctx)
    defer db.Release(ctx)
    
    // Run post tests...
}
```

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
    ID            string                                               // Required: Unique identifier for the pool
    Pool          *pgxpool.Pool                                        // Required: PostgreSQL connection pool to postgres database
    MaxDatabases  int                                                  // Optional: Max databases (default: min(GOMAXPROCS, 64))
    SetupTemplate func(ctx context.Context, conn *pgx.Conn) error      // Required: Initialize template database
    ResetDatabase func(ctx context.Context, pool *pgxpool.Pool) error  // Required: Reset database between uses
}
```

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

### TestDB Interface

Each acquired `TestDB` provides:

- **Connection Pool**: Full `*pgxpool.Pool` for the test database with multiple concurrent connections
- **Database Name**: Access to the unique database name for logging/debugging
- **Connection Reuse**: Connection pools are kept alive when released and reused when the same resource is acquired again, reducing connection establishment overhead

## How It Works

```mermaid
sequenceDiagram
    participant T as Test
    participant P as testdbpool.Pool
    participant N as numpool
    participant PG as PostgreSQL
    participant TD as TestDB

    Note over P,PG: First time setup
    T->>P: New(config)
    P->>PG: Create template database
    P->>PG: Run SetupTemplate function
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
    TD->>PG: Run ResetDatabase function
    TD->>N: Release resource index
    Note over TD: Connection pool kept alive for reuse

    Note over P,PG: Cleanup (TestMain end)
    T->>P: Cleanup()
    P->>PG: Drop all test databases
    P->>PG: Drop template database
```

### Process Flow

1. **Template Creation**: On first use, testdbpool creates a template database using your `SetupTemplate` function
2. **Database Creation**: Test databases are created by cloning the template (fast `CREATE DATABASE ... TEMPLATE` operation)
3. **Resource Management**: Uses [numpool](https://github.com/yuku/numpool) with PostgreSQL advisory locks for efficient resource tracking
4. **Acquisition**: `Acquire()` returns an available test database or blocks until one becomes available
5. **Reset**: `Release()` resets the database using your `ResetDatabase` function before returning it to the pool
6. **Reuse**: Cleaned databases and their connection pools are reused by subsequent `Acquire()` calls

## Configuration Validation

The library validates configuration at startup:

- **ID**: Must be non-empty and result in valid PostgreSQL database names
- **Pool**: Must be a valid connection pool to a PostgreSQL server
- **MaxDatabases**: Must be between 1 and 64 (defaults to `min(GOMAXPROCS, 64)`)
- **SetupTemplate**: Required function to initialize the template database
- **ResetDatabase**: Required function to clean databases between uses

See [config_test.go](config_test.go) for comprehensive validation examples.

## Best Practices

1. **Pool Per Schema**: Create one pool per distinct database schema to maximize reuse
2. **Efficient Reset**: Use `TRUNCATE ... CASCADE` instead of `DELETE` for faster cleanup
3. **Connection Management**: Let `MaxDatabases` default to CPU cores for optimal performance
4. **Always Release**: Always defer `db.Release(ctx)` to ensure databases are returned to the pool
5. **Shared Pools**: Use the same `ID` across test packages that share the same schema
6. **Template Functions**: Keep `SetupTemplate` idempotent - it may be called multiple times
7. **Cleanup Strategy**: Use `Cleanup()` in one place (e.g., TestMain) and `Close()` everywhere else

## Performance Benefits

Using testdbpool provides several key performance and efficiency improvements:

- **Fast Database Creation**: Template-based cloning (`CREATE DATABASE ... TEMPLATE`) is significantly faster than running full schema migrations
- **Efficient Cleanup**: `TRUNCATE` operations are much faster than dropping and recreating databases
- **Connection Pool Reuse**: Eliminates connection establishment overhead by reusing existing connection pools
- **Resource Efficiency**: Optimal resource utilization in CI environments and resource-constrained servers through controlled database pooling
- **Concurrent Testing**: Multiple tests can run in parallel while sharing a limited pool of databases efficiently
- **Reduced Load**: Lower overall load on PostgreSQL server compared to creating/dropping databases for each test

## Requirements

- PostgreSQL 14 or higher (for reliable template database support)
- Go 1.22 or higher

## Dependencies

- [pgx/v5](https://github.com/jackc/pgx) - PostgreSQL driver and connection pooling
- [numpool](https://github.com/yuku/numpool) - Bitmap-based resource pool management
- [testify](https://github.com/stretchr/testify) - Testing utilities (dev dependency)

## License

MIT License - see LICENSE file for details
