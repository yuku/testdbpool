package pgxpool_test

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	tpgxpool "github.com/yuku/testdbpool/pgxpool"
)

func ExampleWrapper_Acquire() {
	// Assumes testPool is already initialized
	wrapper := tpgxpool.New(testPool)
	
	// In a test function
	t := &testing.T{} // This would be provided by the test framework
	
	pool, err := wrapper.Acquire(t)
	if err != nil {
		log.Fatal(err)
	}
	
	ctx := context.Background()
	
	// Use pgx-specific features
	var name string
	err = pool.QueryRow(ctx, "SELECT name FROM users WHERE id = $1", 1).Scan(&name)
	if err != nil {
		log.Fatal(err)
	}
	
	fmt.Printf("User: %s\n", name)
}

func ExampleWrapper_AcquireWithConfig() {
	wrapper := tpgxpool.New(testPool)
	t := &testing.T{}
	
	// Acquire with custom pool configuration
	pool, err := wrapper.AcquireWithConfig(t, func(config *pgxpool.Config) {
		config.MaxConns = 5
		config.MinConns = 1
		
		// Add custom behavior
		// For example, you could add a query tracer here
		// config.ConnConfig.Tracer = customTracer
	})
	if err != nil {
		log.Fatal(err)
	}
	
	// Use the configured pool
	_ = pool
}

func ExampleNewWithConfig() {
	// Create wrapper with custom configuration
	wrapper := tpgxpool.NewWithConfig(testPool, tpgxpool.Config{
		// Use a specific environment variable for password
		PasswordSource: tpgxpool.EnvPasswordSource("MY_APP_DB_PASSWORD"),
		
		// Use custom host/port logic
		HostSource: func(db *sql.DB) (string, string, error) {
			// Custom logic to determine host/port
			// For example, could read from service discovery
			return "db.example.com", "5432", nil
		},
		
		// Add connection parameters
		AdditionalParams: "application_name=my_app&statement_timeout=30000",
	})
	
	t := &testing.T{}
	pool, err := wrapper.Acquire(t)
	if err != nil {
		log.Fatal(err)
	}
	
	// Connection will use the custom configuration
	_ = pool
}

func ExampleWrapper_AcquireBoth() {
	wrapper := tpgxpool.New(testPool)
	t := &testing.T{}
	
	// Get both interfaces
	sqlDB, pgxPool, err := wrapper.AcquireBoth(t)
	if err != nil {
		log.Fatal(err)
	}
	
	ctx := context.Background()
	
	// Use database/sql interface
	rows, err := sqlDB.QueryContext(ctx, "SELECT id, name FROM users")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	
	// Use pgx interface for batch operations
	batch := &pgx.Batch{}
	batch.Queue("UPDATE users SET last_seen = NOW() WHERE id = $1", 1)
	batch.Queue("UPDATE users SET last_seen = NOW() WHERE id = $1", 2)
	
	results := pgxPool.SendBatch(ctx, batch)
	defer results.Close()
}