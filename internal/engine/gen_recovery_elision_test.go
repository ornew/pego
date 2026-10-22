package engine

import (
	"strings"
	"testing"
)

// A grammar without #recover never appends to p.Recovered, so its generated parser neither saves
// the length of it nor truncates it; a grammar with #recover keeps both. The behavior of both
// forms is checked against the engine by TestGeneratedParsersMatchEngine, TestGeneratedTypes,
// TestGeneratedDocumentsMatchBatchOnCorpus, TestGeneratedUnsharedParsersMatchEngine and
// TestGeneratedConstructedFieldChecks, which fail when the rollback is dropped under recovery.
func TestGeneratedRecoveredRollbackOnlyWithRecovery(t *testing.T) {
	const plain = `rule main = (item ";")* $$
rule item = [a-z]+ #atomic / [0-9]+ #atomic
`
	const recovering = `rule main = (item ";")* $$
rule item = ([a-z]+ #atomic) #recover(skip=[^;]+ ";") / [0-9]+ #atomic
`
	for _, c := range []struct {
		name, src string
		recovers  bool
	}{{"plain", plain, false}, {"recovering", recovering, true}} {
		t.Run(c.name, func(t *testing.T) {
			g, err := parseTest(c.src)
			if err != nil {
				t.Fatal(err)
			}
			for _, opts := range []GenOptions{
				{Package: "p", Start: "main"},
				{Package: "p", Start: "main", Types: true, TypedOnly: true},
				{Package: "p", Start: "main", Recognize: true},
			} {
				code, err := generateCode(g, opts)
				if err != nil {
					t.Fatalf("%+v: %v", opts, err)
				}
				src := string(code)
				for _, saved := range []string{"len(p.Recovered)", ".Restore("} {
					if got := strings.Contains(src, saved); got != c.recovers {
						t.Errorf("%+v: generated source contains %q = %v, want %v", opts, saved, got, c.recovers)
					}
				}
			}
		})
	}
}
