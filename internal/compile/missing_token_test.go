package compile

import (
	"strings"
	"testing"
)

func TestMissingTokensModuleGuard(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = ";" #missing`)
	defer func() {
		x := recover()
		err, ok := x.(error)
		if !ok || !strings.Contains(err.Error(), "bytecode module construction") {
			t.Fatal("silent bytecode lowering", x)
		}
	}()
	prog.BytecodeModule()
}
