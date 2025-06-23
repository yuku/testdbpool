package package1_test

import (
	"os"
	"testing"

	"github.com/yuku/testdbpool/examples/pgxpool/shared"
	"github.com/yuku/testdbpool/examples/pgxpool/wrapper"
)

var poolWrapper *wrapper.PoolWrapper

func TestMain(m *testing.M) {
	// Initialize shared pool and get wrapper
	var err error
	poolWrapper, err = shared.GetPoolWrapper()
	if err != nil {
		panic(err)
	}
	
	// Run tests
	os.Exit(m.Run())
}