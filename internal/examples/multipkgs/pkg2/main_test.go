package pkg2

import (
	"os"
	"testing"

	"github.com/yuku/testdbpool/internal/examples/multipkgs"
)

func Test1(t *testing.T) {
	if os.Getenv("TESTDBPOOL_RUN_MULTIPKG_TESTS") != "1" {
		t.Skip("Skipping multipkg test. Set TESTDBPOOL_RUN_MULTIPKG_TESTS=1 to run.")
	}
	multipkgs.RunTest(t)
}

func Test2(t *testing.T) {
	if os.Getenv("TESTDBPOOL_RUN_MULTIPKG_TESTS") != "1" {
		t.Skip("Skipping multipkg test. Set TESTDBPOOL_RUN_MULTIPKG_TESTS=1 to run.")
	}
	multipkgs.RunTest(t)
}
