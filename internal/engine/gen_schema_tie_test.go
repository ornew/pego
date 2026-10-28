package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// The parser package of a typed parser reinterprets a string node of a named node type as the type of
// the types package (rt.TMatchAs), which is sound only for the types package generated with the parser.
// The parser refers to a constant of the types package whose name carries the digest of that package, so
// a parser built against another types package does not compile, however the packages differ.
const schemaTieGrammar = `package p

type Word = node[string]
type Pair = { Left Word; Right Word }

@start
rule main Pair = l:word " " r:word => Pair{Left: $l, Right: $r}
rule word Word = [a-z]+ #atomic
`

// schemaTieExtra declares one more type than schemaTieGrammar and nothing else differs, so a parser of
// schemaTieGrammar compiles against the types package of schemaTieExtra except for the schema constant.
const schemaTieExtra = `package p

type Word = node[string]
type Pair = { Left Word; Right Word }
type Extra = node[string]

@start
rule main Pair = l:word " " r:word => Pair{Left: $l, Right: $r}
rule word Word = [a-z]+ #atomic
`

func schemaTiePair(t *testing.T, root, name, src string) {
	t.Helper()
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	generatePackageIn(t, root, "gentest", name, g, GenOptions{Start: "main", Types: true})
}

func schemaTieBuild(t *testing.T, root string) (string, error) {
	t.Helper()
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	cmd := exec.Command(goBin, "build", "-trimpath", "./...")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestGeneratedSchemaTie(t *testing.T) { eachMode(t, testGeneratedSchemaTie) }

func testGeneratedSchemaTie(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated packages")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(testGoMod("gentest")), 0o644); err != nil {
		t.Fatal(err)
	}
	schemaTiePair(t, root, "one", schemaTieGrammar)
	schemaTiePair(t, root, "two", schemaTieExtra)

	parser, err := os.ReadFile(filepath.Join(root, "one", "parser.go"))
	if err != nil {
		t.Fatal(err)
	}
	tie := regexp.MustCompile(`types\.PegoSchema_[0-9a-f]{16}\b`)
	if n := len(tie.FindAll(parser, -1)); n != 1 {
		t.Fatalf("parser.go refers to the schema constant %d times, want once", n)
	}
	if strings.Contains(string(parser), "unsafe") {
		t.Error("parser.go imports unsafe, the tie replaces the size assertions")
	}

	// Negative control: the pairs compile as generated.
	if out, err := schemaTieBuild(t, root); err != nil {
		t.Fatalf("matching pairs do not compile: %v\n%s", err, out)
	}

	// A parser with the types package of another schema does not compile, though every name it uses is
	// declared with the same type.
	other, err := os.ReadFile(filepath.Join(root, "two", "types", "types.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "one", "types", "types.go"), []byte(strings.ReplaceAll(string(other), "gentest/two", "gentest/one")), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := schemaTieBuild(t, root)
	if err == nil {
		t.Fatal("a parser with the types package of another schema compiles")
	}
	t.Logf("build of the mismatched pair:\n%s", out)
	if !strings.Contains(out, "PegoSchema_") {
		t.Errorf("the build fails, but not on the schema constant:\n%s", out)
	}
}

// The digest does not depend on comments: a grammar that differs only in its comments yields the same tie.
func TestGeneratedSchemaIgnoresComments(t *testing.T) {
	name := func(src string) string {
		g, err := parseTest(src)
		if err != nil {
			t.Fatal(err)
		}
		files, err := Generate(g, GenOptions{Start: "main", Package: "p", Module: "gentest/p", Types: true, Runtime: RuntimeImport})
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if f.Path == "types/types.go" {
				return string(regexp.MustCompile(`PegoSchema_[0-9a-f]{16}`).Find(f.Content))
			}
		}
		t.Fatal("no types package")
		return ""
	}
	a := name(schemaTieGrammar)
	b := name("// Documentation.\n" + strings.Replace(schemaTieGrammar, "type Pair", "// A pair.\ntype Pair", 1))
	c := name(schemaTieExtra)
	if a == "" || a != b {
		t.Errorf("comments change the schema constant: %q, %q", a, b)
	}
	if a == c {
		t.Errorf("an added type keeps the schema constant %q", a)
	}
}
