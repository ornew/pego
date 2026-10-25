package engine

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGeneratedGoActionContextAndExpressions(t *testing.T) {
	eachMode(t, testGeneratedGoActionContextAndExpressions)
}

func testGeneratedGoActionContextAndExpressions(t *testing.T) {
	grammar, err := parseTest(`type Result = { Text string }
rule main Result = (leaf / [a-z]) => Result{Text: "ok"}
rule leaf = "a"
`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := generateCode(grammar, GenOptions{Package: "hygiene", Start: "main", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	source := string(code)
	// Actions never nest and only an action's errors reach the parse-level handler that reads
	// p.Where, so a direct action names its rule and leaves it set.
	if !strings.Contains(source, `p.Where = "main"`) || strings.Contains(source, "p.Where = w") {
		t.Fatalf("direct typed action does not name its rule once:\n%s", source)
	}
	if strings.Contains(source, "p.Depth+0") {
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
	eachMode(t, testGeneratedDirectActionErrorContextAndParserReuse)
}

func testGeneratedDirectActionErrorContextAndParserReuse(t *testing.T) {
	cases := []genCase{{
		name: "typed/direct_action_error_context_and_reuse",
		src: `type Result = { N int }
rule main Result = child
rule child Result = x:"a"? => Result{N: 1 / len($x)}
`,
		inputs: []string{"", "a", "x"},
	}, {
		// The failing action runs after another rule's action succeeded.
		name: "typed/direct_action_error_context_after_action",
		src: `type Result = { N int }
rule main Result = f:first second => $f
rule first Result = x:"a" => Result{N: 1}
rule second Result = y:"b"? => Result{N: 1 / len($y)}
`,
		inputs: []string{"a", "ab", "b"},
	}}
	testGeneratedTypesCorpus(t, filepath.Join(runtime.GOROOT(), "bin", "go"), cases, GenOptions{convertTypes: true}, 1)
}
