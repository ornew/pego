package compile

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse/grammar"
)

// TestIgnoreCasePragmaReachesArguments checks that the pragma folds the literals of a recovery
// skip expression written in the annotated expression, and that the skip is not a called rule.
func TestIgnoreCasePragmaReachesArguments(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = ("ab" #recover(skip="x")) #ignorecase`)
	found := map[string]bool{}
	WalkExpr(prog.ByName["main"].Def.Expr, func(e grammar.Expr) {
		if l, ok := e.(*grammar.Literal); ok {
			found[l.Value] = l.IgnoreCase
		}
	})
	if !found["ab"] || !found["x"] {
		t.Fatalf("literals under the pragma are not case-insensitive: %v", found)
	}
}

// TestIgnoreCaseAttributeFormRejected checks that the attribute spelling of the pragma is rejected
// with the spelling to use.
func TestIgnoreCaseAttributeFormRejected(t *testing.T) {
	t.Parallel()
	if msg := compileError(t, `rule main = "a" @ignorecase`); !strings.Contains(msg, "use #ignorecase") {
		t.Fatalf("attribute form: %v", msg)
	}
}
