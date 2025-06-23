# pgxpool Example

This example demonstrates how to wrap testdbpool to provide `pgxpool.Pool` instances instead of `*sql.DB`, enabling the use of pgx-specific features in tests. It also shows how multiple packages can share the same test database pool when running in parallel with `go test ./...`.

## Overview

The wrapper provides several benefits:
- Access to pgx-specific features (batch queries, copy protocol, notifications)
- Better performance with native PostgreSQL protocol
- Connection pool statistics and monitoring
- Custom tracing and logging capabilities

## Usage

### Basic Usage

```go
// In TestMain
pool, err := testdbpool.New(testdbpool.Configuration{
    RootConnection:  rootDB,
    PoolID:         "myapp_test",
    TemplateCreator: createSchema,
    ResetFunc:      resetData,
})

// Create wrapper
poolWrapper := wrapper.NewPoolWrapper(pool)

// In tests
func TestSomething(t *testing.T) {
    pgxPool, err := poolWrapper.Acquire(t)
    if err != nil {
        t.Fatal(err)
    }
    
    // Use pgx-specific features
    batch := &pgx.Batch{}
    batch.Queue("INSERT INTO users (name) VALUES ($1)", "Alice")
    batch.Queue("INSERT INTO users (name) VALUES ($1)", "Bob")
    
    results := pgxPool.SendBatch(ctx, batch)
    defer results.Close()
}
```

### Custom Configuration

```go
pgxPool, err := poolWrapper.AcquireWithConfig(t, func(config *pgxpool.Config) {
    config.MaxConns = 5
    config.MinConns = 1
    config.ConnConfig.Tracer = myTracer
})
```

### Using Both Interfaces

```go
sqlDB, pgxPool, err := poolWrapper.AcquireBoth(t)
// Use sqlDB for compatibility with database/sql
// Use pgxPool for pgx-specific features
```

## Features Demonstrated

1. **Batch Queries**: Efficient execution of multiple queries in a single round trip
2. **Copy Protocol**: Bulk data insertion using PostgreSQL's COPY command
3. **Notifications**: LISTEN/NOTIFY support for real-time updates
4. **Connection Pool Management**: Fine-grained control over pool configuration
5. **Query Tracing**: Custom logging and monitoring of database operations

## Implementation Details

The wrapper works by:
1. Acquiring a test database from testdbpool
2. Extracting connection parameters from the active connection
3. Creating a new pgxpool with the same database
4. Managing cleanup automatically via `testing.T.Cleanup()`

## Multi-Package Testing

The example includes multiple packages to demonstrate pool sharing:

- **package1**: User operations and batch queries
- **package2**: Post operations, COPY protocol, and statistics
- **package3**: JSON/JSONB operations and complex queries
- **shared**: Common setup code for all packages

### Running Tests

Run all packages in parallel:
```bash
./test_parallel.sh
# or
go test -v ./...
```

This demonstrates that testdbpool correctly manages database isolation even when multiple packages run tests concurrently.

## Package Structure

```
examples/pgxpool/
├── wrapper/          # PoolWrapper implementation
│   └── wrapper.go
├── shared/           # Shared test setup
│   └── setup.go
├── package1/         # User-focused tests
│   └── users_test.go
├── package2/         # Post-focused tests
│   └── posts_test.go
├── package3/         # JSON operations tests
│   └── json_test.go
├── main_test.go      # Original single-package tests
├── test_parallel.sh  # Script to run all tests in parallel
└── README.md
```

## Limitations

- Password extraction from existing connections is not possible, so the wrapper relies on environment variables
- Each test gets its own pgxpool instance, which may have different performance characteristics than sharing a single pool
- Some connection parameters might need manual configuration depending on your PostgreSQL setup