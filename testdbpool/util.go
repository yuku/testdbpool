package testdbpool

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
)

// getConnectionString creates a connection string for the given database name
// Since sql.DB doesn't expose the original connection string, this uses
// environment variables or defaults
func getConnectionString(db *sql.DB, dbName string) string {
	// In a real implementation, you'd pass the connection string through config
	// For testing, we'll use localhost defaults
	host := getEnvOrDefault("PGHOST", "localhost")
	port := getEnvOrDefault("PGPORT", "5432")
	user := getEnvOrDefault("PGUSER", "postgres")
	password := getEnvOrDefault("PGPASSWORD", "postgres")
	sslmode := getEnvOrDefault("PGSSLMODE", "disable")

	if password != "" {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
			user, password, host, port, dbName, sslmode)
	}
	return fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=%s",
		user, host, port, dbName, sslmode)
}

// getEnvOrDefault gets an environment variable or returns a default value
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// parseConnectionString parses a PostgreSQL connection string
func parseConnectionString(connStr string) (map[string]string, error) {
	params := make(map[string]string)
	
	// Simple parser for postgres://user:pass@host:port/dbname?params format
	if strings.HasPrefix(connStr, "postgres://") || strings.HasPrefix(connStr, "postgresql://") {
		// Remove prefix
		connStr = strings.TrimPrefix(connStr, "postgres://")
		connStr = strings.TrimPrefix(connStr, "postgresql://")
		
		// Split by ?
		parts := strings.SplitN(connStr, "?", 2)
		if len(parts) > 1 {
			// Parse query parameters
			queryParams := strings.Split(parts[1], "&")
			for _, param := range queryParams {
				kv := strings.SplitN(param, "=", 2)
				if len(kv) == 2 {
					params[kv[0]] = kv[1]
				}
			}
		}
		
		// Parse main part
		mainPart := parts[0]
		
		// Extract database name
		slashIdx := strings.LastIndex(mainPart, "/")
		if slashIdx >= 0 {
			params["dbname"] = mainPart[slashIdx+1:]
			mainPart = mainPart[:slashIdx]
		}
		
		// Extract host:port
		atIdx := strings.LastIndex(mainPart, "@")
		if atIdx >= 0 {
			hostPort := mainPart[atIdx+1:]
			if colonIdx := strings.LastIndex(hostPort, ":"); colonIdx >= 0 {
				params["host"] = hostPort[:colonIdx]
				params["port"] = hostPort[colonIdx+1:]
			} else {
				params["host"] = hostPort
			}
			
			// Extract user:password
			userPass := mainPart[:atIdx]
			if colonIdx := strings.Index(userPass, ":"); colonIdx >= 0 {
				params["user"] = userPass[:colonIdx]
				params["password"] = userPass[colonIdx+1:]
			} else {
				params["user"] = userPass
			}
		}
	} else {
		// Parse key=value format
		pairs := strings.Fields(connStr)
		for _, pair := range pairs {
			kv := strings.SplitN(pair, "=", 2)
			if len(kv) == 2 {
				params[kv[0]] = kv[1]
			}
		}
	}
	
	return params, nil
}

// buildConnectionString builds a connection string from parameters
func buildConnectionString(params map[string]string) string {
	// Build in postgres://user:pass@host:port/dbname?params format
	var parts []string
	
	// Required parts
	user := params["user"]
	if user == "" {
		user = "postgres"
	}
	
	host := params["host"]
	if host == "" {
		host = "localhost"
	}
	
	dbname := params["dbname"]
	if dbname == "" {
		dbname = "postgres"
	}
	
	// Build base URL
	baseURL := "postgres://"
	
	// Add user and password
	if pass, ok := params["password"]; ok && pass != "" {
		baseURL += fmt.Sprintf("%s:%s@", user, pass)
	} else {
		baseURL += fmt.Sprintf("%s@", user)
	}
	
	// Add host and port
	if port, ok := params["port"]; ok && port != "" {
		baseURL += fmt.Sprintf("%s:%s", host, port)
	} else {
		baseURL += host
	}
	
	// Add database
	baseURL += "/" + dbname
	
	// Add other parameters
	for k, v := range params {
		if k != "user" && k != "password" && k != "host" && k != "port" && k != "dbname" {
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
	}
	
	if len(parts) > 0 {
		baseURL += "?" + strings.Join(parts, "&")
	}
	
	return baseURL
}