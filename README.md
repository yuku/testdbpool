# testdbpool - PostgreSQL Test Database Pool Library

A Go library designed to accelerate PostgreSQL-based testing by managing database pools using template databases for efficient database operations during test execution.

## Features

- **Fast Database Replication**: Uses PostgreSQL template databases for quick cloning
- **Pool Management**: Manages up to N database instances
- **Multi-Process Support**: Share pools across multiple test packages via PostgreSQL state management
- **Automatic Cleanup**: Integrates with `testing.T` for automatic resource management
- **Flexible Reset Strategies**: Multiple ways to reset databases between tests

## Installation

```bash
go get github.com/yuku/testdbpool
```

## Quick Start

```go
package myapp_test

import (
    "context"
    "database/sql"
    "log"
    "os"
    "testing"
    
    _ "github.com/lib/pq"
    "github.com/yuku/testdbpool"
)

var pool *testdbpool.Pool

func TestMain(m *testing.M) {
    // Connect to PostgreSQL
    rootDB, err := sql.Open("postgres", "postgres://user:pass@localhost/postgres")
    if err != nil {
        log.Fatal(err)
    }
    defer rootDB.Close()
    
    // Create pool
    pool, err = testdbpool.New(testdbpool.Configuration{
        RootConnection:  rootDB,
        PoolID:          "myapp_test",
        MaxPoolSize:     10,
        TemplateCreator: createSchema,
        ResetFunc:       testdbpool.ResetByTruncate([]string{"users", "posts"}, seedData),
    })
    if err != nil {
        log.Fatal(err)
    }
    
    // Run tests
    code := m.Run()
    
    // Optional: Clean up pool
    // testdbpool.Cleanup(rootDB, "myapp_test")
    
    os.Exit(code)
}

func createSchema(ctx context.Context, db *sql.DB) error {
    _, err := db.ExecContext(ctx, `
        CREATE TABLE users (
            id SERIAL PRIMARY KEY,
            name VARCHAR(100) NOT NULL,
            email VARCHAR(100) UNIQUE NOT NULL
        );
        
        CREATE TABLE posts (
            id SERIAL PRIMARY KEY,
            user_id INTEGER REFERENCES users(id),
            title VARCHAR(200) NOT NULL,
            content TEXT
        );
    `)
    return err
}

func seedData(ctx context.Context, db *sql.DB) error {
    _, err := db.ExecContext(ctx, `
        INSERT INTO users (name, email) VALUES 
            ('Alice', 'alice@example.com'),
            ('Bob', 'bob@example.com');
    `)
    return err
}

func TestUserCreation(t *testing.T) {
    // Acquire database (automatically cleaned up)
    db, err := pool.Acquire(t)
    if err != nil {
        t.Fatal(err)
    }
    
    // Your test code here
    _, err = db.Exec("INSERT INTO users (name, email) VALUES ($1, $2)",
        "Charlie", "charlie@example.com")
    if err != nil {
        t.Fatal(err)
    }
    
    // Database will be automatically reset when test completes
}
```

## Configuration

### Required Fields

- `RootConnection`: Database connection for managing pool state
- `PoolID`: Unique identifier for the pool (alphanumeric + underscore, max 50 chars)
- `TemplateCreator`: Function to create schema and seed data in template database
- `ResetFunc`: Function to reset database between tests

### Optional Fields

- `StateDatabase`: Database name for state management (default: "postgres")
- `MaxPoolSize`: Maximum number of databases in pool (default: GOMAXPROCS * 2)
- `AcquireTimeout`: Timeout for acquiring database (default: 30s)

## Reset Strategies

### ResetByTruncate

Most common approach - truncates specified tables and re-seeds data:

```go
ResetFunc: testdbpool.ResetByTruncate(
    []string{"posts", "users"}, // Tables to truncate (in order)
    seedData,                    // Optional: Function to restore seed data
)
```

### ResetBySQL

Execute custom SQL for reset:

```go
ResetFunc: testdbpool.ResetBySQL(`
    DELETE FROM posts WHERE created_at < NOW() - INTERVAL '1 day';
    UPDATE users SET last_login = NULL;
`)
```

### ResetByRecreation

Drops all tables and recreates schema (slower but thorough):

```go
ResetFunc: testdbpool.ResetByRecreation(createSchema)
```

## Environment Variables

The library uses these environment variables for database connections:

- `PGHOST`: PostgreSQL host (default: "localhost")
- `PGPORT`: PostgreSQL port (default: "5432")
- `PGUSER`: PostgreSQL user (default: "postgres")
- `PGPASSWORD`: PostgreSQL password (default: "postgres")
- `PGSSLMODE`: SSL mode (default: "disable")

## Performance Tips

1. **Use ResetByTruncate when possible** - It's faster than recreation
2. **Order tables correctly** - List child tables before parent tables to avoid FK issues
3. **Minimize seed data** - Only include essential test data
4. **Set appropriate pool size** - Too large wastes resources, too small causes contention

## Cleanup

Pools persist across test runs for performance. To manually clean up:

```go
err := testdbpool.Cleanup(rootDB, "myapp_test")
```

This removes:
- All pool databases
- Template database
- Pool state record

## How It Works

1. **First Run**: Creates template database with your schema
2. **Acquire**: Clones template database or reuses existing one
3. **Test Runs**: Your test uses the isolated database
4. **Release**: Automatically resets database and returns to pool
5. **Subsequent Tests**: Reuse reset databases for speed

## Thread Safety

The library is designed for concurrent use:
- Multiple tests can acquire databases simultaneously
- Pool state is managed via PostgreSQL transactions
- Safe for use with `go test -parallel`

## Limitations

- PostgreSQL only (uses PostgreSQL-specific features)
- Requires PostgreSQL 9.5+
- Template database name limited by PostgreSQL's 63-character limit
- No automatic cleanup (databases persist for performance)

## License

MIT License - see LICENSE file for details