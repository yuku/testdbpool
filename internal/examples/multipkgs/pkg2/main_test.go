package pkg2

import (
	"testing"

	"github.com/yuku/testdbpool/internal/examples/multipkgs"
)

func Test1(t *testing.T) {
	t.Parallel()
	multipkgs.RunTest(t)
}

func Test2(t *testing.T) {
	t.Parallel()
	multipkgs.RunTest(t)
}
