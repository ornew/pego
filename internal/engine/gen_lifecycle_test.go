package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

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
	code, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main"})
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

func TestGeneratedTSEnvironmentBindings(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated code")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	g, err := parseTest(gentest.BoundStream)
	if err != nil {
		t.Fatal(err)
	}
	code, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main"})
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
