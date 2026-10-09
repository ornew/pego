package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// TestGeneratedActionTracking inspects state after real generated rules have
// run, including direct typed constructors that bypass tctx.finish.
func TestGeneratedActionTracking(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	for _, c := range []struct{ name, src, input string }{
		{"nil", `
type V struct { N int }
type Root struct { Items []*V }
def main = items:item* $$ -> new Root{Items: $items}
def item = "a" -> foldl(nil, list(new V{N: 1}), (acc, v) => nil)`, "aaaa"},
		{"nested constructor", `
type Inner struct { N int }
type Outer struct { Inner Inner }
def main = "a" -> new Outer{Inner: new Inner{N: 1}}`, "a"},
	} {
		t.Run(c.name, func(t *testing.T) {
			g, err := syntax.Parse(c.src)
			if err != nil {
				t.Fatal(err)
			}
			code, err := Generate(g, GenOptions{Package: "main", Start: "main", Types: true})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			main := fmt.Sprintf(`package main
import "fmt"
func main() {
    for _, unit := range []Unit{CodePoints, Bytes} {
        p := &parser{}
        if unit == Bytes { p.unit, p.bs, p.n = Bytes, %[1]q, len(%[1]q) } else { p.setSource(%[1]q) }
        p.memo.stride = nseen
        n, ok := p.call(rules[0], 0)
        if !ok || p.pos != p.n || n == nil { panic("Node parse failed") }
        if len(p.created) != 0 { panic("Node tracking length") }
        for _, n := range p.created[:cap(p.created)] { if n != nil { panic("Node tracking tail") } }
        // Clearing tracking must preserve the returned result and its fields.
        if _, err := n.MarshalJSON(); err != nil { panic(err) }
        tp := &tparser{parser: &parser{}}
        v, err := tp.run(trules[0], %[1]q, []Unit{unit}, &tslabs{})
        if err != nil || v == nil { panic(fmt.Sprintf("typed parse: %%v", err)) }
        if len(tp.created) != 0 { panic("typed tracking length") }
        for _, n := range tp.created[:cap(tp.created)] { if n != nil { panic("typed tracking tail") } }
    }
}`, c.input)
			for name, contents := range map[string][]byte{
				"parser.go": code, "main.go": []byte(main), "go.mod": []byte("module lifecycle\n\ngo 1.27\n"),
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
	if testing.Short() {
		t.Skip("runs generated code")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	g, err := syntax.Parse(nilActionStream)
	if err != nil {
		t.Fatal(err)
	}
	code, err := GenerateTS(g, GenOptions{Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	// Execute in the generated module so private runtime state can be inspected
	// without adding test hooks to its public API.
	code = append(code, []byte(`
for (const unit of [CodePoints, Bytes]) {
  const p = new Parser("aaaa", unit, nseen);
  const n = p.call(rules[0]!, 0);
  if (n === undefined || n === null || p.pos !== p.n) throw new Error("parse failed");
  if (p.created.length !== 0) throw new Error("nil actions retain tracking");
  for (const v of [null, 1]) {
    p.created = [n];
    const c = p.useCtx(emptyFrame, 0, 4, null, null, null);
    let failed = false;
    try { c.result(() => { p.created.push(n); return v; }, "test"); }
    catch { failed = true; }
    if (failed !== (v !== null)) throw new Error("wrong result validation");
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
