package engine

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/schema"
)

func TestOutputSchemaListQueries(t *testing.T) {
	token := schema.QueryPattern{Type: "Token", Fields: []schema.QueryFieldPattern{{Name: "Text", Pattern: schema.QueryPattern{Category: "string"}}}, Capture: "value"}
	nodeToken := schema.QueryPattern{Category: "node", Value: &schema.QueryPattern{Category: "string"}, Capture: "value"}
	list := func(child schema.QueryPattern) schema.QueryPattern {
		return schema.QueryPattern{Category: "list", Elements: []schema.QueryPattern{child}}
	}
	listNode := func(child schema.QueryPattern) schema.QueryPattern {
		return schema.QueryPattern{Category: "node", Elements: []schema.QueryPattern{child}}
	}
	holder := func(value schema.QueryPattern) schema.QueryPattern {
		return schema.QueryPattern{Type: "Holder", Fields: []schema.QueryFieldPattern{{Name: "Values", Pattern: value}}}
	}
	for _, tc := range []struct {
		name, source, input string
		pattern             schema.QueryPattern
	}{
		{"repetition", `rule main = "x"*`, "x", listNode(nodeToken)},
		{"empty repetition", `rule main = "x"*`, "", schema.QueryPattern{Category: "node", Value: &schema.QueryPattern{Category: "list"}, Capture: "value"}},
		{"typed field", `type Token = { Text string }
type Holder = { Values []Token }
rule main = "x" => Holder{Values: []Token{Token{Text: "one"}}}`, "x", holder(list(token))},
		{"empty typed field", `type Token = { Text string }
type Holder = { Values []Token }
rule main = "x" => Holder{Values: []Token{}}`, "x", holder(schema.QueryPattern{Category: "list", Capture: "value"})},
		{"aliased field", `type Token = { Text string }
type Tokens ~ []Token
type Holder = { Values Tokens }
rule main = "x" => Holder{Values: []Token{Token{Text: "one"}}}`, "x", holder(list(token))},
		{"nested typed field", `type Token = { Text string }
type Holder = { Values [][]Token }
rule main = "x" => Holder{Values: [][]Token{[]Token{Token{Text: "one"}}}}`, "x", holder(list(list(token)))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := parseTest(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			p, err := Compile(g, compile.Options{})
			if err != nil {
				t.Fatal(err)
			}
			sch, err := p.OutputSchemaPrototype("main", false)
			if err != nil {
				t.Fatal(err)
			}
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
			q, err := schema.CompileStructuralQuery(tc.pattern, &decoded)
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
						matches, err := q.Matches(n, schema.QueryOptions{})
						if err != nil || len(matches) != 1 || len(matches[0].Captures) != 1 {
							t.Fatalf("list query: %+v, %v", matches, err)
						}
					})
				}
			}
		})
	}
}
