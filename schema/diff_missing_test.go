package schema_test

import (
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/schema"
)

// A field that changes between error and missing is a change of its type.
func TestCompareMissingAndError(t *testing.T) {
	compile := func(typ string) *schema.Output {
		t.Helper()
		p, err := zp.CompileSource("type P = { S "+typ+" }\n@start\nrule main = \"(\" semi \")\" $$ => P{}\nrule semi = \";\" #missing", "main")
		if err != nil {
			t.Fatal(err)
		}
		s, err := p.OutputSchema()
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	missing, errored := compile("missing"), compile("error")
	same, err := schema.Compare(missing, compile("missing"))
	if err != nil || len(same.Types) != 0 || len(same.Rules) != 0 {
		t.Fatalf("equal schemas: %+v %v", same, err)
	}
	diff, err := schema.Compare(missing, errored)
	if err != nil || len(diff.Types) != 1 || diff.Types[0].ID != "declared/P" || diff.Types[0].Kind != schema.Changed {
		t.Fatalf("diff: %+v %v", diff, err)
	}
	if got := diff.Types[0].Before.Shape.Fields[0].Shape.Kind; got != "missing" {
		t.Errorf("before: %s", got)
	}
	if got := diff.Types[0].After.Shape.Fields[0].Shape.Kind; got != "error" {
		t.Errorf("after: %s", got)
	}
}
