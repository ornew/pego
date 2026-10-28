package engine

import (
	"testing"

	"github.com/ornew/pego/internal/gen"
)

// The lines of an Operators Rule in the Node and recognizer tables are printed from Flow IR. The tests
// below check the routes of the lines and run the parsers of the corpus against the engine; the generated
// text of the checked-in grammars with an Operators Rule is frozen in parsers/testdata/output_manifest.txt.

// operatorsLineRecoveryCases are Operators Rules with a line that recovers, which the Flow route prints as it
// prints the other lines; the parsers must match the engine (they are in the generated corpus). rule is the
// Operators Rule with the line.
var operatorsLineRecoveryCases = []struct {
	genCase
	rule string
}{
	{genCase{"Operators Rule operand with recovery", `rule main = v:e $$ => $v
rule e = #operators {
    operand ("i" ";") #recover(skip="?")
    level { infix left "+" }
}`, []string{"i;", "i;+i;", "i?+i;", "?", "i;+", "i", ""}}, "e"},
	{genCase{"Operators Rule operand with a cut in the recovered body", `rule main = v:e $$ => $v
rule e = #operators {
    operand ("i" #cut ";") #recover(skip="?")
    level { infix left "+" }
}`, []string{"i;", "i;+i;", "i?+i;", "?", "i;+", "i", "ix", ""}}, "e"},
}

// TestOperatorsLinesWithRecoveryPrintFromFlow checks that a line with recovery is printed from Flow IR in the
// Node and recognizer tables (a line Flow IR does not print fails the generation).
func TestOperatorsLinesWithRecoveryPrintFromFlow(t *testing.T) {
	t.Setenv("PEGO_TEST_ORDINARY", "1")
	for _, c := range operatorsLineRecoveryCases {
		t.Run(c.name, func(t *testing.T) {
			g, err := parseTest(c.src)
			if err != nil {
				t.Fatal(err)
			}
			flowed := map[string]int{}
			operatorsRouteHook = func(table string, r *staticRule) { flowed[table+"/"+r.name]++ }
			defer func() { operatorsRouteHook = nil }()
			if _, err := generateCode(g, gen.GenOptions{Start: "main", Package: "gen", Recognize: true}); err != nil {
				t.Fatal(err)
			}
			for _, table := range []string{"rules", "recRules"} {
				if flowed[table+"/"+c.rule] == 0 {
					t.Errorf("no line of %s is printed from Flow IR in the %s table (%v)", c.rule, table, flowed)
				}
			}
		})
	}
}
