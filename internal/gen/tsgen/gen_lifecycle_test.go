package tsgen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
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
 // The environment holds one value for each of the three declared variables, however many
 // bindings the parse made.
 if (p.env === null || p.env.length !== 3 || p.envDepth !== 0) throw new Error("lost binding");
 const base = newEnv([0, 1, 2], 0)!;
 p.env = base;
 p.bindValue(0, 0);
 if (p.env !== base) throw new Error("equal binding replaced state");
 p.bindValue(1, true);
 const saved = p.env;
 p.bindValue(1, true);
 if (p.env !== saved) throw new Error("equal rebinding replaced state");
 p.env = base;
 p.bindValue(1, false);
 p.bindValue(2, 2);
 if (saved![1] !== true || base[1] !== 1 || base[2] !== 2) throw new Error("saved environment mutated");
 // A large environment copies only the path to a slot.
 const vals: number[] = [];
 for (let i = 0; i < 280; i++) vals.push(i);
 const big = newEnv(vals, envDepthOf(280))!;
 p.env = big;
 p.envDepth = envDepthOf(280);
 p.bindValue(279, -1);
 const changed = p.env;
 p.env = big;
 p.bindValue(0, -2);
 for (let i = 0; i < 280; i++) {
  if (envGet(big, i, p.envDepth) !== i) throw new Error("large saved environment mutated");
  if (envGet(changed, i, p.envDepth) !== (i === 279 ? -1 : i)) throw new Error("large environment lost a value");
 }
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
