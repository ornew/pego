package tsgen_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

var tsExpectedText = regexp.MustCompile(`syntax error: expected [^"]*`)

// A TypeScript parser generated without expectations, and one generated with them that a parse
// does not ask to record, return the trees of a parse that records and fail at the same
// positions, with no expected list and no messages, with and without budget controls.
func TestGeneratedTSWithoutExpectations(t *testing.T) {
	t.Parallel()
	grammars := []string{
		`rule main = item+ $$
rule item = "x" @error(message="expected an x") / "y" / "(" ^ [a-z]+ ")"`,
		`rule main = stmt* $$
rule stmt = ([a-z]+ #atomic ";") #recover(skip=[^;]+ ";")`,
	}
	const script = `import { parseWithOptions } from "./parser.ts";
for (const s of ["xy", "xz", "z", "(ab)", "(a", "ab;1x;cd;", "ab;cd;", ""]) {
  const r = parseWithOptions(s, OPTIONS);
  const errs = r.error === null ? [] : "errors" in r.error ? r.error.errors : [r.error];
  console.log(JSON.stringify({
    s,
    node: r.node === null ? null : String(r.node).replace(/syntax error: expected [^"]*/g, "syntax error"),
    errors: errs.map((e: any) => [e.pos, e.line, e.col]),
    listed: errs.reduce((n: number, e: any) => n + (e.expected?.length ?? 0) + (e.messages?.length ?? 0), 0),
  }));
}
`
	for _, controls := range []bool{false, true} {
		for i, src := range grammars {
			g, err := parseTest(src)
			if err != nil {
				t.Fatal(err)
			}
			run := func(available, ask bool) []string {
				options := "undefined"
				if ask {
					options = "{ expectations: true }"
				}
				out := gentest.RunTSGrammarScriptRaw(t, g, strings.Replace(script, "OPTIONS", options, 1), gen.GenOptions{Start: "main", Expectations: available, BudgetControls: controls})
				return strings.Split(strings.TrimSuffix(out, "\n"), "\n")
			}
			off, avail, on := run(false, false), run(true, false), run(true, true)
			if len(off) != len(on) || len(avail) != len(on) || len(on) == 0 {
				t.Fatalf("grammar %d: %d and %d results", i, len(off), len(on))
			}
			failed := 0
			for k := range off {
				listedOff, listedOn := lastField(off[k]), lastField(on[k])
				if strip(off[k]) != strip(on[k]) {
					t.Errorf("grammar %d controls %v: results differ\n off %s\n on  %s", i, controls, off[k], on[k])
				}
				if off[k] != avail[k] {
					t.Errorf("grammar %d controls %v: generating with expectations changes a parse that does not ask\n off   %s\n avail %s", i, controls, off[k], avail[k])
				}
				if listedOff != "0" {
					t.Errorf("grammar %d controls %v: expectations recorded: %s", i, controls, off[k])
				}
				if !strings.Contains(on[k], `"errors":[]`) {
					failed++
					if listedOn == "0" {
						t.Errorf("grammar %d controls %v: no expectations recorded: %s", i, controls, on[k])
					}
				}
			}
			if failed == 0 {
				t.Errorf("grammar %d: no input failed", i)
			}
		}
	}
}

// lastField returns the value of the listed member, the last of a result line.
func lastField(line string) string {
	i := strings.LastIndex(line, `"listed":`)
	return strings.TrimSuffix(line[i+len(`"listed":`):], "}")
}

// strip drops the listed member and the expectations in recovered error texts.
func strip(line string) string {
	line = line[:strings.LastIndex(line, `,"listed":`)]
	return tsExpectedText.ReplaceAllString(line, "syntax error")
}
