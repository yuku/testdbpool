# testdbpool - PostgreSQL Test Database Pool Library

## Overview
A Go library designed to accelerate PostgreSQL-based testing by managing database pools using template databases for efficient database operations during test execution. Enables pool sharing across multiple packages executed with `go test ./...` through PostgreSQL-based state management.

## Key Features
- High-speed database replication from template databases
- Database pool management up to N instances
- Pool state sharing across multiple processes (using PostgreSQL state management tables)
- Automatic reset processing for test isolation
- Integration with testing.T for automatic resource management

## Package Design

### Package Name
`testdbpool`

### Main Public API

```go
package testdbpool

import (
    "context"
    "database/sql"
    "runtime"
    "testing"
    "time"
)

// Pool manages database pools
type Pool struct {
    // Internal implementation is private
}

// Configuration holds pool initialization settings
type Configuration struct {
    // Database connection settings (for state management table, required)
    RootConnection *sql.DB
    
    // State management database name (default: "postgres")
    StateDatabase string
    
    // Pool ID (identifier for multi-process & DB name prefix, required)
    // Max 50 characters, alphanumeric and underscore only
    PoolID string
    
    // Maximum pool size (default: runtime.GOMAXPROCS(0) * 2)
    MaxPoolSize int
    
    // Timeout settings (default: 30 seconds)
    AcquireTimeout time.Duration
    
    // Template DB creation function (schema + seed data, required)
    TemplateCreator func(ctx context.Context, db *sql.DB) error
    
    // Reset function (data reset on Release, required)
    ResetFunc func(ctx context.Context, db *sql.DB) error
}

// Initialization function
func New(config Configuration) (*Pool, error)

// Database acquisition (automatically releases via testing.T.Cleanup)
func (p *Pool) Acquire(t *testing.T) (*sql.DB, error)
```

## Operational Specifications

### Pool State Management
Create the following table in PostgreSQL to manage pool state:

```sql
CREATE TABLE IF NOT EXISTS testdbpool_state (
    pool_id VARCHAR PRIMARY KEY,           -- Pool identifier (also serves as DB name prefix)
    template_db VARCHAR NOT NULL,          -- Template DB name
    available_dbs TEXT[] DEFAULT '{}',     -- List of available DB names
    in_use_dbs TEXT[] DEFAULT '{}',        -- List of in-use DB names
    failed_dbs TEXT[] DEFAULT '{}',        -- List of unusable DB names (reset failures, etc.)
    max_pool_size INTEGER NOT NULL,        -- Maximum pool size
    created_at TIMESTAMP DEFAULT NOW(),
    last_accessed TIMESTAMP DEFAULT NOW()
);
```

### Initialization Flow
1. **Configuration Validation**:
   - `RootConnection` must not be nil
   - `PoolID` must be 1-50 characters, alphanumeric and underscore only
   - `TemplateCreator` and `ResetFunc` must not be nil
   - Other invalid values result in errors
2. **Apply Default Values**:
   - `StateDatabase`: "postgres"
   - `MaxPoolSize`: `runtime.GOMAXPROCS(0) * 2`
   - `AcquireTimeout`: 30 seconds
3. **State Management DB Connection**: Connect to specified database
4. Create pool state management table `testdbpool_state` if it doesn't exist
5. Check for existing record with specified pool_id
6. **If existing record exists**: Use existing configuration (no cleanup performed)
7. **If new**: Insert new pool configuration into table
8. Return Pool instance

### Acquire Flow
1. **Start PostgreSQL Transaction** (with timeout)
2. **Acquire Pool State Lock**: `SELECT ... FOR UPDATE`
3. **First Acquire Only**: Create template database (`{pool_id}_template`)
   - Error during `TemplateCreator` execution: Leave partially created DB as-is and return error
4. **Check Available DBs**:
   - If `available_dbs` has capacity: Take DB and move to `in_use_dbs`
   - If no capacity and under `max_pool_size`: Create new DB and add to `in_use_dbs`
   - If no capacity and at limit: Wait until timeout then return error
5. **Commit State Update**
6. **On Timeout**: Ensure lock is released and return error
7. Register Release processing with `testing.T.Cleanup`
8. Return `*sql.DB`

### Release Flow (Automatic Execution)
1. **Execute Reset Function** (restore database to seed data state)
   - Error during `ResetFunc` execution: Move DB to `failed_dbs` and mark as unusable
2. **Start PostgreSQL Transaction**
3. **Acquire Pool State Lock**: `SELECT ... FOR UPDATE`
4. **Update State**: 
   - Reset success: Move DB from `in_use_dbs` to `available_dbs`
   - Reset failure: Move DB from `in_use_dbs` to `failed_dbs`
5. **Update last_accessed**
6. **Commit Transaction**

### Database Deletion Processing
Cleanup outside of initialization requires manual execution by users:

```go
// Manual cleanup function (provided separately)
func Cleanup(rootDB *sql.DB, poolID string) error
```

Deletion Rules:
- Other processes connected to target DB: Error and abort processing
- Target DB doesn't exist: Ignore and continue
- PostgreSQL user lacks DB deletion permissions: Error and abort processing

### Error Handling
- All invalid input values: Return error (no automatic interpretation or correction)
- Template DB creation failure: Return error (test failure)
- Reset failure: Mark DB as unusable, continue testing with other DBs
- Pool exhaustion/timeout: Return error (test failure)
- Use simple `fmt.Errorf`, no complex wrapping

### Logging
- Environment with `testing.T`: Use `t.Log`
- Environment without `testing.T`: Use standard `log` package

### Concurrency Control
- Reliable exclusive control via PostgreSQL transactions + `FOR UPDATE`
- Support simultaneous initialization, Acquire/Release calls across multiple processes
- Timeout settings for deadlock avoidance

## Implementation Requirements

### Coding Style
- Adopt functional programming style (internal implementation)
- Explicit configuration specification via struct format
- Strive for immutable design
- Minimize side effects

### Database Naming Conventions
- State management table: `testdbpool_state` in specified database
- Template DB: `{pool_id}_template`
- Individual DBs: `{pool_id}_1`, `{pool_id}_2`, ...
- PoolID serves as DB name prefix, considering 63-character limit

### Dependencies
- Use standard library only
- External DB drivers managed by users
- Utilize `database/sql` interface

## Utility Functions (Reset Strategies)

The library provides utility functions implementing common reset strategies:

```go
package testdbpool

// Transaction-based reset (no nested transactions)
func ResetByTransaction() func(ctx context.Context, db *sql.DB) error

// TRUNCATE specified tables + restore initial data
func ResetByTruncate(tables []string, seedFunc func(ctx context.Context, db *sql.DB) error) func(ctx context.Context, db *sql.DB) error

// Database recreation (most reliable but time-consuming)
func ResetByRecreation(templateCreator func(ctx context.Context, db *sql.DB) error) func(ctx context.Context, db *sql.DB) error

// Custom SQL-based reset
func ResetBySQL(resetSQL string) func(ctx context.Context, db *sql.DB) error

// Manual cleanup function
func Cleanup(rootDB *sql.DB, poolID string) error
```

## pgxpool Support

The library provides an optional wrapper package `testdbpool/pgxpool` for users who need pgx-specific features:

```go
package pgxpool

import (
    "testing"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/yuku/testdbpool"
)

// Wrapper wraps a testdbpool.Pool to provide pgxpool.Pool instances
type Wrapper struct {
    pool *testdbpool.Pool
    config Config
}

// Config holds configuration for pgxpool wrapper
type Config struct {
    // Custom password source (default: environment variables)
    PasswordSource PasswordSource
    
    // Custom host source (default: environment variables)
    HostSource HostSource
    
    // Additional connection parameters
    AdditionalParams string
}

// Create wrapper with default configuration
func New(pool *testdbpool.Pool) *Wrapper

// Create wrapper with custom configuration
func NewWithConfig(pool *testdbpool.Pool, config Config) *Wrapper

// Acquire pgxpool.Pool (automatically releases via testing.T.Cleanup)
func (w *Wrapper) Acquire(t *testing.T) (*pgxpool.Pool, error)

// Acquire with custom pgxpool configuration
func (w *Wrapper) AcquireWithConfig(t *testing.T, configFunc func(*pgxpool.Config)) (*pgxpool.Pool, error)

// Acquire both *sql.DB and *pgxpool.Pool
func (w *Wrapper) AcquireBoth(t *testing.T) (*sql.DB, *pgxpool.Pool, error)
```

### pgxpool Usage Example

```go
import (
    "github.com/yuku/testdbpool"
    "github.com/yuku/testdbpool/pgxpool"
)

func TestMain(m *testing.M) {
    // Create regular testdbpool
    pool, err := testdbpool.New(testdbpool.Configuration{
        RootConnection:  rootDB,
        PoolID:         "myapp_test",
        TemplateCreator: createSchema,
        ResetFunc:      resetData,
    })
    if err != nil {
        log.Fatal(err)
    }
    
    // Wrap for pgxpool support
    poolWrapper := pgxpool.New(pool)
    
    os.Exit(m.Run())
}

func TestBatchQueries(t *testing.T) {
    // Get pgxpool.Pool instead of *sql.DB
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

### pgxpool Features

- **Batch Queries**: Execute multiple queries in a single round trip
- **COPY Protocol**: High-performance bulk data operations
- **Notifications**: LISTEN/NOTIFY support
- **Native Types**: Work with PostgreSQL arrays, JSON, etc.
- **Connection Pool Stats**: Monitor pool health and performance
- **Query Tracing**: Custom logging and monitoring

### pgxpool Configuration

The wrapper extracts connection parameters from the test database connection and constructs a pgxpool connection string. For security reasons, passwords cannot be extracted from existing connections, so the wrapper uses:

1. **Default**: Environment variables (DB_PASSWORD, PGPASSWORD)
2. **Custom**: Provide your own PasswordSource implementation

```go
// Custom password source
poolWrapper := pgxpool.NewWithConfig(pool, pgxpool.Config{
    PasswordSource: pgxpool.StaticPasswordSource("mypassword"),
    HostSource: pgxpool.EnvHostSource("CUSTOM_HOST", "CUSTOM_PORT"),
    AdditionalParams: "application_name=myapp_test",
})
```

## Usage Example

```go
func TestMain(m *testing.M) {
    // Prepare root DB connection (for state management)
    rootDB, _ := sql.Open("postgres", "postgres://user:pass@localhost/postgres")
    
    // Manual cleanup (if needed)
    // testdbpool.Cleanup(rootDB, "myapp_test")
    
    // Pool initialization
    pool, err := testdbpool.New(testdbpool.Configuration{
        RootConnection:  rootDB,
        StateDatabase:   "postgres", // Can be omitted as it's the default
        PoolID:          "myapp_test", // Inter-process shared identifier (also serves as DB name prefix)
        MaxPoolSize:     10, // Default is runtime.GOMAXPROCS(0)*2
        AcquireTimeout:  30 * time.Second, // Can be omitted as it's the default
        TemplateCreator: func(ctx context.Context, db *sql.DB) error {
            // Execute migrations + insert seed data
            if err := runMigrations(ctx, db); err != nil {
                return err
            }
            return insertSeedData(ctx, db)
        },
        ResetFunc: testdbpool.ResetByTruncate(
            []string{"users", "orders", "payments"}, 
            func(ctx context.Context, db *sql.DB) error {
                return insertSeedData(ctx, db)
            },
        ),
    })
    if err != nil {
        log.Fatal(err)
    }
    
    // Execute tests
    os.Exit(m.Run())
}

func TestUserRepository(t *testing.T) {
    // Acquire database (automatically registers with Cleanup)
    db, err := pool.Acquire(t)
    if err != nil {
        t.Fatal(err)
    }
    
    // Execute test
    repo := NewUserRepository(db)
    // Test processing...
}
```

## Performance Goals
- Significantly reduce DB preparation time for each test through template-based DB replication
- Optimize resource efficiency through pool sharing during `go test ./...` multi-package execution
- Reduce total test execution time in CI pipelines
- Optimize database resource utilization during parallel test execution

## Technical Constraints
- PostgreSQL only
- Go 1.20 or higher
- Uses additional PostgreSQL tables for pool state management
- Manual cleanup required (no automatic deletion)
- Uses PostgreSQL transactions for inter-process communication (slight overhead)
- PoolID limited to alphanumeric and underscore characters, max 50 characters
