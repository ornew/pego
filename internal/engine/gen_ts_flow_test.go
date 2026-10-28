package engine

import (
	"strings"
	"testing"
)

// The lean value-free rules of the TypeScript parser are printed from Flow IR, as labeled blocks.
func TestGeneratedTSPrintsLeanRulesFromFlow(t *testing.T) {
	src := `rule main = word+
rule word = [a-z]+ ("-" [a-z]+)*`
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, disabled := range []bool{false, true} {
		files, err := GenerateTS(g, GenOptions{Start: "main", Recognize: true, disableTSFlow: disabled})
		if err != nil {
			t.Fatal(err)
		}
		var parser string
		for _, f := range files {
			if strings.HasSuffix(f.Path, "parser.ts") {
				parser = string(f.Content)
			}
		}
		if got := strings.Contains(parser, "L_fail"); got == disabled {
			t.Errorf("disableTSFlow=%v: parser prints labels = %v", disabled, got)
		}
	}
}
