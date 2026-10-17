package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

const queryShapeGrammar = `type Int terminal
type Name terminal
type Pair = { Key Name; Value *(Int | Name | Pair); Items []Int }
type Box = { Inner *Pair }
rule main = b:box $$ => $b
rule box Box = "[" p:pair? "]" => Box{Inner: $p}
rule pair Pair = k:name "=" v:value i:int* => Pair{Key: $k, Value: $v, Items: $i}
rule value *(Int | Name | Pair) = int / name / nested / none
rule none *Int = "_" => nil
rule nested Pair = "(" p:pair ")" => $p
rule int Int = [0-9]
rule name Name = [a-z]
`

func queryShapeSchema(t *testing.T) *OutputSchema {
	t.Helper()
	s, err := compile(t, queryShapeGrammar).OutputSchemaPrototype("main", false)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func field(name string, p QueryPattern) QueryFieldPattern {
	return QueryFieldPattern{Name: name, Pattern: p}
}

// TestStructuralQueryNestedShapes checks that a pattern compiles if, and only if, some value of the
// schema can match it where it is applied, through unions, optional values, lists and nesting.
func TestStructuralQueryNestedShapes(t *testing.T) {
	s := queryShapeSchema(t)
	for _, tc := range []struct {
		name    string
		pattern QueryPattern
		err     string // "" if the pattern compiles
	}{
		{"union alternative", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Value", QueryPattern{Type: "Int"})}}, ""},
		{"nested union alternative", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Value", QueryPattern{Fields: []QueryFieldPattern{field("Key", QueryPattern{Type: "Name"})}})}}, ""},
		{"nil alternative", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Value", QueryPattern{Category: "nil"})}}, ""},
		{"through optional", QueryPattern{Type: "Box", Fields: []QueryFieldPattern{field("Inner", QueryPattern{Fields: []QueryFieldPattern{field("Items", QueryPattern{})}})}}, ""},
		{"list element", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Items", QueryPattern{Type: "List", Children: []QueryPattern{{Type: "Int"}}})}}, ""},
		{"deep", QueryPattern{Type: "Box", Fields: []QueryFieldPattern{field("Inner", QueryPattern{Fields: []QueryFieldPattern{field("Value", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Value", QueryPattern{Type: "Name"})}})}})}}, ""},
		{"untyped root", QueryPattern{Fields: []QueryFieldPattern{field("Anything", QueryPattern{Category: "int"})}}, ""},

		{"type outside union", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Value", QueryPattern{Type: "Box"})}}, "alternatives matches"},
		{"field no alternative has", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Value", QueryPattern{Fields: []QueryFieldPattern{field("Inner", QueryPattern{})}})}}, "alternatives matches"},
		{"scalar category in node union", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Value", QueryPattern{Category: "string"})}}, "alternatives matches"},
		{"wrong type", QueryPattern{Type: "Box", Fields: []QueryFieldPattern{field("Inner", QueryPattern{Type: "Int"})}}, "value is Pair, not Int"},
		{"nested wrong field", QueryPattern{Type: "Box", Fields: []QueryFieldPattern{field("Inner", QueryPattern{Fields: []QueryFieldPattern{field("Inner", QueryPattern{})}})}}, "Pair has no field Inner"},
		{"terminal has no fields", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Key", QueryPattern{Fields: []QueryFieldPattern{field("X", QueryPattern{})}})}}, "Name has no field X"},
		{"list element type", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Items", QueryPattern{Children: []QueryPattern{{Type: "Name"}}})}}, "child 0 of List"},
		{"named type below unknown", QueryPattern{Fields: []QueryFieldPattern{field("X", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Key", QueryPattern{Type: "Int"})}})}}, "field Pair.Key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CompileStructuralQuery(tc.pattern, s)
			switch {
			case tc.err == "" && err != nil:
				t.Fatalf("rejected: %v", err)
			case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
				t.Fatalf("error %v, want %q", err, tc.err)
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
		g, err := syntax.Parse(c.src)
		if err != nil {
			continue
		}
		prog, err := Compile(g, Options{})
		if err != nil {
			continue
		}
		start := g.Rules()[0].Name
		s, err := prog.OutputSchemaPrototype(start, false)
		if err != nil {
			continue
		}
		schemas++
		for _, in := range c.inputs {
			// Recovered parses report errors with their trees.
			root, _ := prog.ParseWith(start, in, ParseOptions{MissingTokens: 3})
			if root == nil {
				continue
			}
			err = visitQueryGraph(root, &queryBudget{}, func(v queryVisit) error {
				n, ok := v.value.(*Node)
				if !ok || n == nil {
					return nil
				}
				p := describeQueryValue(n, 3)
				patterns++
				if n.Type() == TypeError || n.Type() == TypeMissing {
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
	{"query recovery in terminal field", `type Num terminal
type P = { V Num; W Num }
rule main = "(" v:(num #recover(skip=[^),]+)) "," w:num ")" $$ => P{V: $v, W: $w}
rule num Num = [0-9]`, []string{"(1,2)", "(x,2)", "(1,y)"}},
	{"query recovery in list field", `type Num terminal
type P = { L []Num }
rule main = "(" l:(nums #recover(skip=[^)]+)) ")" $$ => P{L: $l}
rule nums []Num = (num #recover(skip=[a-z]))+
rule num Num = [0-9]`, []string{"(12)", "(x)", "(1a2)"}},
	{"query missing token", `type P = { S Match }
rule main = "(" s:semi ")" $$ => P{S: $s}
rule semi = ";" #missing`, []string{"(;)", "()"}},
}

// TestStructuralQueryRecoveryNodes checks patterns of recovery nodes where declared types are
// expected, and of their fields.
func TestStructuralQueryRecoveryNodes(t *testing.T) {
	g, err := syntax.Parse(queryRecoveryCases[0].src)
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
	ok := QueryPattern{Type: "P", Fields: []QueryFieldPattern{field("V", QueryPattern{Type: "Error", Fields: []QueryFieldPattern{field("message", QueryPattern{Category: "string"})}})}}
	q, err := CompileStructuralQuery(ok, s)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := prog.ParseWith("main", "(x,2)", ParseOptions{})
	if m, err := q.Matches(root, QueryOptions{}); err != nil || len(m) != 1 {
		t.Fatalf("matches %v %v", m, err)
	}
	if _, err := CompileStructuralQuery(QueryPattern{Type: "Error", Fields: []QueryFieldPattern{field("message", QueryPattern{})}}, s); err != nil {
		t.Fatalf("Error message field: %v", err)
	}
	for _, bad := range []QueryPattern{
		{Type: "P", Fields: []QueryFieldPattern{field("V", QueryPattern{Type: "Error", Fields: []QueryFieldPattern{field("message", QueryPattern{Category: "int"})}})}},
		{Type: "P", Fields: []QueryFieldPattern{field("V", QueryPattern{Type: "Error", Fields: []QueryFieldPattern{field("expected", QueryPattern{})}})}},
		{Type: "P", Fields: []QueryFieldPattern{field("V", QueryPattern{Children: []QueryPattern{{}}})}},
	} {
		if _, err := CompileStructuralQuery(bad, s); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

// TestStructuralQueryErrorSize checks that the reason a deeply nested pattern over unions matches
// nothing stays small.
func TestStructuralQueryErrorSize(t *testing.T) {
	src := "type Num terminal\ntype E ~ A | B | C | D | Num\n"
	for _, n := range []string{"A", "B", "C", "D"} {
		src += "type " + n + " = { X E }\n"
	}
	src += "rule main E = e\nrule e E = n:[0-9] => Num{text($n)}\n"
	s, err := compile(t, src).OutputSchemaPrototype("main", true)
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
	category, _ := queryCategory(v)
	p := QueryPattern{Category: category}
	n, ok := v.(*Node)
	if !ok || n == nil || depth == 0 {
		return p
	}
	p.Type, p.Rule = n.Type(), n.Rule()
	for _, f := range n.Fields {
		p.Fields = append(p.Fields, QueryFieldPattern{Name: f.Name, Pattern: describeQueryValue(f.Value, depth-1)})
	}
	for i, c := range n.Children {
		if i == 2 {
			break
		}
		p.Children = append(p.Children, describeQueryValue(c, depth-1))
	}
	return p
}
