package engine

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/ornew/pego/internal/compile"
	ischema "github.com/ornew/pego/internal/schema"
	"github.com/ornew/pego/internal/value"
	"github.com/ornew/pego/schema"
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

func queryShapeSchema(t *testing.T) *schema.Output {
	t.Helper()
	s, err := outputSchemaPrototype(mustCompile(t, queryShapeGrammar), "main", false)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func field(name string, p ischema.QueryPattern) ischema.QueryFieldPattern {
	return ischema.QueryFieldPattern{Name: name, Pattern: p}
}

// textStruct matches a value of the text struct type name, such as Int or Name.
func textStruct(name string) ischema.QueryPattern {
	return ischema.QueryPattern{Type: name, Fields: []ischema.QueryFieldPattern{{Name: "Text", Pattern: ischema.QueryPattern{Category: "string"}}}}
}

// TestStructuralQueryNestedShapes checks that a pattern compiles if, and only if, some value of the
// schema can match it where it is applied, through unions, optional values, lists and nesting.
func TestStructuralQueryNestedShapes(t *testing.T) {
	s := queryShapeSchema(t)
	prepared, err := ischema.PrepareQuerySchema(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		pattern ischema.QueryPattern
		err     string // "" if the pattern compiles
	}{
		{"union alternative", ischema.QueryPattern{Type: "Pair", Fields: []ischema.QueryFieldPattern{field("Value", textStruct("Int"))}}, ""},
		{"nested union alternative", ischema.QueryPattern{Type: "Pair", Fields: []ischema.QueryFieldPattern{field("Value", ischema.QueryPattern{Fields: []ischema.QueryFieldPattern{field("Key", textStruct("Name"))}})}}, ""},
		{"nil alternative", ischema.QueryPattern{Type: "Pair", Fields: []ischema.QueryFieldPattern{field("Value", ischema.QueryPattern{Category: "nil"})}}, ""},
		{"through optional", ischema.QueryPattern{Type: "Box", Fields: []ischema.QueryFieldPattern{field("Inner", ischema.QueryPattern{Fields: []ischema.QueryFieldPattern{field("Items", ischema.QueryPattern{})}})}}, ""},
		{"list element", ischema.QueryPattern{Type: "Pair", Fields: []ischema.QueryFieldPattern{field("Items", ischema.QueryPattern{Category: "list", Elements: []ischema.QueryPattern{textStruct("Int")}})}}, ""},
		{"deep", ischema.QueryPattern{Type: "Box", Fields: []ischema.QueryFieldPattern{field("Inner", ischema.QueryPattern{Fields: []ischema.QueryFieldPattern{field("Value", ischema.QueryPattern{Type: "Pair", Fields: []ischema.QueryFieldPattern{field("Value", textStruct("Int"))}})}})}}, ""},
		{"untyped root", ischema.QueryPattern{Fields: []ischema.QueryFieldPattern{field("Anything", ischema.QueryPattern{Category: "int"})}}, ""},
		{"rule result elements", ischema.QueryPattern{Category: "node", Elements: []ischema.QueryPattern{{Type: "Box"}}}, ""},

		{"type outside union", ischema.QueryPattern{Type: "Pair", Fields: []ischema.QueryFieldPattern{field("Value", ischema.QueryPattern{Type: "Box"})}}, "alternatives matches"},
		{"field no alternative has", ischema.QueryPattern{Type: "Pair", Fields: []ischema.QueryFieldPattern{field("Value", ischema.QueryPattern{Fields: []ischema.QueryFieldPattern{field("Inner", ischema.QueryPattern{})}})}}, "alternatives matches"},
		{"scalar category in node union", ischema.QueryPattern{Type: "Pair", Fields: []ischema.QueryFieldPattern{field("Value", ischema.QueryPattern{Category: "string"})}}, "alternatives matches"},
		{"wrong type", ischema.QueryPattern{Type: "Box", Fields: []ischema.QueryFieldPattern{field("Inner", ischema.QueryPattern{Type: "Box"})}}, "value is a struct Pair, not Box"},
		{"nested wrong field", ischema.QueryPattern{Type: "Box", Fields: []ischema.QueryFieldPattern{field("Inner", ischema.QueryPattern{Fields: []ischema.QueryFieldPattern{field("Inner", ischema.QueryPattern{})}})}}, "a struct Pair has no field Inner"},
		{"text struct has no such field", ischema.QueryPattern{Type: "Pair", Fields: []ischema.QueryFieldPattern{field("Key", ischema.QueryPattern{Fields: []ischema.QueryFieldPattern{field("X", ischema.QueryPattern{})}})}}, "a struct Name has no field X"},
		{"text struct is not a node", ischema.QueryPattern{Type: "Pair", Fields: []ischema.QueryFieldPattern{field("Key", ischema.QueryPattern{Value: &ischema.QueryPattern{Category: "int"}})}}, "value is a struct Name, not a node"},
		{"list element type", ischema.QueryPattern{Type: "Pair", Fields: []ischema.QueryFieldPattern{field("Items", ischema.QueryPattern{Elements: []ischema.QueryPattern{{Category: "int"}}})}}, "element 0"},
		{"terminal has no elements", ischema.QueryPattern{Type: "Pair", Fields: []ischema.QueryFieldPattern{field("Key", ischema.QueryPattern{Elements: []ischema.QueryPattern{{}}})}}, "has no elements"},
		{"named type below unknown", ischema.QueryPattern{Fields: []ischema.QueryFieldPattern{field("X", ischema.QueryPattern{Type: "Pair", Fields: []ischema.QueryFieldPattern{field("Key", ischema.QueryPattern{Category: "int"})}})}}, "field Key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ischema.CompileStructuralQuery(tc.pattern, s)
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
		prog, err := Compile(g, compile.Options{SourceInfo: true})
		if err != nil {
			continue
		}
		start := g.Rules()[0].Name
		s, err := outputSchemaPrototype(prog, start, false)
		if err != nil {
			continue
		}
		schemas++
		prepared, err := ischema.PrepareQuerySchema(s)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, in := range c.inputs {
			// Recovered parses report errors with their trees.
			root, _ := prog.ParseWith(start, in, ParseOptions{MissingTokens: 3})
			if root == nil {
				continue
			}
			err = ischema.VisitQueryGraph(root, &ischema.QueryBudget{}, func(v ischema.QueryVisit) error {
				n, ok := v.Value.(*value.Node)
				if !ok || n == nil {
					return nil
				}
				p := describeQueryValue(n, 3)
				patterns++
				if value.ShapeOf(n) == value.ShapeError {
					recovered++
				}
				q, err := ischema.CompileStructuralQuery(p, s)
				if err != nil {
					t.Errorf("%s %q: pattern of %s rejected: %v", c.name, in, n, err)
					return nil
				}
				if m, err := q.Matches(n, ischema.QueryOptions{}); err != nil || len(m) == 0 {
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
	prog, err := Compile(g, compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := outputSchemaPrototype(prog, "main", false)
	if err != nil {
		t.Fatal(err)
	}
	ok := ischema.QueryPattern{Type: "P", Fields: []ischema.QueryFieldPattern{field("V", ischema.QueryPattern{Type: "error", Fields: []ischema.QueryFieldPattern{field("message", ischema.QueryPattern{Category: "string"})}})}}
	q, err := ischema.CompileStructuralQuery(ok, s)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := prog.ParseWith("main", "(x,2)", ParseOptions{})
	if m, err := q.Matches(root, ischema.QueryOptions{}); err != nil || len(m) != 1 {
		t.Fatalf("matches %v %v", m, err)
	}
	for _, good := range []ischema.QueryPattern{
		{Type: "error", Fields: []ischema.QueryFieldPattern{field("message", ischema.QueryPattern{})}},
		{Type: "P", Fields: []ischema.QueryFieldPattern{field("V", ischema.QueryPattern{Category: "error", Fields: []ischema.QueryFieldPattern{field("expected", ischema.QueryPattern{Category: "string"})}})}},
	} {
		if _, err := ischema.CompileStructuralQuery(good, s); err != nil {
			t.Errorf("rejected %+v: %v", good, err)
		}
	}
	for _, bad := range []ischema.QueryPattern{
		{Type: "P", Fields: []ischema.QueryFieldPattern{field("V", ischema.QueryPattern{Type: "error", Fields: []ischema.QueryFieldPattern{field("message", ischema.QueryPattern{Category: "int"})}})}},
		{Type: "P", Fields: []ischema.QueryFieldPattern{field("V", ischema.QueryPattern{Type: "error", Fields: []ischema.QueryFieldPattern{field("other", ischema.QueryPattern{})}})}},
		{Type: "P", Fields: []ischema.QueryFieldPattern{field("V", ischema.QueryPattern{Category: "error", Fields: []ischema.QueryFieldPattern{field("other", ischema.QueryPattern{})}})}},
		{Type: "P", Fields: []ischema.QueryFieldPattern{field("V", ischema.QueryPattern{Elements: []ischema.QueryPattern{{}}})}},
		{Type: "P", Fields: []ischema.QueryFieldPattern{field("W", ischema.QueryPattern{Type: "error", Rule: "num"})}},
	} {
		if _, err := ischema.CompileStructuralQuery(bad, s); err == nil {
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
	s, err := outputSchemaPrototype(mustCompile(t, src), "main", true)
	if err != nil {
		t.Fatal(err)
	}
	p := ischema.QueryPattern{Category: "string"}
	for range 16 {
		p = ischema.QueryPattern{Fields: []ischema.QueryFieldPattern{field("X", p)}}
	}
	p.Type = "A"
	_, err = ischema.CompileStructuralQuery(p, s)
	if err == nil || len(err.Error()) > 4096 {
		t.Fatalf("error of %d bytes: %.200v", len(fmt.Sprint(err)), err)
	}
}

// describeQueryValue returns a pattern that matches v, describing it to the given depth.
func describeQueryValue(v any, depth int) ischema.QueryPattern {
	v = value.UnboxValue(v)
	category, _ := ischema.QueryCategory(v)
	p := ischema.QueryPattern{Category: category}
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
		val := describeQueryValue(ischema.QueryNodeValue(n), depth-1)
		val.Elements = nil
		p.Value = &val
	}
	if category == "struct" || category == "error" {
		for _, f := range value.D(n).Fields {
			p.Fields = append(p.Fields, ischema.QueryFieldPattern{Name: f.Name, Pattern: describeQueryValue(f.Value, depth-1)})
		}
	}
	if ischema.QueryHasElements(n) {
		for i, c := range value.D(n).Children {
			if i == 2 {
				break
			}
			p.Elements = append(p.Elements, describeQueryValue(c, depth-1))
		}
	}
	return p
}

// TestQuerySchemaConcurrentCompile compiles queries from several goroutines;
// run it with -race.
func TestQuerySchemaConcurrentCompile(t *testing.T) {
	q, err := ischema.PrepareQuerySchema(queryShapeSchema(t))
	if err != nil {
		t.Fatal(err)
	}
	patterns := []ischema.QueryPattern{
		{Type: "Pair", Fields: []ischema.QueryFieldPattern{field("Value", textStruct("Int"))}},
		{Type: "Box", Fields: []ischema.QueryFieldPattern{field("Inner", ischema.QueryPattern{Fields: []ischema.QueryFieldPattern{field("Value", ischema.QueryPattern{Type: "Pair", Fields: []ischema.QueryFieldPattern{field("Value", textStruct("Int"))}})}})}},
		{Type: "Pair", Fields: []ischema.QueryFieldPattern{field("Value", ischema.QueryPattern{Type: "Box"})}},
	}
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 50 {
				p := patterns[(g+i)%len(patterns)]
				_, err := q.CompileStructuralQuery(p)
				if (err == nil) != (p.Fields[0].Pattern.Type != "Box") {
					t.Errorf("pattern %d: %v", (g+i)%len(patterns), err)
					return
				}
			}
		})
	}
	wg.Wait()
}
