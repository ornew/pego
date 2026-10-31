package complexitytest

import (
	"os"
	"testing"

	"github.com/ornew/zeroparse/internal/gentest"
)

// TestMain gives the go commands that the tests run, which build generated parsers, a build cache
// of their own, removed at the end.
func TestMain(m *testing.M) {
	remove := gentest.SetGoCache()
	code := m.Run()
	remove()
	os.Exit(code)
}
