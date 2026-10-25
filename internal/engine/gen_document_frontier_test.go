package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The examined frontier, the evaluations and the reuse that a Document reports for a rule that
// fails after its callee matched must be the same for the typed route and the Node route, and must
// include the position where the rule failed: a direct rule without captures is generated as one
// body for its call and its invocation only without Documents, since a Document reads the
// position the invocation leaves at its failure.
func TestGeneratedDocumentFrontierAfterFailure(t *testing.T) {
	eachMode(t, testGeneratedDocumentFrontierAfterFailure)
}

func testGeneratedDocumentFrontierAfterFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("builds standalone generated Document consumers")
	}
	const grammar = `var x int
rule main = part "b" / part / "a" [a-z]
rule part = leaf "a" {x == 1}
rule leaf = &"a"
`
	g, err := parseTest(grammar)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name, data string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", testGoMod("frontier"))
	const consumer = `package PACKAGE

import (
	"fmt"
	"strings"
	"testing"

	pegort "github.com/ornew/pego/runtime"
)

func TestFrontier(t *testing.T) {
	d, err := NewDocument("ab")
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	for i := 0; i < 2; i++ {
		_, _ = d.PARSE(pegort.WithTrace(func(e pegort.TraceEvent) {
			if e.Kind == pegort.TraceExit {
				fmt.Fprintf(&out, "exit %s pos=%d memo=%v evals=%d examined=%d\n", e.Rule, e.Pos, e.Memo, e.Evals, e.Examined)
			}
		}))
		fmt.Fprintf(&out, "stats %+v\n", d.Stats())
		if i == 0 {
			if err := d.Edit(1, 2, "c"); err != nil {
				t.Fatal(err)
			}
		}
	}
	fmt.Print("RESULT\n", out.String(), "END\n")
}
`
	results := map[string]string{}
	for _, route := range []string{"node", "typed"} {
		code, err := generateCode(g, GenOptions{Package: route, Start: "main", Document: true, Types: route == "typed", Recognize: true, Observability: true})
		if err != nil {
			t.Fatal(route, err)
		}
		write(route+"/parser.go", string(code))
		parse := "Parse"
		if route == "typed" {
			parse = "ParseAST"
		}
		write(route+"/frontier_test.go", strings.NewReplacer("PACKAGE", route, "PARSE", parse).Replace(consumer))
		cmd := exec.Command("go", "test", "-trimpath", "-count=1", "-v", "./"+route)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", route, err, out)
		}
		_, b, ok := strings.Cut(string(out), "RESULT\n")
		if !ok {
			t.Fatalf("%s: no result\n%s", route, out)
		}
		results[route], _, _ = strings.Cut(b, "END\n")
	}
	if results["node"] != results["typed"] {
		t.Errorf("the typed route reports differently from the Node route\nnode:\n%s\ntyped:\n%s", results["node"], results["typed"])
	}
	// The failing part examines the position after the matched leaf, then fails at it.
	if !strings.Contains(results["typed"], "exit part pos=0 memo=false evals=1 examined=2") {
		t.Errorf("part does not report the position where it failed:\n%s", results["typed"])
	}
}
