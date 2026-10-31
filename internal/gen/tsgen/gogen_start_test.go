package tsgen_test

import (
	"fmt"
	"github.com/ornew/zeroparse/internal/syntax"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

// startEntryWant is what the interpreter reports for each of startEntryNames, one line each.
func startEntryWant(t *testing.T) string {
	t.Helper()
	prog, err := compileStarts(t, gentest.StartRestrictionSource)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, name := range gentest.StartEntryNames {
		if err := prog.CheckStart(name); err != nil {
			fmt.Fprintf(&b, "%s: %v\n", name, err)
		} else {
			fmt.Fprintf(&b, "%s: ok\n", name)
		}
	}
	return b.String()
}

// Generated TypeScript parsers enter only start rules, with the interpreter's errors.
func TestGeneratedTSStartRules(t *testing.T) {
	t.Parallel()
	want := startEntryWant(t)
	g, err := syntax.Parse(gentest.StartRestrictionSource)
	if err != nil {
		t.Fatal(err)
	}
	script := `import { parse, parseRule, parseRuleWithOptions } from "./parser.ts";
const names = ["main", "item", "other", "_h", "nope", ""];
for (const f of [(n: string) => parseRule(n, "a"), (n: string) => parseRuleWithOptions(n, "a", {})]) {
  for (const n of names) {
    const r = f(n);
    console.log(n + ": " + (r.error === null ? "ok" : r.error.message));
  }
}
console.log("default: " + (parse("a").error === null) + " " + (parse("aa").error === null));
console.log("empty: " + (parseRule("", "aa").error === null) + " " + (parseRuleWithOptions("", "aa", {}).error === null));
`
	for _, start := range []string{"", "item"} {
		// An empty name selects the declared default start rule main, which matches "aa",
		// even in a parser generated for item.
		out := gentest.RunTSGrammarScriptWith(t, g, script, gen.GenOptions{Start: start})
		parse := "default: true true\n"
		if start == "item" {
			parse = "default: true false\n"
		}
		if expected := want + want + parse + "empty: true true\n"; out != expected {
			t.Errorf("start %q: got\n%s\nwant\n%s", start, out, expected)
		}
	}
}
