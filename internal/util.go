package internal

import (
	"database/sql"
	"fmt"
	"os"
)

// GetConnectionString creates a connection string for the given database name
// Since sql.DB doesn't expose the original connection string, this uses
// environment variables or defaults
func GetConnectionString(db *sql.DB, dbName string) string {
	// In a real implementation, you'd pass the connection string through config
	// For testing, we'll use environment variables that match what the tests set
	host := GetEnvOrDefault("DB_HOST", GetEnvOrDefault("PGHOST", "localhost"))
	port := GetEnvOrDefault("DB_PORT", GetEnvOrDefault("PGPORT", "5432"))
	user := GetEnvOrDefault("DB_USER", GetEnvOrDefault("PGUSER", "postgres"))
	password := GetEnvOrDefault("DB_PASSWORD", GetEnvOrDefault("PGPASSWORD", "postgres"))
	sslmode := GetEnvOrDefault("PGSSLMODE", "disable")

	if password != "" {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
			user, password, host, port, dbName, sslmode)
	}
	return fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=%s",
		user, host, port, dbName, sslmode)
}

// GetEnvOrDefault gets an environment variable or returns a default value
func GetEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GetDriverName returns the driver name from the database connection
func GetDriverName(db *sql.DB) string {
	// To avoid data races, we'll use environment variables and defaults
	// instead of trying to inspect the database object

	// First, check environment variable
	if driver := os.Getenv("DB_DRIVER"); driver != "" {
		return driver
	}

	// Try to detect which driver is available by checking registered drivers
	for _, driver := range sql.Drivers() {
		switch driver {
		case "pgx":
			return "pgx"
		case "postgres":
			return "postgres"
		}
	}

	// Default to postgres (lib/pq) as it's more commonly used
	return "postgres"
}
