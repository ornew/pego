package engine

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func TestGeneratedGoActionContextAndExpressions(t *testing.T) {
	grammar, err := syntax.Parse(`type Result struct { Text string }
rule main Result = (leaf / [a-z]) => Result{Text: "ok"}
rule leaf = "a"
`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Generate(grammar, GenOptions{Package: "hygiene", Start: "main", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	source := string(code)
	if !strings.Contains(source, `p.where = "main"`) || !strings.Contains(source, "p.where = w") {
		t.Fatalf("direct typed action does not restore its prior p.where:\n%s", source)
	}
	if strings.Contains(source, "p.depth+0") {
		t.Fatal("generated Go contains a zero-depth addition")
	}
	if strings.Contains(source, "!!(") {
		t.Fatal("generated Go contains a double-negated first-character condition")
	}
}

func TestGeneratedRuntimeWritesStringPartsWithoutConcatenation(t *testing.T) {
	for _, fragment := range []string{
		strings.Join([]string{`b.WriteString("`, "`", `" + v + "`, "`", `")`}, ""),
		`b.WriteString("(" + n.Type())`,
		`b.WriteString(f.Name + "=")`,
	} {
		if strings.Contains(runtimeSource, fragment) {
			t.Errorf("generated runtime still concatenates in WriteString: %s", fragment)
		}
	}
}

func TestGeneratedDirectActionErrorContextAndParserReuse(t *testing.T) {
	cases := []genCase{{
		name: "typed/direct_action_error_context_and_reuse",
		src: `type Result struct { N int }
rule main Result = child
rule child Result = x:"a"? => Result{N: 1 / len($x)}
`,
		inputs: []string{"", "a", "x"},
	}}
	testGeneratedTypesCorpus(t, filepath.Join(runtime.GOROOT(), "bin", "go"), cases, GenOptions{convertTypes: true}, 1)
}
