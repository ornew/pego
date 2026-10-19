package engine

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func TestOutputSchemaListQueries(t *testing.T) {
	token := QueryPattern{Type: "Token", Fields: []QueryFieldPattern{{Name: "Text", Pattern: QueryPattern{Category: "string"}}}, Capture: "value"}
	nodeToken := QueryPattern{Category: "node", Value: &QueryPattern{Category: "string"}, Capture: "value"}
	list := func(child QueryPattern) QueryPattern {
		return QueryPattern{Category: "list", Elements: []QueryPattern{child}}
	}
	listNode := func(child QueryPattern) QueryPattern {
		return QueryPattern{Category: "node", Elements: []QueryPattern{child}}
	}
	holder := func(value QueryPattern) QueryPattern {
		return QueryPattern{Type: "Holder", Fields: []QueryFieldPattern{{Name: "Values", Pattern: value}}}
	}
	for _, tc := range []struct {
		name, source, input string
		pattern             QueryPattern
	}{
		{"repetition", `rule main = "x"*`, "x", listNode(nodeToken)},
		{"empty repetition", `rule main = "x"*`, "", QueryPattern{Category: "node", Value: &QueryPattern{Category: "list"}, Capture: "value"}},
		{"typed field", `type Token = { Text string }
type Holder = { Values []Token }
rule main = "x" => Holder{Values: []Token{Token{Text: "one"}}}`, "x", holder(list(token))},
		{"empty typed field", `type Token = { Text string }
type Holder = { Values []Token }
rule main = "x" => Holder{Values: []Token{}}`, "x", holder(QueryPattern{Category: "list", Capture: "value"})},
		{"aliased field", `type Token = { Text string }
type Tokens ~ []Token
type Holder = { Values Tokens }
rule main = "x" => Holder{Values: []Token{Token{Text: "one"}}}`, "x", holder(list(token))},
		{"nested typed field", `type Token = { Text string }
type Holder = { Values [][]Token }
rule main = "x" => Holder{Values: [][]Token{[]Token{Token{Text: "one"}}}}`, "x", holder(list(list(token)))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := syntax.Parse(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			p, err := Compile(g, Options{})
			if err != nil {
				t.Fatal(err)
			}
			schema, err := p.OutputSchemaPrototype("main", false)
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(); err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(schema)
			if err != nil {
				t.Fatal(err)
			}
			var decoded OutputSchema
			if err := json.Unmarshal(data, &decoded); err != nil || !reflect.DeepEqual(schema, &decoded) {
				t.Fatalf("schema round trip: %v", err)
			}
			q, err := CompileStructuralQuery(tc.pattern, &decoded)
			if err != nil {
				t.Fatal(err)
			}
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				for _, unit := range []PositionUnit{CodePoints, Bytes} {
					t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
						n, err := p.ParseWith("main", tc.input, ParseOptions{Backend: backend, Unit: unit})
						if err != nil {
							t.Fatal(err)
						}
						matches, err := q.Matches(n, QueryOptions{})
						if err != nil || len(matches) != 1 || len(matches[0].Captures) != 1 {
							t.Fatalf("list query: %+v, %v", matches, err)
						}
					})
				}
			}
		})
	}
}
