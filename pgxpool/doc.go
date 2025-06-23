// Package pgxpool provides integration between testdbpool and pgx/v5/pgxpool,
// allowing tests to use pgx-specific features while maintaining test isolation.
//
// # Basic Usage
//
// The simplest way to use this package is to wrap an existing testdbpool.Pool:
//
//	pool, err := testdbpool.New(config)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	
//	wrapper := pgxpool.New(pool)
//	
//	func TestSomething(t *testing.T) {
//	    pgxPool, err := wrapper.Acquire(t)
//	    if err != nil {
//	        t.Fatal(err)
//	    }
//	    
//	    // Use pgx-specific features
//	    batch := &pgx.Batch{}
//	    batch.Queue("INSERT INTO users (name) VALUES ($1)", "Alice")
//	    results := pgxPool.SendBatch(ctx, batch)
//	}
//
// # Configuration
//
// For more control over connection parameters, use NewWithConfig:
//
//	wrapper := pgxpool.NewWithConfig(pool, pgxpool.Config{
//	    PasswordSource: pgxpool.EnvPasswordSource("MY_DB_PASSWORD"),
//	    HostSource: pgxpool.EnvHostSource("MY_DB_HOST", "MY_DB_PORT"),
//	    AdditionalParams: "sslmode=require&connect_timeout=10",
//	})
//
// # Password Sources
//
// The package provides several ways to obtain database passwords:
//
//   - DefaultPasswordSource: Checks DB_PASSWORD, PGPASSWORD, POSTGRES_PASSWORD
//   - EnvPasswordSource: Reads from a specific environment variable
//   - StaticPasswordSource: Returns a hardcoded password
//
// You can also implement custom PasswordSource functions.
//
// # Host Sources
//
// Similarly, host and port information can be obtained through:
//
//   - DefaultHostSource: Queries PostgreSQL or falls back to environment variables
//   - EnvHostSource: Reads from specific environment variables
//
// # Advanced Usage
//
// For tests that need both database/sql and pgx interfaces:
//
//	sqlDB, pgxPool, err := wrapper.AcquireBoth(t)
//	if err != nil {
//	    t.Fatal(err)
//	}
//	
//	// Use sqlDB for database/sql operations
//	// Use pgxPool for pgx-specific operations
//
// # Custom Pool Configuration
//
// Apply custom pgxpool configuration:
//
//	pgxPool, err := wrapper.AcquireWithConfig(t, func(config *pgxpool.Config) {
//	    config.MaxConns = 5
//	    config.MinConns = 1
//	    config.MaxConnLifetime = 5 * time.Minute
//	})
//
// # Connection String Building
//
// The wrapper automatically builds connection strings by:
//
// 1. Querying the test database name from the acquired connection
// 2. Obtaining host/port via the configured HostSource
// 3. Getting the current user from the database
// 4. Retrieving password via the configured PasswordSource
// 5. Appending any additional parameters
//
// # Limitations
//
//   - Password cannot be extracted from existing database/sql connections
//   - Each Acquire creates a new pgxpool rather than sharing pools
//   - Some PostgreSQL configurations may require custom HostSource implementations
package pgxpool