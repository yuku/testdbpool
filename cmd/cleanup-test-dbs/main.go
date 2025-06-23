package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/lib/pq"
	"github.com/yuku/testdbpool"
)

func main() {
	// Connect to PostgreSQL
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "localhost"
	}
	user := os.Getenv("PGUSER")
	if user == "" {
		user = "postgres"
	}
	password := os.Getenv("PGPASSWORD")
	if password == "" {
		password = "postgres"
	}

	connStr := fmt.Sprintf("postgres://%s:%s@%s/postgres?sslmode=disable", user, password, host)
	rootDB, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}
	defer rootDB.Close()

	// List of pool IDs used in tests
	poolIDs := []string{
		"example_test",
		"blog_api_test",
		"test_pool_1",
		"test_pool_2",
		"test_pool_3",
		"test_pool_4",
		"test_acquire_pool",
		"test_concurrent_pool",
		"test_exhaustion_pool",
		"test_cleanup_pool",
	}

	for _, poolID := range poolIDs {
		fmt.Printf("Cleaning up pool: %s\n", poolID)
		if err := testdbpool.Cleanup(rootDB, poolID); err != nil {
			fmt.Printf("  Warning: %v\n", err)
		} else {
			fmt.Printf("  Success\n")
		}
	}

	// Also clean up any orphaned databases
	fmt.Println("\nCleaning up orphaned test databases...")
	query := `
		SELECT datname FROM pg_database 
		WHERE datname LIKE '%_test_%' 
		   OR datname LIKE 'test_%'
		   OR datname LIKE 'example_test%'
	`
	rows, err := rootDB.Query(query)
	if err != nil {
		log.Printf("Failed to query databases: %v", err)
		return
	}
	defer rows.Close()

	var databases []string
	for rows.Next() {
		var dbName string
		if err := rows.Scan(&dbName); err != nil {
			log.Printf("Failed to scan database name: %v", err)
			continue
		}
		databases = append(databases, dbName)
	}

	for _, dbName := range databases {
		fmt.Printf("Dropping database: %s\n", dbName)
		// Terminate connections
		termQuery := fmt.Sprintf(`
			SELECT pg_terminate_backend(pid) 
			FROM pg_stat_activity 
			WHERE datname = '%s' AND pid <> pg_backend_pid()`, dbName)
		rootDB.Exec(termQuery)

		// Drop database
		dropQuery := fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName)
		if _, err := rootDB.Exec(dropQuery); err != nil {
			fmt.Printf("  Warning: %v\n", err)
		} else {
			fmt.Printf("  Success\n")
		}
	}

	fmt.Println("\nCleanup complete!")
}