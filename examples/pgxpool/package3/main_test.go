package package3_test

import (
	"os"
	"testing"

	"github.com/yuku/testdbpool/examples/pgxpool/shared"
	tpgxpool "github.com/yuku/testdbpool/pgxpool"
)

var poolWrapper *tpgxpool.Wrapper

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
