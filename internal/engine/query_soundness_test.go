package engine_test

import (
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/starttest"
	"github.com/ornew/pego/internal/value"
	"github.com/ornew/pego/query"
)

// queryShapeCase nests unions, optional values and lists of declared types.
var queryShapeCase = gentest.Case{Name: "query shapes", Src: `type Int = { Text string }
type Name = { Text string }
type Pair = { Key Name; Value *(Int | Name | Pair); Items []Int }
type Box = { Inner *Pair }
rule main = b:box $$ => $b
rule box Box = "[" p:pair? "]" => Box{Inner: $p}
rule pair Pair = k:name "=" v:value i:int* => Pair{Key: $k, Value: $v, Items: $i.Value}
rule value *(Int | Name | Pair) = int / name / nested / none
rule none *Int = "_" => nil
rule nested Pair = "(" p:pair ")" => $p
rule int Int = t:([0-9]) #atomic => Int{Text: text($t)}
rule name Name = t:([a-z]) #atomic => Name{Text: text($t)}
`, Inputs: []string{"[]", "[a=1]", "[a=b12]", "[a=(b=_3)4]", "[a=_]"}}

// queryRecoveryCases put Error and Missing nodes where declared types expect other nodes.
var queryRecoveryCases = []gentest.Case{
	{Name: "query recovery in terminal field", Src: `type Num = { Text string }
type P = { V Num | error; W Num }
rule main = "(" v:(num #recover(skip=[^),]+)) "," w:num ")" $$ => P{V: $v, W: $w}
rule num Num = t:([0-9]) #atomic => Num{Text: text($t)}`, Inputs: []string{"(1,2)", "(x,2)", "(1,y)"}},
	{Name: "query recovery in list field", Src: `type Num = { Text string }
type P = { L []Num }
rule main = "(" l:(nums #recover(skip=[^)]+)) ")" $$ => P{L: $l}
rule nums []Num = (num #recover(skip=[a-z]))+
rule num Num = t:([0-9]) #atomic => Num{Text: text($t)}`, Inputs: []string{"(12)", "(x)", "(1a2)"}},
	{Name: "query missing token", Src: `type P = { S node[string] | error }
rule main = "(" s:semi ")" $$ => P{S: $s}
rule semi = ";" #missing`, Inputs: []string{"(;)", "()"}},
}

// TestStructuralQueryNestedShapesAreSound checks that patterns describing actual parse results
// compile against the grammar's schema: the check must never reject a pattern that matches.
func TestStructuralQueryNestedShapesAreSound(t *testing.T) {
	t.Parallel()
	cases := append(gentest.SchemaCorpus(t), queryShapeCase)
	cases = append(cases, queryRecoveryCases...)
	// every matches each candidate value: every node, struct, tuple, list and error value.
	every, err := query.Compile(query.Pattern{Capture: "value"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	schemas, patterns, recovered := 0, 0, 0
	for _, c := range cases {
		g, err := pego.ParseGrammar(c.Src)
		if err != nil {
			continue
		}
		start := g.Rules()[0].Name
		p, err := pego.Compile(starttest.MarkAll(g), start, pego.WithSourceInfo())
		if err != nil {
			continue
		}
		s, err := p.OutputSchema()
		if err != nil {
			continue
		}
		schemas++
		prepared, err := query.Prepare(s)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		for _, in := range c.Inputs {
			// Recovered parses report errors with their trees.
			root, _ := p.Parse(in, pego.WithMissingTokens(3))
			if root == nil {
				continue
			}
			candidates, err := every.Matches(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range candidates {
				n := m.Captures[0].Value.(*value.Node)
				pattern := describeQueryValue(n, 3)
				patterns++
				if value.ShapeOf(n) == value.ShapeError {
					recovered++
				}
				q, err := query.Compile(pattern, s)
				if err != nil {
					t.Errorf("%s %q: pattern of %s rejected: %v", c.Name, in, n, err)
					continue
				}
				if m, err := q.Matches(n); err != nil || len(m) == 0 {
					t.Errorf("%s %q: pattern of %s does not match it: %v", c.Name, in, n, err)
				}
				if _, err := prepared.Compile(pattern); err != nil {
					t.Errorf("%s %q: pattern of %s rejected with the prepared schema: %v", c.Name, in, n, err)
				}
			}
		}
	}
	if schemas < 20 || patterns < 1000 || recovered < 4 {
		t.Fatalf("checked %d patterns of %d schemas, %d of recovery nodes", patterns, schemas, recovered)
	}
}

// describeQueryValue returns a pattern that matches v, describing it to the given depth.
func describeQueryValue(v any, depth int) query.Pattern {
	v = value.UnboxValue(v)
	n, _ := v.(*value.Node)
	p := query.Pattern{Category: queryCategory(v)}
	if n == nil || depth == 0 {
		return p
	}
	switch p.Category {
	case "struct":
		p.Type = n.Type()
	case "error":
		p.Type = "error"
	case "node":
		p.Rule = n.Rule()
		// The value of a node is its text, or a tuple or list sharing its elements.
		val := query.Pattern{Category: "string"}
		switch value.ShapeOf(n) {
		case value.ShapeTuple:
			val.Category = "tuple"
		case value.ShapeList:
			val.Category = "list"
		}
		p.Value = &val
	}
	if p.Category == "struct" || p.Category == "error" {
		for _, f := range value.D(n).Fields {
			p.Fields = append(p.Fields, query.FieldPattern{Name: f.Name, Pattern: describeQueryValue(f.Value, depth-1)})
		}
	}
	switch value.ShapeOf(n) {
	case value.ShapeTuple, value.ShapeList, value.ShapeTupleValue, value.ShapeListValue:
		for i, c := range value.D(n).Children {
			if i == 2 {
				break
			}
			p.Elements = append(p.Elements, describeQueryValue(c, depth-1))
		}
	}
	return p
}

// queryCategory returns the query category of an unboxed value.
func queryCategory(v any) string {
	switch v := v.(type) {
	case *value.Node:
		if v == nil {
			return "nil"
		}
		switch value.ShapeOf(v) {
		case value.ShapeString, value.ShapeTuple, value.ShapeList:
			return "node"
		case value.ShapeError:
			return "error"
		case value.ShapeTupleValue:
			return "tuple"
		case value.ShapeListValue:
			return "list"
		case value.ShapeStruct:
			return "struct"
		}
	case int:
		return "int"
	case string:
		return "string"
	case bool:
		return "bool"
	case nil:
		return "nil"
	}
	return ""
}
