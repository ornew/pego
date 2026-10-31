package apitest_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/grammar"
)

// stripActions returns g written without value actions and rule types, which it removes from the
// JSON form of the grammar: a reference independent of the compiler's own pass.
func stripActions(t *testing.T, g *grammar.Grammar) *grammar.Grammar {
	t.Helper()
	data, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	var tree any
	if err := json.Unmarshal(data, &tree); err != nil {
		t.Fatal(err)
	}
	var strip func(v any)
	strip = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			// Rule definitions are the objects with a name and an expression; a type
			// declaration has no expression.
			_, hasExpr := v["expr"]
			_, hasName := v["name"]
			if hasExpr && hasName {
				delete(v, "type")
			}
			delete(v, "action")
			delete(v, "defaultAction")
			for _, c := range v {
				strip(c)
			}
		case []any:
			for _, c := range v {
				strip(c)
			}
		}
	}
	strip(tree)
	data, err = json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	out, err := grammar.UnmarshalJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestDefaultValueCorpus checks, for every grammar of the repository, that default values give
// the result of the grammar with all actions and rule types removed, on every backend. The
// parsers' grammars have predicates that read values made by actions, so the rules that make those
// values keep their actions; for them the backends must agree, and the input must match exactly
// when it matches the grammar without actions (if that compiles).
func TestDefaultValueCorpus(t *testing.T) {
	chdirModuleRoot(t)
	grammars, _ := filepath.Glob("examples/*/*.zp")
	more, _ := filepath.Glob("parsers/*/*.zp")
	grammars = append(grammars, more...)
	if len(grammars) == 0 {
		t.Fatal("no grammars")
	}
	for _, path := range grammars {
		t.Run(path, func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			g, err := zp.ParseGrammar(string(src))
			if err != nil {
				t.Fatal(err)
			}
			got, err := zp.Compile(g, "main", zp.WithDefaultValue(), zp.WithSourceInfo())
			if err != nil {
				t.Fatalf("default values: %v", err)
			}
			exact := strings.HasPrefix(path, "examples/")
			want, err := zp.Compile(stripActions(t, g), "main", zp.WithSourceInfo())
			if err != nil {
				if exact {
					t.Fatalf("stripped: %v", err)
				}
				want = nil // predicates read values that only actions make
			}
			dir := filepath.Dir(path)
			inputs, _ := filepath.Glob(filepath.Join(dir, "testdata", "*.txt"))
			if len(inputs) > 40 {
				inputs = inputs[:40]
			}
			for _, in := range inputs {
				data, err := os.ReadFile(in)
				if err != nil || len(data) > 200_000 {
					continue
				}
				ref := render(got.Parse(string(data), zp.WithBackend(zp.Closure)))
				for _, b := range []zp.Backend{zp.Bytecode, zp.BytecodeIterative} {
					if gotTree := render(got.Parse(string(data), zp.WithBackend(b))); gotTree != ref {
						t.Errorf("%s (%v): differs from the closure backend\n got  %.300s\n want %.300s", in, b, gotTree, ref)
					}
				}
				if want == nil {
					continue
				}
				wantTree := render(want.Parse(string(data), zp.WithBackend(zp.Closure)))
				if !exact {
					_, e1 := got.Parse(string(data))
					_, e2 := want.Parse(string(data))
					ref, wantTree = fmt.Sprint(e1), fmt.Sprint(e2)
				}
				if ref != wantTree {
					t.Errorf("%s: default value differs from the grammar without actions\n got  %.300s\n want %.300s", in, ref, wantTree)
				}
			}
		})
	}
}

func render(n *zp.Node, err error) string {
	s := ""
	if n != nil {
		s = n.String()
	}
	if err != nil {
		s += "\nerror: " + err.Error()
	}
	return s
}
