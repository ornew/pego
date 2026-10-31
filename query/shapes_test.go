package query_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/query"
	"github.com/ornew/zeroparse/schema"
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
	return mustSchema(t, mustCompile(t, queryShapeGrammar, zeroparse.WithSourceInfo()))
}

// TestStructuralQueryNestedShapes checks that a pattern compiles if, and only if, some value of the
// schema can match it where it is applied, through unions, optional values, lists and nesting.
func TestStructuralQueryNestedShapes(t *testing.T) {
	s := queryShapeSchema(t)
	prepared, err := query.Prepare(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		pattern query.Pattern
		err     string // "" if the pattern compiles
	}{
		{"union alternative", query.Pattern{Type: "Pair", Fields: []query.FieldPattern{field("Value", textStruct("Int"))}}, ""},
		{"nested union alternative", query.Pattern{Type: "Pair", Fields: []query.FieldPattern{field("Value", query.Pattern{Fields: []query.FieldPattern{field("Key", textStruct("Name"))}})}}, ""},
		{"nil alternative", query.Pattern{Type: "Pair", Fields: []query.FieldPattern{field("Value", query.Pattern{Category: "nil"})}}, ""},
		{"through optional", query.Pattern{Type: "Box", Fields: []query.FieldPattern{field("Inner", query.Pattern{Fields: []query.FieldPattern{field("Items", query.Pattern{})}})}}, ""},
		{"list element", query.Pattern{Type: "Pair", Fields: []query.FieldPattern{field("Items", query.Pattern{Category: "list", Elements: []query.Pattern{textStruct("Int")}})}}, ""},
		{"deep", query.Pattern{Type: "Box", Fields: []query.FieldPattern{field("Inner", query.Pattern{Fields: []query.FieldPattern{field("Value", query.Pattern{Type: "Pair", Fields: []query.FieldPattern{field("Value", textStruct("Int"))}})}})}}, ""},
		{"untyped root", query.Pattern{Fields: []query.FieldPattern{field("Anything", query.Pattern{Category: "int"})}}, ""},
		{"rule result elements", query.Pattern{Category: "node", Elements: []query.Pattern{{Type: "Box"}}}, ""},

		{"type outside union", query.Pattern{Type: "Pair", Fields: []query.FieldPattern{field("Value", query.Pattern{Type: "Box"})}}, "alternatives matches"},
		{"field no alternative has", query.Pattern{Type: "Pair", Fields: []query.FieldPattern{field("Value", query.Pattern{Fields: []query.FieldPattern{field("Inner", query.Pattern{})}})}}, "alternatives matches"},
		{"scalar category in node union", query.Pattern{Type: "Pair", Fields: []query.FieldPattern{field("Value", query.Pattern{Category: "string"})}}, "alternatives matches"},
		{"wrong type", query.Pattern{Type: "Box", Fields: []query.FieldPattern{field("Inner", query.Pattern{Type: "Box"})}}, "value is a struct Pair, not Box"},
		{"nested wrong field", query.Pattern{Type: "Box", Fields: []query.FieldPattern{field("Inner", query.Pattern{Fields: []query.FieldPattern{field("Inner", query.Pattern{})}})}}, "a struct Pair has no field Inner"},
		{"text struct has no such field", query.Pattern{Type: "Pair", Fields: []query.FieldPattern{field("Key", query.Pattern{Fields: []query.FieldPattern{field("X", query.Pattern{})}})}}, "a struct Name has no field X"},
		{"text struct is not a node", query.Pattern{Type: "Pair", Fields: []query.FieldPattern{field("Key", query.Pattern{Value: &query.Pattern{Category: "int"}})}}, "value is a struct Name, not a node"},
		{"list element type", query.Pattern{Type: "Pair", Fields: []query.FieldPattern{field("Items", query.Pattern{Elements: []query.Pattern{{Category: "int"}}})}}, "element 0"},
		{"terminal has no elements", query.Pattern{Type: "Pair", Fields: []query.FieldPattern{field("Key", query.Pattern{Elements: []query.Pattern{{}}})}}, "has no elements"},
		{"named type below unknown", query.Pattern{Fields: []query.FieldPattern{field("X", query.Pattern{Type: "Pair", Fields: []query.FieldPattern{field("Key", query.Pattern{Category: "int"})}})}}, "field Key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := query.Compile(tc.pattern, s)
			switch {
			case tc.err == "" && err != nil:
				t.Fatalf("rejected: %v", err)
			case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
				t.Fatalf("error %v, want %q", err, tc.err)
			}
			if _, perr := prepared.Compile(tc.pattern); fmt.Sprint(perr) != fmt.Sprint(err) {
				t.Fatalf("prepared schema: error %v, want %v", perr, err)
			}
		})
	}
}

// queryRecoveryGrammar puts an Error node where a declared type expects another node.
const queryRecoveryGrammar = `type Num = { Text string }
type P = { V Num | error; W Num }
rule main = "(" v:(num #recover(skip=[^),]+)) "," w:num ")" $$ => P{V: $v, W: $w}
rule num Num = t:([0-9]) #atomic => Num{Text: text($t)}`

// TestStructuralQueryRecoveryNodes checks patterns of recovery nodes where declared types are
// expected, and of their fields.
func TestStructuralQueryRecoveryNodes(t *testing.T) {
	p := mustCompile(t, queryRecoveryGrammar)
	s := mustSchema(t, p)
	ok := query.Pattern{Type: "P", Fields: []query.FieldPattern{field("V", query.Pattern{Type: "error", Fields: []query.FieldPattern{field("message", query.Pattern{Category: "string"})}})}}
	q, err := query.Compile(ok, s)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := p.Parse("(x,2)")
	if m, err := q.Matches(root); err != nil || len(m) != 1 {
		t.Fatalf("matches %v %v", m, err)
	}
	for _, good := range []query.Pattern{
		{Type: "error", Fields: []query.FieldPattern{field("message", query.Pattern{})}},
		{Type: "P", Fields: []query.FieldPattern{field("V", query.Pattern{Category: "error", Fields: []query.FieldPattern{field("expected", query.Pattern{Category: "string"})}})}},
	} {
		if _, err := query.Compile(good, s); err != nil {
			t.Errorf("rejected %+v: %v", good, err)
		}
	}
	for _, bad := range []query.Pattern{
		{Type: "P", Fields: []query.FieldPattern{field("V", query.Pattern{Type: "error", Fields: []query.FieldPattern{field("message", query.Pattern{Category: "int"})}})}},
		{Type: "P", Fields: []query.FieldPattern{field("V", query.Pattern{Type: "error", Fields: []query.FieldPattern{field("other", query.Pattern{})}})}},
		{Type: "P", Fields: []query.FieldPattern{field("V", query.Pattern{Category: "error", Fields: []query.FieldPattern{field("other", query.Pattern{})}})}},
		{Type: "P", Fields: []query.FieldPattern{field("V", query.Pattern{Elements: []query.Pattern{{}}})}},
		{Type: "P", Fields: []query.FieldPattern{field("W", query.Pattern{Type: "error", Rule: "num"})}},
	} {
		if _, err := query.Compile(bad, s); err == nil {
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
	s := mustSchema(t, mustCompile(t, src, zeroparse.WithSourceInfo()), zeroparse.WithAllSchemaRules())
	p := query.Pattern{Category: "string"}
	for range 16 {
		p = query.Pattern{Fields: []query.FieldPattern{field("X", p)}}
	}
	p.Type = "A"
	_, err := query.Compile(p, s)
	if err == nil || len(err.Error()) > 4096 {
		t.Fatalf("error of %d bytes: %.200v", len(fmt.Sprint(err)), err)
	}
}

// TestQuerySchemaConcurrentCompile compiles queries from several goroutines;
// run it with -race.
func TestQuerySchemaConcurrentCompile(t *testing.T) {
	q, err := query.Prepare(queryShapeSchema(t))
	if err != nil {
		t.Fatal(err)
	}
	patterns := []query.Pattern{
		{Type: "Pair", Fields: []query.FieldPattern{field("Value", textStruct("Int"))}},
		{Type: "Box", Fields: []query.FieldPattern{field("Inner", query.Pattern{Fields: []query.FieldPattern{field("Value", query.Pattern{Type: "Pair", Fields: []query.FieldPattern{field("Value", textStruct("Int"))}})}})}},
		{Type: "Pair", Fields: []query.FieldPattern{field("Value", query.Pattern{Type: "Box"})}},
	}
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 50 {
				p := patterns[(g+i)%len(patterns)]
				_, err := q.Compile(p)
				if (err == nil) != (p.Fields[0].Pattern.Type != "Box") {
					t.Errorf("pattern %d: %v", (g+i)%len(patterns), err)
					return
				}
			}
		})
	}
	wg.Wait()
}
