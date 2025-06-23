package package2_test

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

	// Cleanup is handled by package3's TestMain
	os.Exit(code)
}