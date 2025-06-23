package package3_test

import (
	"os"
	"testing"

	"github.com/yuku/testdbpool/examples/multiple-packages/shared"
)

func TestMain(m *testing.M) {
	// Initialize the shared pool
	if err := shared.InitializePool(); err != nil {
		panic(err)
	}

	// Run tests
	code := m.Run()

	// Clean up after all tests (this is the last package)
	if err := shared.CleanupPool(); err != nil {
		// Log but don't fail
		println("Warning: failed to cleanup pool:", err.Error())
	}

	os.Exit(code)
}