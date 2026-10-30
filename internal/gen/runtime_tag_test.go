package gen_test

import (
	"testing"

	"github.com/ornew/pego/internal/gentest"
)

// The go commands that the tests run build the runtime that the generators of the test binary
// emit.
func TestSubprocessRuntimeTag(t *testing.T) {
	t.Parallel()
	gentest.CheckRuntimeTag(t)
}
