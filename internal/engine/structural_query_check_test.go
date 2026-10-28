package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/value"
)

const queryShapeGrammar = `type Int = { Text string }
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
`

func queryShapeSchema(t *testing.T) *OutputSchema {
	t.Helper()
	s, err := mustCompile(t, queryShapeGrammar).OutputSchemaPrototype("main", false)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func field(name string, p QueryPattern) QueryFieldPattern {
	return QueryFieldPattern{Name: name, Pattern: p}
}

// textStruct matches a value of the text struct type name, such as Int or Name.
func textStruct(name string) QueryPattern {
	return QueryPattern{Type: name, Fields: []QueryFieldPattern{{Name: "Text", Pattern: QueryPattern{Category: "string"}}}}
}

// TestStructuralQueryNestedShapes checks that a pattern compiles if, and only if, some value of the
// schema can match it where it is applied, through unions, optional values, lists and nesting.
func TestStructuralQueryNestedShapes(t *testing.T) {
	s := queryShapeSchema(t)
	prepared, err := PrepareQuerySchema(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		pattern QueryPattern
		err     string // "" if the pattern compiles
	}{
		{"union alternative", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Value", textStruct("Int"))}}, ""},
		{"nested union alternative", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Value", QueryPattern{Fields: []QueryFieldPattern{field("Key", textStruct("Name"))}})}}, ""},
		{"nil alternative", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Value", QueryPattern{Category: "nil"})}}, ""},
		{"through optional", QueryPattern{Type: "Box", Fields: []QueryFieldPattern{field("Inner", QueryPattern{Fields: []QueryFieldPattern{field("Items", QueryPattern{})}})}}, ""},
		{"list element", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Items", QueryPattern{Category: "list", Elements: []QueryPattern{textStruct("Int")}})}}, ""},
		{"deep", QueryPattern{Type: "Box", Fields: []QueryFieldPattern{field("Inner", QueryPattern{Fields: []QueryFieldPattern{field("Value", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Value", textStruct("Int"))}})}})}}, ""},
		{"untyped root", QueryPattern{Fields: []QueryFieldPattern{field("Anything", QueryPattern{Category: "int"})}}, ""},
		{"rule result elements", QueryPattern{Category: "node", Elements: []QueryPattern{{Type: "Box"}}}, ""},

		{"type outside union", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Value", QueryPattern{Type: "Box"})}}, "alternatives matches"},
		{"field no alternative has", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Value", QueryPattern{Fields: []QueryFieldPattern{field("Inner", QueryPattern{})}})}}, "alternatives matches"},
		{"scalar category in node union", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Value", QueryPattern{Category: "string"})}}, "alternatives matches"},
		{"wrong type", QueryPattern{Type: "Box", Fields: []QueryFieldPattern{field("Inner", QueryPattern{Type: "Box"})}}, "value is a struct Pair, not Box"},
		{"nested wrong field", QueryPattern{Type: "Box", Fields: []QueryFieldPattern{field("Inner", QueryPattern{Fields: []QueryFieldPattern{field("Inner", QueryPattern{})}})}}, "a struct Pair has no field Inner"},
		{"text struct has no such field", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Key", QueryPattern{Fields: []QueryFieldPattern{field("X", QueryPattern{})}})}}, "a struct Name has no field X"},
		{"text struct is not a node", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Key", QueryPattern{Value: &QueryPattern{Category: "int"}})}}, "value is a struct Name, not a node"},
		{"list element type", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Items", QueryPattern{Elements: []QueryPattern{{Category: "int"}}})}}, "element 0"},
		{"terminal has no elements", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Key", QueryPattern{Elements: []QueryPattern{{}}})}}, "has no elements"},
		{"named type below unknown", QueryPattern{Fields: []QueryFieldPattern{field("X", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Key", QueryPattern{Category: "int"})}})}}, "field Key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CompileStructuralQuery(tc.pattern, s)
			switch {
			case tc.err == "" && err != nil:
				t.Fatalf("rejected: %v", err)
			case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
				t.Fatalf("error %v, want %q", err, tc.err)
			}
			if _, perr := prepared.CompileStructuralQuery(tc.pattern); fmt.Sprint(perr) != fmt.Sprint(err) {
				t.Fatalf("prepared schema: error %v, want %v", perr, err)
			}
		})
	}
}

// TestStructuralQueryNestedShapesAreSound checks that patterns describing actual parse results
// compile against the grammar's schema: the check must never reject a pattern that matches.
func TestStructuralQueryNestedShapesAreSound(t *testing.T) {
	cases := append(genCorpus(t), genCase{"query shapes", queryShapeGrammar, []string{"[]", "[a=1]", "[a=b12]", "[a=(b=_3)4]", "[a=_]"}})
	cases = append(cases, queryRecoveryCases...)
	cases = append(cases, constructorCases()...)
	cases = append(cases, fieldCheckCases()...)
	cases = append(cases, resultCheckCases()...)
	schemas, patterns, recovered := 0, 0, 0
	for _, c := range cases {
		g, err := parseTest(c.src)
		if err != nil {
			continue
		}
		prog, err := Compile(g, Options{SourceInfo: true})
		if err != nil {
			continue
		}
		start := g.Rules()[0].Name
		s, err := prog.OutputSchemaPrototype(start, false)
		if err != nil {
			continue
		}
		schemas++
		prepared, err := PrepareQuerySchema(s)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, in := range c.inputs {
			// Recovered parses report errors with their trees.
			root, _ := prog.ParseWith(start, in, ParseOptions{MissingTokens: 3})
			if root == nil {
				continue
			}
			err = visitQueryGraph(root, &queryBudget{}, func(v queryVisit) error {
				n, ok := v.value.(*value.Node)
				if !ok || n == nil {
					return nil
				}
				p := describeQueryValue(n, 3)
				patterns++
				if value.ShapeOf(n) == value.ShapeError {
					recovered++
				}
				q, err := CompileStructuralQuery(p, s)
				if err != nil {
					t.Errorf("%s %q: pattern of %s rejected: %v", c.name, in, n, err)
					return nil
				}
				if m, err := q.Matches(n, QueryOptions{}); err != nil || len(m) == 0 {
					t.Errorf("%s %q: pattern of %s does not match it: %v", c.name, in, n, err)
				}
				if _, err := prepared.CompileStructuralQuery(p); err != nil {
					t.Errorf("%s %q: pattern of %s rejected with the prepared schema: %v", c.name, in, n, err)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if schemas < 20 || patterns < 1000 || recovered < 4 {
		t.Fatalf("checked %d patterns of %d schemas, %d of recovery nodes", patterns, schemas, recovered)
	}
}

// queryRecoveryCases put Error and Missing nodes where declared types expect other nodes.
var queryRecoveryCases = []genCase{
	{"query recovery in terminal field", `type Num = { Text string }
type P = { V Num | error; W Num }
rule main = "(" v:(num #recover(skip=[^),]+)) "," w:num ")" $$ => P{V: $v, W: $w}
rule num Num = t:([0-9]) #atomic => Num{Text: text($t)}`, []string{"(1,2)", "(x,2)", "(1,y)"}},
	{"query recovery in list field", `type Num = { Text string }
type P = { L []Num }
rule main = "(" l:(nums #recover(skip=[^)]+)) ")" $$ => P{L: $l}
rule nums []Num = (num #recover(skip=[a-z]))+
rule num Num = t:([0-9]) #atomic => Num{Text: text($t)}`, []string{"(12)", "(x)", "(1a2)"}},
	{"query missing token", `type P = { S node[string] | error }
rule main = "(" s:semi ")" $$ => P{S: $s}
rule semi = ";" #missing`, []string{"(;)", "()"}},
}

// TestStructuralQueryRecoveryNodes checks patterns of recovery nodes where declared types are
// expected, and of their fields.
func TestStructuralQueryRecoveryNodes(t *testing.T) {
	g, err := parseTest(queryRecoveryCases[0].src)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := Compile(g, Options{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := prog.OutputSchemaPrototype("main", false)
	if err != nil {
		t.Fatal(err)
	}
	ok := QueryPattern{Type: "P", Fields: []QueryFieldPattern{field("V", QueryPattern{Type: "error", Fields: []QueryFieldPattern{field("message", QueryPattern{Category: "string"})}})}}
	q, err := CompileStructuralQuery(ok, s)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := prog.ParseWith("main", "(x,2)", ParseOptions{})
	if m, err := q.Matches(root, QueryOptions{}); err != nil || len(m) != 1 {
		t.Fatalf("matches %v %v", m, err)
	}
	for _, good := range []QueryPattern{
		{Type: "error", Fields: []QueryFieldPattern{field("message", QueryPattern{})}},
		{Type: "P", Fields: []QueryFieldPattern{field("V", QueryPattern{Category: "error", Fields: []QueryFieldPattern{field("expected", QueryPattern{Category: "string"})}})}},
	} {
		if _, err := CompileStructuralQuery(good, s); err != nil {
			t.Errorf("rejected %+v: %v", good, err)
		}
	}
	for _, bad := range []QueryPattern{
		{Type: "P", Fields: []QueryFieldPattern{field("V", QueryPattern{Type: "error", Fields: []QueryFieldPattern{field("message", QueryPattern{Category: "int"})}})}},
		{Type: "P", Fields: []QueryFieldPattern{field("V", QueryPattern{Type: "error", Fields: []QueryFieldPattern{field("other", QueryPattern{})}})}},
		{Type: "P", Fields: []QueryFieldPattern{field("V", QueryPattern{Category: "error", Fields: []QueryFieldPattern{field("other", QueryPattern{})}})}},
		{Type: "P", Fields: []QueryFieldPattern{field("V", QueryPattern{Elements: []QueryPattern{{}}})}},
		{Type: "P", Fields: []QueryFieldPattern{field("W", QueryPattern{Type: "error", Rule: "num"})}},
	} {
		if _, err := CompileStructuralQuery(bad, s); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

// TestStructuralQueryErrorSize checks that the reason a deeply nested pattern over unions matches
// nothing stays small.
func TestStructuralQueryErrorSize(t *testing.T) {
	src := "type Num = { Text string }\ntype E ~ A | B | C | D | Num\n"
	for _, n := range []string{"A", "B", "C", "D"} {
		src += "type " + n + " = { X E }\n"
	}
	src += "rule main E = e\nrule e E = n:[0-9] => Num{Text: text($n)}\n"
	s, err := mustCompile(t, src).OutputSchemaPrototype("main", true)
	if err != nil {
		t.Fatal(err)
	}
	p := QueryPattern{Category: "string"}
	for range 16 {
		p = QueryPattern{Fields: []QueryFieldPattern{field("X", p)}}
	}
	p.Type = "A"
	_, err = CompileStructuralQuery(p, s)
	if err == nil || len(err.Error()) > 4096 {
		t.Fatalf("error of %d bytes: %.200v", len(fmt.Sprint(err)), err)
	}
}

// describeQueryValue returns a pattern that matches v, describing it to the given depth.
func describeQueryValue(v any, depth int) QueryPattern {
	v = value.UnboxValue(v)
	category, _ := queryCategory(v)
	p := QueryPattern{Category: category}
	n, ok := v.(*value.Node)
	if !ok || n == nil || depth == 0 {
		return p
	}
	switch category {
	case "struct":
		p.Type = n.Type()
	case "error":
		p.Type = "error"
	case "node":
		p.Rule = n.Rule()
		val := describeQueryValue(queryNodeValue(n), depth-1)
		val.Elements = nil
		p.Value = &val
	}
	if category == "struct" || category == "error" {
		for _, f := range value.D(n).Fields {
			p.Fields = append(p.Fields, QueryFieldPattern{Name: f.Name, Pattern: describeQueryValue(f.Value, depth-1)})
		}
	}
	if queryHasElements(n) {
		for i, c := range value.D(n).Children {
			if i == 2 {
				break
			}
			p.Elements = append(p.Elements, describeQueryValue(c, depth-1))
		}
	}
	return p
}
