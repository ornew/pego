package tsgen_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// TestGeneratedTSLeftRecursionGuardIgnoresVariables is TestLeftRecursionGuardIgnoresVariables
// for generated TypeScript parsers.
func TestGeneratedTSLeftRecursionGuardIgnoresVariables(t *testing.T) {
	t.Parallel()
	var script strings.Builder
	script.WriteString("import { parse, recognize } from \"./parser.ts\";\n")
	var want strings.Builder
	prog := compileBare(t, gentest.LRBindingSource, compile.Options{LeftRecursionBindings: true})
	for _, in := range gentest.LRBindingInputs {
		fmt.Fprintf(&script, "{ const r = parse(%q); const e = recognize(%[1]q); console.log(r.error === null ? String(r.node) : \"error: \" + r.error.message, e === null ? \"null\" : e.message); }\n", in)
		n, err := prog.ParseWith("main", in, engine.ParseOptions{Expectations: true, MaxDepth: 400})
		_, recErr := prog.ParseWith("main", in, engine.ParseOptions{Expectations: true, MaxDepth: 400, Recognize: true})
		if err != nil {
			fmt.Fprintf(&want, "error: %v %v\n", err, recErr)
		} else {
			fmt.Fprintf(&want, "%s null\n", n.String())
		}
	}
	out := gentest.RunTSGrammarScriptWith(t, mustParse(gentest.LRBindingSource), script.String(), gen.GenOptions{Start: "main", Recognize: true, MaxDepth: 400, LeftRecursionBindings: true})
	if out != want.String() {
		t.Fatalf("generated\n%s\nengine\n%s", out, want.String())
	}
}
