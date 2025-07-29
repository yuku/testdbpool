// Package gitutil provides Git-related utilities for schema versioning in testdbpool.
//
// This package helps manage database schema versions by tracking Git commits
// and detecting unstaged changes in schema files.
package gitutil

import (
	"crypto/rand"
	"fmt"
	"os/exec"
	"strings"
)

// GetFilesRevision returns the latest git commit hash that affected any of the specified files.
// Returns the first 8 characters of the commit hash, or an error if git operations fail.
func GetFilesRevision(filePaths []string) (string, error) {
	if len(filePaths) == 0 {
		return "", fmt.Errorf("no file paths provided")
	}

	args := []string{"log", "-n", "1", "--pretty=format:%H", "--"}
	args = append(args, filePaths...)

	cmd := exec.Command("git", args...)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git log failed: %w", err)
	}

	revision := strings.TrimSpace(string(output))
	if revision == "" {
		return "", fmt.Errorf("no commits found for specified files")
	}

	if len(revision) >= 8 {
		return revision[:8], nil
	}

	return revision, nil
}

// HasUnstagedChanges checks if any of the specified files have unstaged changes.
// Returns true if any file has been modified but not staged for commit.
func HasUnstagedChanges(filePaths []string) (bool, error) {
	if len(filePaths) == 0 {
		return false, nil
	}

	args := []string{"diff", "--name-only", "--"}
	args = append(args, filePaths...)

	cmd := exec.Command("git", args...)
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git diff failed: %w", err)
	}

	return strings.TrimSpace(string(output)) != "", nil
}

// GetSchemaVersion returns a schema version string based on Git history.
// If any of the specified files have unstaged changes, returns a random version
// to force creation of a new database pool.
// Otherwise, returns the git commit hash of the latest change to any of the files.
func GetSchemaVersion(schemaPaths []string) string {
	// Check for unstaged changes first
	hasChanges, err := HasUnstagedChanges(schemaPaths)
	if err != nil {
		// If git operations fail, use random version for safety
		return generateRandomVersion()
	}

	if hasChanges {
		// Force new database creation for unstaged changes
		return generateRandomVersion()
	}

	// Get git revision of schema files
	revision, err := GetFilesRevision(schemaPaths)
	if err != nil {
		// If git operations fail, use random version for safety
		return generateRandomVersion()
	}

	return revision
}

// generateRandomVersion creates a random 8-character hex string.
func generateRandomVersion() string {
	bytes := make([]byte, 4)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to a fixed string if random generation fails
		return "unknown"
	}
	return fmt.Sprintf("%x", bytes)
}