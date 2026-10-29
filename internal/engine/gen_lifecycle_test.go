package engine

import (
	"fmt"
	"github.com/ornew/pego/internal/gentest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

// TestGeneratedActionTracking inspects state after real generated rules have
// run, including direct typed constructors that bypass tctx.finish.
func TestGeneratedActionTracking(t *testing.T) {
	t.Parallel()
	eachMode(t, testGeneratedActionTracking)
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
			code, err := generateCode(t, g, gen.GenOptions{Package: "main", Start: "main", Types: true})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			main := fmt.Sprintf(`package main
import (
	"fmt"
	pegort "github.com/ornew/pego/runtime"
)
func main() {
    for _, unit := range []PositionUnit{CodePoints, Bytes} {
        p := &pegort.Parser{MaxDepth: pegoGrammar.X_maxDepth}
        if unit == Bytes { p.X_unit, p.X_bs, p.X_n = Bytes, %[1]q, len(%[1]q) } else { p.X_setSource(%[1]q) }
        p.X_setGrammar(pegoGrammar)
        n, ok := p.Call(rules[0], 0)
        if !ok || p.Pos != p.X_n || n == nil { panic("Node parse failed") }
        if len(p.X_created) != 0 { panic("Node tracking length") }
        for _, n := range p.X_created[:cap(p.X_created)] { if n != nil { panic("Node tracking tail") } }
        // Clearing tracking must preserve the returned result and its fields.
        if _, err := n.MarshalJSON(); err != nil { panic(err) }
        tp := &pegort.TParser{Parser: &pegort.Parser{}}
        v, err := tp.X_run(trules[0], %[1]q, pegort.X_parseOptions{X_unit: unit, X_maxDepth: pegoGrammar.X_maxDepth}, &tslabs{})
        if err != nil || v == nil { panic(fmt.Sprintf("typed parse: %%v", err)) }
        if len(tp.X_created) != 0 { panic("typed tracking length") }
        for _, n := range tp.X_created[:cap(tp.X_created)] { if n != nil { panic("typed tracking tail") } }
    }
}`, c.input)
			for name, contents := range map[string][]byte{
				"parser.go": code, "main.go": []byte(main), "go.mod": []byte(testGoMod("lifecycle")),
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

func TestGeneratedTSActionTracking(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated code")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	g, err := parseTest(gentest.NilActionStream)
	if err != nil {
		t.Fatal(err)
	}
	code, err := generateTSBundle(g, gen.GenOptions{Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	// Execute in the generated module so private runtime state can be inspected
	// without adding test hooks to its public API.
	code = append(code, []byte(`
for (const unit of [CodePoints, Bytes]) {
  const p = new Parser(G, "aaaa", unit, nseen, G.maxDepth);
  const n = p.call(rules[0]!, 0);
  if (n === undefined || n === null || p.pos !== p.n) throw new Error("parse failed");
  if (p.created.length !== 0) throw new Error("nil actions retain tracking");
  // Any value is a result (boxed unless it is a node); a function is not.
  for (const v of [null, 1, () => 0]) {
    p.created = [n];
    const c = p.useCtx(emptyFrame, 0, 4, null, null, null);
    let failed = false;
    try { c.result(() => { p.created.push(n); return v; }, "test"); }
    catch { failed = true; }
    if (failed !== (typeof v === "function")) throw new Error("wrong result validation");
    if (p.created.length !== 1 || p.created[0] !== n) throw new Error("result tracking cleanup");
  }
  p.created = [n];
  const c = p.useCtx(emptyFrame, 0, 4, null, null, null);
  const failure = new Error("unexpected failure");
  try { c.result(() => { p.created.push(n); throw failure; }, "test"); }
  catch (x) { if (x !== failure) throw x; }
  if (p.created.length !== 1 || p.created[0] !== n) throw new Error("throw tracking cleanup");
}
`)...)
	dir := t.TempDir()
	for name, contents := range map[string][]byte{
		"parser.ts": code, "package.json": []byte(`{"type":"module"}`),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), contents, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(node, "parser.ts")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated TS tracking: %v\n%s", err, out)
	}
}

// Inspect the live environment before returning from a generated rule: rule
// return restores the caller's environment and would conceal retained history.
func TestGeneratedEnvironmentBindings(t *testing.T) {
	t.Parallel()
	eachMode(t, testGeneratedEnvironmentBindings)
}

func testGeneratedEnvironmentBindings(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	g, err := parseTest(boundStream)
	if err != nil {
		t.Fatal(err)
	}
	code, err := generateCode(t, g, gen.GenOptions{Package: "main", Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	main := `package main
import (
	"strings"
	pegort "github.com/ornew/pego/runtime"
)
func main() {
 for _, unit := range []PositionUnit{CodePoints, Bytes} {
  input := strings.Repeat("a", 10000)
  p := &pegort.Parser{MaxDepth: pegoGrammar.X_maxDepth}
  if unit == Bytes { p.X_unit, p.X_bs, p.X_n = Bytes, input, len(input) } else { p.X_setSource(input) }
  p.X_setGrammar(pegoGrammar)
  p.Frame = p.NewFrame(len(rules[0].Scope))
  if _, ok := rules[0].Body(p, 0); !ok || p.Pos != p.X_n { panic("parse failed") }
  names := make(map[string]bool)
  for e := p.Env; e != nil; e = e.X_next {
   if names[e.X_name] { panic("retained shadowed binding") }
   names[e.X_name] = true
  }
  if len(names) != 3 { panic("lost binding") }
 }
}`
	for name, contents := range map[string][]byte{"parser.go": code, "main.go": []byte(main), "go.mod": []byte(testGoMod("lifecycle"))} {
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

func TestGeneratedTSEnvironmentBindings(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated code")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	g, err := parseTest(boundStream)
	if err != nil {
		t.Fatal(err)
	}
	code, err := generateTSBundle(g, gen.GenOptions{Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	code = append(code, []byte(`
for (const unit of [CodePoints, Bytes]) {
 const p = new Parser(G, "a".repeat(10000), unit, nseen, G.maxDepth);
 if (rules[0]!.body(p,0) === undefined || p.pos !== p.n) throw new Error("parse failed");
 const names = new Set<string>();
 for (let e = p.env; e !== null; e = e.next) {
  if (names.has(e.name)) throw new Error("retained shadowed binding");
  names.add(e.name);
 }
 if (names.size !== 3) throw new Error("lost binding");
 p.env = null;
 p.bindValue("z", 1);
 const saved = p.env as Env | null;
 p.bindValue("z", 1);
 if (p.env !== saved) throw new Error("equal binding replaced state");
 p.bindValue("w", true);
 p.env = saved;
 p.bindValue("w", false);
 p.bindValue("z", 2);
 if (saved!.val !== 1) throw new Error("saved environment mutated");
 const q = new Parser(G, "", unit, nseen, G.maxDepth);
 q.env = null; // the parser starts from the declared variables
 q.bindValue("x",1);
 const first = q.env as Env | null;
 q.env = null;
 q.bindValue("y",2);
 q.bindValue("z",3);
 q.env = first;
 q.bindValue("w",4);
 q.bindValue("x",5);
 let count = 0;
 for (let e = q.env; e !== null; e = e.next) count++;
 if (count !== 2 || first!.val !== 1) throw new Error("early snapshot restore");
}
`)...)
	dir := t.TempDir()
	for name, contents := range map[string][]byte{"parser.ts": code, "package.json": []byte(`{"type":"module"}`)} {
		if err := os.WriteFile(filepath.Join(dir, name), contents, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(node, "parser.ts")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated TS binding state: %v\n%s", err, out)
	}
}
