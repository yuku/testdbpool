# testdbpool

A Go library for managing a pool of test databases in PostgreSQL. Built on top of [numpool](https://github.com/yuku/numpool), testdbpool provides efficient test database management with automatic cleanup and concurrent access support.

## Features

- **Efficient Database Pooling**: Reuse test databases across test runs
- **Template-based Setup**: Create test databases from a template for fast initialization
- **Automatic Cleanup**: Databases are automatically reset between uses
- **Concurrent Support**: Safe concurrent access with fair queuing
- **Process Cleanup**: Automatic cleanup when processes terminate

## Installation

```bash
go get github.com/yuku/testdbpool
```

## Usage

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
    defer pool.Close()
    
    // Configure the test database pool
    config := &testdbpool.Config{
        PoolID:       "myapp-test",
        DBPool:       pool,
        MaxDatabases: 5,
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
    defer testPool.Close()
    
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

## Configuration

### Config Fields

- `PoolID`: Unique identifier for the test database pool
- `DBPool`: PostgreSQL connection pool (`*pgxpool.Pool`)
- `MaxDatabases`: Maximum number of test databases (1-64)
- `SetupTemplate`: Function to initialize the template database schema
- `ResetDatabase`: Function to reset a database to clean state

### How It Works

1. **Template Creation**: On first use, testdbpool creates a template database and runs your `SetupTemplate` function
2. **Database Creation**: Test databases are created by cloning the template database (fast operation in PostgreSQL)
3. **Acquisition**: When you call `Acquire()`, you get an available test database or wait if all are in use
4. **Reset**: When you call `Release()` or `Close()`, the database is reset using your `ResetDatabase` function
5. **Reuse**: The cleaned database is returned to the pool for reuse

## Best Practices

1. **Pool Per Test Suite**: Create one pool per test suite that shares the same schema
2. **Efficient Reset**: Make your `ResetDatabase` function fast by using TRUNCATE instead of DELETE
3. **Connection Limits**: Set `MaxDatabases` based on your PostgreSQL connection limits
4. **Cleanup**: Always defer `Close()` or `Release()` to ensure databases are returned to the pool

## Requirements

- PostgreSQL 14 or higher
- Go 1.22 or higher

## License

MIT License - see LICENSE file for details