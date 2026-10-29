package engine

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/schema"
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

func queryShapeSchema(t *testing.T) *schema.Output {
	t.Helper()
	s, err := mustCompile(t, queryShapeGrammar).OutputSchemaPrototype("main", false)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func field(name string, p schema.QueryPattern) schema.QueryFieldPattern {
	return schema.QueryFieldPattern{Name: name, Pattern: p}
}

// textStruct matches a value of the text struct type name, such as Int or Name.
func textStruct(name string) schema.QueryPattern {
	return schema.QueryPattern{Type: name, Fields: []schema.QueryFieldPattern{{Name: "Text", Pattern: schema.QueryPattern{Category: "string"}}}}
}

// TestStructuralQueryNestedShapes checks that a pattern compiles if, and only if, some value of the
// schema can match it where it is applied, through unions, optional values, lists and nesting.
func TestStructuralQueryNestedShapes(t *testing.T) {
	s := queryShapeSchema(t)
	prepared, err := schema.PrepareQuerySchema(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		pattern schema.QueryPattern
		err     string // "" if the pattern compiles
	}{
		{"union alternative", schema.QueryPattern{Type: "Pair", Fields: []schema.QueryFieldPattern{field("Value", textStruct("Int"))}}, ""},
		{"nested union alternative", schema.QueryPattern{Type: "Pair", Fields: []schema.QueryFieldPattern{field("Value", schema.QueryPattern{Fields: []schema.QueryFieldPattern{field("Key", textStruct("Name"))}})}}, ""},
		{"nil alternative", schema.QueryPattern{Type: "Pair", Fields: []schema.QueryFieldPattern{field("Value", schema.QueryPattern{Category: "nil"})}}, ""},
		{"through optional", schema.QueryPattern{Type: "Box", Fields: []schema.QueryFieldPattern{field("Inner", schema.QueryPattern{Fields: []schema.QueryFieldPattern{field("Items", schema.QueryPattern{})}})}}, ""},
		{"list element", schema.QueryPattern{Type: "Pair", Fields: []schema.QueryFieldPattern{field("Items", schema.QueryPattern{Category: "list", Elements: []schema.QueryPattern{textStruct("Int")}})}}, ""},
		{"deep", schema.QueryPattern{Type: "Box", Fields: []schema.QueryFieldPattern{field("Inner", schema.QueryPattern{Fields: []schema.QueryFieldPattern{field("Value", schema.QueryPattern{Type: "Pair", Fields: []schema.QueryFieldPattern{field("Value", textStruct("Int"))}})}})}}, ""},
		{"untyped root", schema.QueryPattern{Fields: []schema.QueryFieldPattern{field("Anything", schema.QueryPattern{Category: "int"})}}, ""},
		{"rule result elements", schema.QueryPattern{Category: "node", Elements: []schema.QueryPattern{{Type: "Box"}}}, ""},

		{"type outside union", schema.QueryPattern{Type: "Pair", Fields: []schema.QueryFieldPattern{field("Value", schema.QueryPattern{Type: "Box"})}}, "alternatives matches"},
		{"field no alternative has", schema.QueryPattern{Type: "Pair", Fields: []schema.QueryFieldPattern{field("Value", schema.QueryPattern{Fields: []schema.QueryFieldPattern{field("Inner", schema.QueryPattern{})}})}}, "alternatives matches"},
		{"scalar category in node union", schema.QueryPattern{Type: "Pair", Fields: []schema.QueryFieldPattern{field("Value", schema.QueryPattern{Category: "string"})}}, "alternatives matches"},
		{"wrong type", schema.QueryPattern{Type: "Box", Fields: []schema.QueryFieldPattern{field("Inner", schema.QueryPattern{Type: "Box"})}}, "value is a struct Pair, not Box"},
		{"nested wrong field", schema.QueryPattern{Type: "Box", Fields: []schema.QueryFieldPattern{field("Inner", schema.QueryPattern{Fields: []schema.QueryFieldPattern{field("Inner", schema.QueryPattern{})}})}}, "a struct Pair has no field Inner"},
		{"text struct has no such field", schema.QueryPattern{Type: "Pair", Fields: []schema.QueryFieldPattern{field("Key", schema.QueryPattern{Fields: []schema.QueryFieldPattern{field("X", schema.QueryPattern{})}})}}, "a struct Name has no field X"},
		{"text struct is not a node", schema.QueryPattern{Type: "Pair", Fields: []schema.QueryFieldPattern{field("Key", schema.QueryPattern{Value: &schema.QueryPattern{Category: "int"}})}}, "value is a struct Name, not a node"},
		{"list element type", schema.QueryPattern{Type: "Pair", Fields: []schema.QueryFieldPattern{field("Items", schema.QueryPattern{Elements: []schema.QueryPattern{{Category: "int"}}})}}, "element 0"},
		{"terminal has no elements", schema.QueryPattern{Type: "Pair", Fields: []schema.QueryFieldPattern{field("Key", schema.QueryPattern{Elements: []schema.QueryPattern{{}}})}}, "has no elements"},
		{"named type below unknown", schema.QueryPattern{Fields: []schema.QueryFieldPattern{field("X", schema.QueryPattern{Type: "Pair", Fields: []schema.QueryFieldPattern{field("Key", schema.QueryPattern{Category: "int"})}})}}, "field Key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := schema.CompileStructuralQuery(tc.pattern, s)
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
		s, err := prog.OutputSchemaPrototype(start, false)
		if err != nil {
			continue
		}
		schemas++
		prepared, err := schema.PrepareQuerySchema(s)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, in := range c.inputs {
			// Recovered parses report errors with their trees.
			root, _ := prog.ParseWith(start, in, ParseOptions{MissingTokens: 3})
			if root == nil {
				continue
			}
			err = schema.VisitQueryGraph(root, &schema.QueryBudget{}, func(v schema.QueryVisit) error {
				n, ok := v.Value.(*value.Node)
				if !ok || n == nil {
					return nil
				}
				p := describeQueryValue(n, 3)
				patterns++
				if value.ShapeOf(n) == value.ShapeError {
					recovered++
				}
				q, err := schema.CompileStructuralQuery(p, s)
				if err != nil {
					t.Errorf("%s %q: pattern of %s rejected: %v", c.name, in, n, err)
					return nil
				}
				if m, err := q.Matches(n, schema.QueryOptions{}); err != nil || len(m) == 0 {
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
	s, err := prog.OutputSchemaPrototype("main", false)
	if err != nil {
		t.Fatal(err)
	}
	ok := schema.QueryPattern{Type: "P", Fields: []schema.QueryFieldPattern{field("V", schema.QueryPattern{Type: "error", Fields: []schema.QueryFieldPattern{field("message", schema.QueryPattern{Category: "string"})}})}}
	q, err := schema.CompileStructuralQuery(ok, s)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := prog.ParseWith("main", "(x,2)", ParseOptions{})
	if m, err := q.Matches(root, schema.QueryOptions{}); err != nil || len(m) != 1 {
		t.Fatalf("matches %v %v", m, err)
	}
	for _, good := range []schema.QueryPattern{
		{Type: "error", Fields: []schema.QueryFieldPattern{field("message", schema.QueryPattern{})}},
		{Type: "P", Fields: []schema.QueryFieldPattern{field("V", schema.QueryPattern{Category: "error", Fields: []schema.QueryFieldPattern{field("expected", schema.QueryPattern{Category: "string"})}})}},
	} {
		if _, err := schema.CompileStructuralQuery(good, s); err != nil {
			t.Errorf("rejected %+v: %v", good, err)
		}
	}
	for _, bad := range []schema.QueryPattern{
		{Type: "P", Fields: []schema.QueryFieldPattern{field("V", schema.QueryPattern{Type: "error", Fields: []schema.QueryFieldPattern{field("message", schema.QueryPattern{Category: "int"})}})}},
		{Type: "P", Fields: []schema.QueryFieldPattern{field("V", schema.QueryPattern{Type: "error", Fields: []schema.QueryFieldPattern{field("other", schema.QueryPattern{})}})}},
		{Type: "P", Fields: []schema.QueryFieldPattern{field("V", schema.QueryPattern{Category: "error", Fields: []schema.QueryFieldPattern{field("other", schema.QueryPattern{})}})}},
		{Type: "P", Fields: []schema.QueryFieldPattern{field("V", schema.QueryPattern{Elements: []schema.QueryPattern{{}}})}},
		{Type: "P", Fields: []schema.QueryFieldPattern{field("W", schema.QueryPattern{Type: "error", Rule: "num"})}},
	} {
		if _, err := schema.CompileStructuralQuery(bad, s); err == nil {
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
	p := schema.QueryPattern{Category: "string"}
	for range 16 {
		p = schema.QueryPattern{Fields: []schema.QueryFieldPattern{field("X", p)}}
	}
	p.Type = "A"
	_, err = schema.CompileStructuralQuery(p, s)
	if err == nil || len(err.Error()) > 4096 {
		t.Fatalf("error of %d bytes: %.200v", len(fmt.Sprint(err)), err)
	}
}

// describeQueryValue returns a pattern that matches v, describing it to the given depth.
func describeQueryValue(v any, depth int) schema.QueryPattern {
	v = value.UnboxValue(v)
	category, _ := schema.QueryCategory(v)
	p := schema.QueryPattern{Category: category}
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
		val := describeQueryValue(schema.QueryNodeValue(n), depth-1)
		val.Elements = nil
		p.Value = &val
	}
	if category == "struct" || category == "error" {
		for _, f := range value.D(n).Fields {
			p.Fields = append(p.Fields, schema.QueryFieldPattern{Name: f.Name, Pattern: describeQueryValue(f.Value, depth-1)})
		}
	}
	if schema.QueryHasElements(n) {
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
	q, err := schema.PrepareQuerySchema(queryShapeSchema(t))
	if err != nil {
		t.Fatal(err)
	}
	patterns := []schema.QueryPattern{
		{Type: "Pair", Fields: []schema.QueryFieldPattern{field("Value", textStruct("Int"))}},
		{Type: "Box", Fields: []schema.QueryFieldPattern{field("Inner", schema.QueryPattern{Fields: []schema.QueryFieldPattern{field("Value", schema.QueryPattern{Type: "Pair", Fields: []schema.QueryFieldPattern{field("Value", textStruct("Int"))}})}})}},
		{Type: "Pair", Fields: []schema.QueryFieldPattern{field("Value", schema.QueryPattern{Type: "Box"})}},
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
