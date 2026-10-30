package tsgen_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/gentest"
)

// ignoreCaseLines is the expected output of a program that parses each input and prints "ok" with
// the consumed input, or "error".
func ignoreCaseLines(t *testing.T, src string, inputs []string) string {
	t.Helper()
	prog := mustCompile(t, src)
	var b strings.Builder
	for _, in := range inputs {
		n, err := prog.ParseWith("main", in, engine.ParseOptions{Expectations: true, Backend: engine.Closure})
		if err != nil {
			b.WriteString("error\n")
			continue
		}
		r := []rune(in)
		fmt.Fprintf(&b, "ok %s\n", string(r[n.Start():n.End()]))
	}
	return b.String()
}

// TestIgnoreCaseGeneratedTS checks that the generated TypeScript parser agrees with the closure
// backend on case-insensitive literals.
func TestIgnoreCaseGeneratedTS(t *testing.T) {
	t.Parallel()
	want := ignoreCaseLines(t, gentest.IgnoreCaseGenGrammar, gentest.IgnoreCaseGenInputs)
	var list strings.Builder
	for _, in := range gentest.IgnoreCaseGenInputs {
		fmt.Fprintf(&list, "  %s,\n", strconv.Quote(in))
	}
	script := "import { parse } from \"./parser.ts\";\nfor (const s of [\n" + list.String() + "]) {\n" +
		"  const r = parse(s);\n  if (r.error !== null) {\n    console.log(\"error\");\n    continue;\n  }\n" +
		"  console.log(\"ok \" + Array.from(s).slice(r.node.start, r.node.end).join(\"\"));\n}\n"
	if got := runTSScript(t, gentest.IgnoreCaseGenGrammar, script); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
