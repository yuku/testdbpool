package gitutil_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yuku/testdbpool/gitutil"
)

// TestBasicAPIBehavior tests that the API functions work without panicking
func TestBasicAPIBehavior(t *testing.T) {
	t.Run("GetFilesRevision basic validation", func(t *testing.T) {
		// Empty file paths should return error
		_, err := gitutil.GetFilesRevision([]string{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no file paths provided")

		// Function should not panic with any input
		assert.NotPanics(t, func() {
			_, _ = gitutil.GetFilesRevision([]string{"any-file.txt"})
		})
	})

	t.Run("HasUnstagedChanges basic validation", func(t *testing.T) {
		// Empty file paths should return false, no error
		hasChanges, err := gitutil.HasUnstagedChanges([]string{})
		assert.NoError(t, err)
		assert.False(t, hasChanges)

		// Function should not panic with any input
		assert.NotPanics(t, func() {
			_, _ = gitutil.HasUnstagedChanges([]string{"any-file.txt"})
		})
	})

	t.Run("GetSchemaVersion always returns non-empty string", func(t *testing.T) {
		// Should always return something, never empty
		version1 := gitutil.GetSchemaVersion([]string{})
		assert.NotEmpty(t, version1)

		version2 := gitutil.GetSchemaVersion([]string{"any-file.txt"})
		assert.NotEmpty(t, version2)

		version3 := gitutil.GetSchemaVersion([]string{"file1.sql", "file2.sql"})
		assert.NotEmpty(t, version3)

		// Function should not panic with any input
		assert.NotPanics(t, func() {
			_ = gitutil.GetSchemaVersion([]string{"any-file.txt"})
		})
	})
}

func TestRandomVersionGeneration(t *testing.T) {
	t.Run("random versions are different", func(t *testing.T) {
		// Test with non-existent files to trigger random generation
		version1 := gitutil.GetSchemaVersion([]string{"non-existent-1.txt"})
		version2 := gitutil.GetSchemaVersion([]string{"non-existent-2.txt"})

		// Random versions should be different (with very high probability)
		assert.NotEqual(t, version1, version2, "random versions should be different")
		assert.NotEmpty(t, version1)
		assert.NotEmpty(t, version2)
	})

	t.Run("random versions have reasonable format", func(t *testing.T) {
		version := gitutil.GetSchemaVersion([]string{"non-existent.txt"})
		assert.NotEmpty(t, version)
		assert.True(t, len(version) >= 8, "version should be at least 8 characters")
	})
}

func TestEdgeCases(t *testing.T) {
	t.Run("very long file paths", func(t *testing.T) {
		longPath := make([]byte, 1000)
		for i := range longPath {
			longPath[i] = 'a'
		}

		version := gitutil.GetSchemaVersion([]string{string(longPath) + ".sql"})
		assert.NotEmpty(t, version)
	})

	t.Run("special characters in file paths", func(t *testing.T) {
		specialPath := "special-chars!@#$%.sql"
		version := gitutil.GetSchemaVersion([]string{specialPath})
		assert.NotEmpty(t, version)
	})

	t.Run("many file paths", func(t *testing.T) {
		manyPaths := make([]string, 100)
		for i := range manyPaths {
			manyPaths[i] = "file" + string(rune('a'+i%26)) + ".sql"
		}
		version := gitutil.GetSchemaVersion(manyPaths)
		assert.NotEmpty(t, version)
	})

	t.Run("nil and empty inputs", func(t *testing.T) {
		// Empty slice
		version := gitutil.GetSchemaVersion([]string{})
		assert.NotEmpty(t, version)

		// Single empty string
		version2 := gitutil.GetSchemaVersion([]string{""})
		assert.NotEmpty(t, version2)
	})
}
