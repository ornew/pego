package gogen_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

// TestGeneratedActionTracking inspects state after real generated rules have
// run, including direct typed constructors that bypass tctx.finish.
func TestGeneratedActionTracking(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedActionTracking)
}

func testGeneratedActionTracking(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	for _, c := range []struct{ name, src, input string }{
		{"nil", `type V = { N int }
type Root = { Items []*V }
rule main = items:item* $$ => Root{Items: $items.Value}
rule item = "a" => foldl(nil, list(V{N: 1}), (acc, v) => nil)`, "aaaa"},
		{"nested constructor", `type Inner = { N int }
type Outer = { Inner Inner }
rule main = "a" => Outer{Inner: Inner{N: 1}}`, "a"},
	} {
		t.Run(c.name, func(t *testing.T) {
			g, err := parseTest(c.src)
			if err != nil {
				t.Fatal(err)
			}
			code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "main", Start: "main", Types: true})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			main := fmt.Sprintf(`package main
import (
	"fmt"
	zprt "github.com/ornew/zeroparse/runtime"
)
func main() {
    for _, unit := range []PositionUnit{CodePoints, Bytes} {
        p := &zprt.Parser{MaxDepth: zpGrammar.X_maxDepth}
        if unit == Bytes { p.X_unit, p.X_bs, p.X_n = Bytes, %[1]q, len(%[1]q) } else { p.X_setSource(%[1]q) }
        p.X_setGrammar(zpGrammar)
        n, ok := p.Call(rules[0], 0)
        if !ok || p.Pos != p.X_n || n == nil { panic("Node parse failed") }
        if len(p.X_created) != 0 { panic("Node tracking length") }
        for _, n := range p.X_created[:cap(p.X_created)] { if n != nil { panic("Node tracking tail") } }
        // Clearing tracking must preserve the returned result and its fields.
        if _, err := n.MarshalJSON(); err != nil { panic(err) }
        tp := &zprt.TParser{Parser: &zprt.Parser{}}
        v, err := tp.X_run(trules[0], %[1]q, zprt.X_parseOptions{X_expectations: true, X_unit: unit, X_maxDepth: zpGrammar.X_maxDepth}, &tslabs{})
        if err != nil || v == nil { panic(fmt.Sprintf("typed parse: %%v", err)) }
        if len(tp.X_created) != 0 { panic("typed tracking length") }
        for _, n := range tp.X_created[:cap(tp.X_created)] { if n != nil { panic("typed tracking tail") } }
    }
}`, c.input)
			for name, contents := range map[string][]byte{
				"parser.go": code, "main.go": []byte(main), "go.mod": []byte(gentest.GoMod("lifecycle")),
			} {
				if err := os.WriteFile(filepath.Join(dir, name), contents, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("go", "run", ".")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("generated rules: %v\n%s", err, out)
			}
		})
	}
}

// Inspect the live environment before returning from a generated rule: rule
// return restores the caller's environment and would conceal retained history.
func TestGeneratedEnvironmentBindings(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedEnvironmentBindings)
}

func testGeneratedEnvironmentBindings(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	g, err := parseTest(gentest.BoundStream)
	if err != nil {
		t.Fatal(err)
	}
	code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "main", Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	main := `package main
import (
	"strings"
	zprt "github.com/ornew/zeroparse/runtime"
)
func main() {
 for _, unit := range []PositionUnit{CodePoints, Bytes} {
  input := strings.Repeat("a", 10000)
  p := &zprt.Parser{MaxDepth: zpGrammar.X_maxDepth}
  if unit == Bytes { p.X_unit, p.X_bs, p.X_n = Bytes, input, len(input) } else { p.X_setSource(input) }
  p.X_setGrammar(zpGrammar)
  p.Frame = p.NewFrame(len(rules[0].Scope))
  if _, ok := rules[0].Body(p, 0); !ok || p.Pos != p.X_n { panic("parse failed") }
  // The environment holds one value for each of the three declared variables, however many
  // bindings the parse made.
  if p.Env == nil || len(p.Env.X_vals) != 3 || p.X_envDepth != 0 { panic("lost binding") }
 }
}`
	for name, contents := range map[string][]byte{"parser.go": code, "main.go": []byte(main), "go.mod": []byte(gentest.GoMod("lifecycle"))} {
		if err := os.WriteFile(filepath.Join(dir, name), contents, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated binding state: %v\n%s", err, out)
	}
}
