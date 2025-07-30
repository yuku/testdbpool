package pgconst

import "regexp"

const (
	// MaxDatabaseNameLength is the maximum length of a database name in PostgreSQL.
	MaxDatabaseNameLength = 63

	// MaxIdentifierLength is the maximum length of a PostgreSQL identifier.
	MaxIdentifierLength = 63
)

var (
	// PostgreSQL identifier regex pattern: starts with letter/underscore,
	// followed by letters/digits/underscores/dollar signs, max 63 characters
	postgresIdentifierRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_$]*$`)
)

// IsValidPostgreSQLIdentifier checks if the given string is a valid PostgreSQL identifier.
// PostgreSQL identifiers must start with a letter or underscore, followed by letters,
// digits, underscores, or dollar signs, and must not exceed 63 characters.
func IsValidPostgreSQLIdentifier(identifier string) bool {
	if len(identifier) == 0 || len(identifier) > MaxIdentifierLength {
		return false
	}
	return postgresIdentifierRegex.MatchString(identifier)
}
