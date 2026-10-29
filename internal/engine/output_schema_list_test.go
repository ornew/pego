package engine

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ornew/pego/internal/compile"
	ischema "github.com/ornew/pego/internal/schema"
	"github.com/ornew/pego/schema"
)

func TestOutputSchemaListQueries(t *testing.T) {
	token := ischema.QueryPattern{Type: "Token", Fields: []ischema.QueryFieldPattern{{Name: "Text", Pattern: ischema.QueryPattern{Category: "string"}}}, Capture: "value"}
	nodeToken := ischema.QueryPattern{Category: "node", Value: &ischema.QueryPattern{Category: "string"}, Capture: "value"}
	list := func(child ischema.QueryPattern) ischema.QueryPattern {
		return ischema.QueryPattern{Category: "list", Elements: []ischema.QueryPattern{child}}
	}
	listNode := func(child ischema.QueryPattern) ischema.QueryPattern {
		return ischema.QueryPattern{Category: "node", Elements: []ischema.QueryPattern{child}}
	}
	holder := func(value ischema.QueryPattern) ischema.QueryPattern {
		return ischema.QueryPattern{Type: "Holder", Fields: []ischema.QueryFieldPattern{{Name: "Values", Pattern: value}}}
	}
	for _, tc := range []struct {
		name, source, input string
		pattern             ischema.QueryPattern
	}{
		{"repetition", `rule main = "x"*`, "x", listNode(nodeToken)},
		{"empty repetition", `rule main = "x"*`, "", ischema.QueryPattern{Category: "node", Value: &ischema.QueryPattern{Category: "list"}, Capture: "value"}},
		{"typed field", `type Token = { Text string }
type Holder = { Values []Token }
rule main = "x" => Holder{Values: []Token{Token{Text: "one"}}}`, "x", holder(list(token))},
		{"empty typed field", `type Token = { Text string }
type Holder = { Values []Token }
rule main = "x" => Holder{Values: []Token{}}`, "x", holder(ischema.QueryPattern{Category: "list", Capture: "value"})},
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
			sch, err := outputSchemaPrototype(p, "main", false)
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
			q, err := ischema.CompileStructuralQuery(tc.pattern, &decoded)
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
						matches, err := q.Matches(n, ischema.QueryOptions{})
						if err != nil || len(matches) != 1 || len(matches[0].Captures) != 1 {
							t.Fatalf("list query: %+v, %v", matches, err)
						}
					})
				}
			}
		})
	}
}
