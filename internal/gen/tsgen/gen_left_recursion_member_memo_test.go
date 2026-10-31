package tsgen_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

// TestGeneratedTSLeftRecursionMemberMemoLinear checks that generated TypeScript parsers memoize
// the members of a left-recursion cycle that are not its leader: a member reached from two call
// sites at one position is evaluated again only while a seed it read is still growing, so
// parsing the nested input takes time in proportion to its size, where the evaluations used to
// double with each nesting level.
func TestGeneratedTSLeftRecursionMemberMemoLinear(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated code")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	g, err := parseTest(`@start
rule main = a $$
rule a = c "+" / "n"
rule c = "(" c ")" "y" / "(" c ")" "z" / a`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	code = append(code, []byte(`
for (const unit of [CodePoints, Bytes]) {
  for (const n of [12, 300]) {
    const input = "(".repeat(n) + "n" + ")z".repeat(n) + "+";
    const p = new Parser(G, input, unit, nseen, G.maxDepth);
    const v = p.call(rules[0]!, 0);
    if (v === undefined || p.pos !== p.n) throw new Error("parse failed at " + n);
  }
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
	// Without the memo the parse at n = 300 runs for ages; fail instead of waiting.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "parser.ts")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated TS member memo: %v\n%s", err, out)
	}
}

// TestGeneratedTSLeftRecursionMemberMemoStale checks that generated TypeScript parsers give the
// engine's results on grammars where a memoized member result could go stale while a leader grows.
// TestLeftRecursionMemberMemoStale in the engine checks that the engine gives the results of the
// grammars compiled without a memo.
func TestGeneratedTSLeftRecursionMemberMemoStale(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated TypeScript")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	gentest.TSParsersCorpus(t, node, append(gentest.LeftRecursionMemberCases(), gentest.LeftRecursionMemberSeedCases()...), gen.GenOptions{})
}
