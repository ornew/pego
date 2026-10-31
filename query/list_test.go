package query_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/query"
	"github.com/ornew/zeroparse/schema"
)

func TestOutputSchemaListQueries(t *testing.T) {
	token := query.Pattern{Type: "Token", Fields: []query.FieldPattern{{Name: "Text", Pattern: query.Pattern{Category: "string"}}}, Capture: "value"}
	nodeToken := query.Pattern{Category: "node", Value: &query.Pattern{Category: "string"}, Capture: "value"}
	list := func(child query.Pattern) query.Pattern {
		return query.Pattern{Category: "list", Elements: []query.Pattern{child}}
	}
	listNode := func(child query.Pattern) query.Pattern {
		return query.Pattern{Category: "node", Elements: []query.Pattern{child}}
	}
	holder := func(value query.Pattern) query.Pattern {
		return query.Pattern{Type: "Holder", Fields: []query.FieldPattern{{Name: "Values", Pattern: value}}}
	}
	for _, tc := range []struct {
		name, source, input string
		pattern             query.Pattern
	}{
		{"repetition", `rule main = "x"*`, "x", listNode(nodeToken)},
		{"empty repetition", `rule main = "x"*`, "", query.Pattern{Category: "node", Value: &query.Pattern{Category: "list"}, Capture: "value"}},
		{"typed field", `type Token = { Text string }
type Holder = { Values []Token }
rule main = "x" => Holder{Values: []Token{Token{Text: "one"}}}`, "x", holder(list(token))},
		{"empty typed field", `type Token = { Text string }
type Holder = { Values []Token }
rule main = "x" => Holder{Values: []Token{}}`, "x", holder(query.Pattern{Category: "list", Capture: "value"})},
		{"aliased field", `type Token = { Text string }
type Tokens ~ []Token
type Holder = { Values Tokens }
rule main = "x" => Holder{Values: []Token{Token{Text: "one"}}}`, "x", holder(list(token))},
		{"nested typed field", `type Token = { Text string }
type Holder = { Values [][]Token }
rule main = "x" => Holder{Values: [][]Token{[]Token{Token{Text: "one"}}}}`, "x", holder(list(list(token)))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := mustCompile(t, tc.source)
			sch := mustSchema(t, p)
			if err := sch.Validate(); err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(sch)
			if err != nil {
				t.Fatal(err)
			}
			var decoded schema.Output
			if err := json.Unmarshal(data, &decoded); err != nil || !reflect.DeepEqual(sch, &decoded) {
				t.Fatalf("schema round trip: %v", err)
			}
			q, err := query.Compile(tc.pattern, &decoded)
			if err != nil {
				t.Fatal(err)
			}
			for _, backend := range []zeroparse.Backend{zeroparse.Closure, zeroparse.Bytecode, zeroparse.BytecodeIterative} {
				for _, unit := range []zeroparse.PositionUnit{zeroparse.CodePoints, zeroparse.Bytes} {
					t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
						n, err := p.Parse(tc.input, zeroparse.WithBackend(backend), zeroparse.WithUnit(unit))
						if err != nil {
							t.Fatal(err)
						}
						matches, err := q.Matches(n)
						if err != nil || len(matches) != 1 || len(matches[0].Captures) != 1 {
							t.Fatalf("list query: %+v, %v", matches, err)
						}
					})
				}
			}
		})
	}
}
