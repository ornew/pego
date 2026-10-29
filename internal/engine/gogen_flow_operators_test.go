package engine

import (
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gen/gogen"
	"github.com/ornew/pego/internal/gentest"
)

// The lines of an Operators Rule in the Node and recognizer tables are printed from Flow IR. The tests
// below check the routes of the lines and run the parsers of the corpus against the engine; the generated
// text of the checked-in grammars with an Operators Rule is frozen in parsers/testdata/output_manifest.txt.

// TestOperatorsLinesWithRecoveryPrintFromFlow checks that a line with recovery is printed from Flow IR in the
// Node and recognizer tables (a line Flow IR does not print fails the generation).
func TestOperatorsLinesWithRecoveryPrintFromFlow(t *testing.T) {
	t.Parallel()
	gentest.PinOrdinary(t)
	for _, c := range gentest.OperatorsLineRecoveryCases {
		t.Run(c.Name, func(t *testing.T) {
			g, err := parseTest(c.Src)
			if err != nil {
				t.Fatal(err)
			}
			flowed := map[string]int{}
			gh := &gogen.Hooks{}
			gentest.SetGenHooks(t, gh)
			gh.OperatorsRoute = func(table string, r *compile.Rule) { flowed[table+"/"+r.Name]++ }
			if _, err := gentest.GenerateCode(t, g, gen.GenOptions{Start: "main", Package: "gen", Recognize: true}); err != nil {
				t.Fatal(err)
			}
			for _, table := range []string{"rules", "recRules"} {
				if flowed[table+"/"+c.Rule] == 0 {
					t.Errorf("no line of %s is printed from Flow IR in the %s table (%v)", c.Rule, table, flowed)
				}
			}
		})
	}
}
