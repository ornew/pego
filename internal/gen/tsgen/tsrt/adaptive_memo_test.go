package tsrt

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestAdaptiveMemoCounters exercises the TypeScript runtime's counter arithmetic
// directly while reusing its Parser implementation from the same module.
func TestAdaptiveMemoCounters(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	// go test runs in the package directory, which holds runtime.ts; the path does not depend on
	// how the test binary was built (-trimpath rewrites source file names).
	source, err := os.ReadFile("runtime.ts")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "adaptive_memo.ts")
	if err := os.WriteFile(path, append(source, []byte(adaptiveMemoTypeScriptTest)...), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, path)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("node: %v\n%s", err, output)
	}
}

const adaptiveMemoTypeScriptTest = `

const maxCalls = 0x7fffffff;
const cases = [
  ["below threshold", 15, 0, true, 16, 1, false],
  ["above threshold", 14, 0, true, 15, 1, true],
  ["ordinary next call", 16, 0, true, 17, 1, false],
  ["threshold before discount", 0x7ffffffe, 0x07ffffff, true, maxCalls, 0x08000000, true],
  ["large exact threshold", 0x3fffffff, 0x03ffffff, true, 0x40000000, 0x04000000, false],
  ["large above threshold", 0x3ffffffe, 0x03ffffff, true, 0x3fffffff, 0x04000000, true],
  ["before rescale", maxCalls - 1, 1, false, maxCalls, 1, false],
  ["rescale odd unique", maxCalls, 1, false, 0x40000000, 0, false],
  ["rescale odd repeat", maxCalls, 3, true, 0x40000000, 2, false],
  ["rescale unique", maxCalls, 0, false, 0x40000000, 0, false],
  ["rescale repeat", maxCalls, 0, true, 0x40000000, 1, false],
  ["rescale below threshold", maxCalls, 0x07fffffd, true, 0x40000000, 0x03ffffff, false],
  ["rescale exact threshold", maxCalls, 0x07ffffff, true, 0x40000000, 0x04000000, false],
  ["rescale above threshold", maxCalls, 0x08000000, true, 0x40000000, 0x04000001, true],
];
for (const [name, calls, repeats, repeat, wantCalls, wantRepeats, eager] of cases) {
  const p = new Parser(mkGrammar(100_000, true, false), "", CodePoints, 1, 100);
  p.calls[0] = calls;
  p.repeats[0] = repeats;
  if (repeat) p.seen = Int32Array.of(1);
  const got = p.firstCall({ seen: 0 });
  if (got !== !repeat || p.calls[0] !== wantCalls || p.repeats[0] !== wantRepeats || (p.eager[0] !== 0) !== eager) {
    throw new Error(name + ": first=" + got + ", calls=" + p.calls[0] + ", repeats=" + p.repeats[0] + ", eager=" + p.eager[0]);
  }
  if (eager) {
    const state = [p.calls[0], p.repeats[0], p.eager[0]];
    if (p.firstCall({ seen: 0 }) || state.some((value, i) => value !== [p.calls[0], p.repeats[0], p.eager[0]][i])) {
      throw new Error(name + ": eager state resumed counting");
    }
  }
}
const p = new Parser(mkGrammar(100_000, true, false), "", CodePoints, 1, 100);
p.calls[0] = maxCalls;
if (!p.firstCall({ seen: 0 }) || p.firstCall({ seen: 0 }) || p.eager[0] !== 0) {
  throw new Error("fresh then repeated calls became eager after rescaling");
}
`
