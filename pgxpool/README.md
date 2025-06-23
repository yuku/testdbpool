# testdbpool/pgxpool

This package provides a wrapper around testdbpool to return `pgxpool.Pool` instances instead of `*sql.DB`, enabling the use of pgx-specific features in tests while maintaining test isolation.

## Installation

```bash
go get github.com/yuku/testdbpool
```

## Basic Usage

```go
import (
    "github.com/yuku/testdbpool"
    "github.com/yuku/testdbpool/pgxpool"
)

// Create testdbpool as usual
pool, err := testdbpool.New(config)
if err != nil {
    log.Fatal(err)
}

// Wrap it for pgx support
wrapper := pgxpool.New(pool)

func TestSomething(t *testing.T) {
    // Get pgxpool.Pool instead of *sql.DB
    pgxPool, err := wrapper.Acquire(t)
    if err != nil {
        t.Fatal(err)
    }
    
    // Use pgx-specific features
    batch := &pgx.Batch{}
    batch.Queue("INSERT INTO users (name) VALUES ($1)", "Alice")
    results := pgxPool.SendBatch(ctx, batch)
    defer results.Close()
}
```

## Advanced Configuration

### Custom Password Sources

```go
wrapper := pgxpool.NewWithConfig(pool, pgxpool.Config{
    // Use specific environment variable
    PasswordSource: pgxpool.EnvPasswordSource("MY_DB_PASSWORD"),
    
    // Or use static password
    PasswordSource: pgxpool.StaticPasswordSource("mypassword"),
    
    // Or custom logic
    PasswordSource: func() (string, error) {
        // Your custom password retrieval logic
        return getPasswordFromVault(), nil
    },
})
```

### Custom Host Configuration

```go
wrapper := pgxpool.NewWithConfig(pool, pgxpool.Config{
    // Use specific environment variables
    HostSource: pgxpool.EnvHostSource("MY_DB_HOST", "MY_DB_PORT"),
    
    // Or custom logic
    HostSource: func(db *sql.DB) (host, port string, err error) {
        // Custom host resolution logic
        return serviceDiscovery.GetHost(), "5432", nil
    },
})
```

### Additional Connection Parameters

```go
wrapper := pgxpool.NewWithConfig(pool, pgxpool.Config{
    AdditionalParams: "sslmode=require&application_name=my_test_app",
})
```

## Features

### Batch Queries

```go
pgxPool, _ := wrapper.Acquire(t)

batch := &pgx.Batch{}
batch.Queue("UPDATE users SET active = $1 WHERE id = $2", true, 1)
batch.Queue("UPDATE users SET active = $1 WHERE id = $2", true, 2)
batch.Queue("SELECT COUNT(*) FROM users WHERE active = true")

results := pgxPool.SendBatch(ctx, batch)
defer results.Close()

// Process results in order
tag, err := results.Exec()
tag, err = results.Exec()

var count int
err = results.QueryRow().Scan(&count)
```

### COPY Protocol

```go
pgxPool, _ := wrapper.Acquire(t)

rows := [][]any{
    {"Alice", "alice@example.com"},
    {"Bob", "bob@example.com"},
}

copyCount, err := pgxPool.CopyFrom(
    ctx,
    pgx.Identifier{"users"},
    []string{"name", "email"},
    pgx.CopyFromRows(rows),
)
```

### Custom Pool Configuration

```go
pgxPool, err := wrapper.AcquireWithConfig(t, func(config *pgxpool.Config) {
    config.MaxConns = 5
    config.MinConns = 1
    config.MaxConnLifetime = 5 * time.Minute
    config.MaxConnIdleTime = 1 * time.Minute
})
```

### Using Both Interfaces

Sometimes you need both `database/sql` and pgx interfaces:

```go
sqlDB, pgxPool, err := wrapper.AcquireBoth(t)
if err != nil {
    t.Fatal(err)
}

// Use sqlDB for database/sql operations
rows, _ := sqlDB.Query("SELECT * FROM users")

// Use pgxPool for pgx operations
batch := &pgx.Batch{}
// ...
```

## Password Sources

The package provides several built-in password sources:

- `DefaultPasswordSource`: Checks DB_PASSWORD, PGPASSWORD, POSTGRES_PASSWORD environment variables
- `EnvPasswordSource(varName)`: Reads from a specific environment variable
- `StaticPasswordSource(password)`: Returns a hardcoded password

## Host Sources

Similarly for host configuration:

- `DefaultHostSource`: Queries PostgreSQL or falls back to environment variables
- `EnvHostSource(hostVar, portVar)`: Reads from specific environment variables

## Limitations

- Password cannot be extracted from existing `*sql.DB` connections
- Each `Acquire` creates a new pgxpool instance (pools are not reused between tests)
- URL encoding is applied to user and password fields for special characters

## Example

See the [examples/pgxpool](../examples/pgxpool/) directory for a complete working example including:
- Basic pgx feature usage
- Batch operations
- COPY protocol
- Custom configuration
- Multi-package parallel testing