# testdbpool

A Go library for managing a pool of test databases in PostgreSQL. Built on top of [numpool](https://github.com/yuku/numpool), testdbpool provides efficient test database management with automatic cleanup and concurrent access support.

## Features

- **Efficient Database Pooling**: Reuse test databases across test runs to significantly speed up integration tests
- **Template-based Setup**: Create test databases from a template for fast initialization using PostgreSQL's template database feature
- **Automatic Cleanup**: Databases are automatically reset between uses via customizable reset function
- **Concurrent Support**: Safe concurrent access with fair queuing through numpool's bitmap-based resource tracking
- **Process Cleanup**: Automatic cleanup when processes terminate - no manual cleanup required
- **Smart Defaults**: Automatically scales pool size based on available CPU cores
- **Multi-package Support**: Multiple test packages can share the same pool using a common PoolID

## Installation

```bash
go get github.com/yuku/testdbpool
```

## Usage

### Basic Example

```go
package mytest

import (
    "context"
    "testing"
    
    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/yuku/testdbpool"
)

func TestWithDatabase(t *testing.T) {
    ctx := context.Background()
    
    // Create a connection pool
    pool, err := pgxpool.New(ctx, "postgres://user:pass@localhost/postgres")
    if err != nil {
        t.Fatal(err)
    }
    
    // Configure the test database pool
    config := &testdbpool.Config{
        PoolID:       "myapp-test",
        DBPool:       pool,
        // MaxDatabases: 0, // Defaults to min(GOMAXPROCS, 64)
        SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
            // Set up your database schema
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
        ResetDatabase: func(ctx context.Context, conn *pgx.Conn) error {
            // Reset the database to a clean state
            _, err := conn.Exec(ctx, `
                TRUNCATE users, posts RESTART IDENTITY CASCADE;
            `)
            return err
        },
    }
    
    // Create the pool
    testPool, err := testdbpool.New(ctx, config)
    if err != nil {
        t.Fatal(err)
    }
    
    // Acquire a test database
    db, err := testPool.Acquire(ctx)
    if err != nil {
        t.Fatal(err)
    }
    defer db.Close() // Database is reset and returned to pool
    
    // Use the database connection
    conn := db.Conn()
    _, err = conn.Exec(ctx, "INSERT INTO users (name) VALUES ($1)", "Alice")
    if err != nil {
        t.Fatal(err)
    }
}
```

### Sharing Pools Across Packages

Different test packages can share the same pool by using the same PoolID:

```go
// package1/user_test.go
func TestUserOperations(t *testing.T) {
    config := &testdbpool.Config{
        PoolID:        "myapp-shared-pool", // Same ID across packages
        DBPool:        getDBPool(),
        SetupTemplate: setupSchema,
        ResetDatabase: resetData,
    }
    
    pool, _ := testdbpool.New(ctx, config)
    db, _ := pool.Acquire(ctx)
    defer db.Close()
    
    // Run user tests...
}

// package2/post_test.go  
func TestPostOperations(t *testing.T) {
    config := &testdbpool.Config{
        PoolID:        "myapp-shared-pool", // Same ID - shares resources
        DBPool:        getDBPool(),
        SetupTemplate: setupSchema,
        ResetDatabase: resetData,
    }
    
    pool, _ := testdbpool.New(ctx, config)
    db, _ := pool.Acquire(ctx)
    defer db.Close()
    
    // Run post tests...
}
```

## API Reference

### Configuration

```go
type Config struct {
    PoolID        string                                          // Required: Unique identifier for the pool
    DBPool        *pgxpool.Pool                                   // Required: PostgreSQL connection pool
    MaxDatabases  int                                             // Optional: Max databases (default: min(GOMAXPROCS, numpool.MaxResourcesLimit))
    SetupTemplate func(ctx context.Context, conn *pgx.Conn) error // Required: Initialize template database
    ResetDatabase func(ctx context.Context, conn *pgx.Conn) error // Required: Reset database between uses
}
```

### Pool Operations

```go
// Create or connect to a test database pool
pool, err := testdbpool.New(ctx, config)

// Acquire a test database from the pool
db, err := pool.Acquire(ctx)

// Get the database name (for debugging/logging)
name := db.DatabaseName()

// Get the database connection
conn := db.Conn()

// Return the database to the pool (resets it first)
err := db.Close()
```

### How It Works

1. **Template Creation**: On first use, testdbpool creates a template database and runs your `SetupTemplate` function
2. **Database Creation**: Test databases are created by cloning the template database (fast operation in PostgreSQL)
3. **Resource Management**: Uses [numpool](https://github.com/yuku/numpool) for efficient resource tracking with PostgreSQL advisory locks
4. **Acquisition**: When you call `Acquire()`, you get an available test database or wait if all are in use
5. **Reset**: When you call `Close()`, the database is reset using your `ResetDatabase` function
6. **Reuse**: The cleaned database is returned to the pool for reuse by other tests

## Best Practices

1. **Pool Per Schema**: Create one pool per distinct database schema to maximize reuse
2. **Efficient Reset**: Use `TRUNCATE ... CASCADE` instead of `DELETE` for faster cleanup
3. **Connection Management**: Let `MaxDatabases` default to CPU cores for optimal performance
4. **Always Close**: Always defer `db.Close()` to ensure databases are returned to the pool
5. **Shared Pools**: Use the same `PoolID` across test packages that share the same schema
6. **Template Functions**: Keep `SetupTemplate` idempotent - it may be called multiple times

## Performance Benefits

Using testdbpool can significantly speed up your integration tests:

- **Database Creation**: ~50ms with template cloning vs ~500ms+ for schema migration
- **Cleanup**: ~5ms with TRUNCATE vs ~50ms+ dropping and recreating
- **Overall**: 10-100x faster test execution for database-heavy test suites

## Requirements

- PostgreSQL 14 or higher (for reliable template database support)
- Go 1.22 or higher

## License

MIT License - see LICENSE file for details