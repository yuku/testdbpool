package package1_test

import (
	"os"
	"testing"

	"github.com/yuku/testdbpool/examples/pgxpool/shared"
)

func TestMain(m *testing.M) {
	// Initialize shared pool
	_, err := shared.GetPoolWrapper()
	if err != nil {
		panic(err)
	}
	
	// Run tests
	os.Exit(m.Run())
}