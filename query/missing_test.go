package query_test

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/query"
	"github.com/ornew/zeroparse/schema"
)

const missingSchemaGrammar = `type P = { M missing; E error; O *missing; L []missing }
rule main = "(" semi ")" $$ => P{}
rule semi = ";" #missing`

func missingSchema(t *testing.T) *schema.Output {
	t.Helper()
	return mustSchema(t, mustCompile(t, missingSchemaGrammar), zeroparse.WithAllSchemaRules())
}

// Patterns of errors apply to the values of the shape missing, and others do not.
func TestStructuralQueryMissingShape(t *testing.T) {
	s := missingSchema(t)
	field := func(pattern query.Pattern) query.Pattern {
		return query.Pattern{Type: "P", Fields: []query.FieldPattern{{Name: "M", Pattern: pattern}}}
	}
	for _, good := range []query.Pattern{
		field(query.Pattern{Category: "error"}),
		field(query.Pattern{Type: "error"}),
		field(query.Pattern{Category: "error", Fields: []query.FieldPattern{{Name: "message", Pattern: query.Pattern{Category: "string"}}}}),
	} {
		if _, err := query.Compile(good, s); err != nil {
			t.Errorf("rejected %+v: %v", good, err)
		}
	}
	for _, bad := range []query.Pattern{
		field(query.Pattern{Category: "string"}),
		field(query.Pattern{Category: "nil"}),
		field(query.Pattern{Type: "P"}),
		field(query.Pattern{Category: "error", Fields: []query.FieldPattern{{Name: "other"}}}),
	} {
		if _, err := query.Compile(bad, s); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	_, err := query.Compile(field(query.Pattern{Category: "string"}), s)
	if err == nil || !strings.Contains(err.Error(), "value is missing, not string") {
		t.Errorf("reason: %v", err)
	}
}
