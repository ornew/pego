package gen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/gen"
	"github.com/ornew/pego/internal/ir/exec/core"
)

// TestGenerateGoEmbeddedPackageBuilds generates a parser with the runtime embedded, as pego gen does
// by default, and builds and runs it in a module that requires nothing: the copy of the runtime must
// compile on its own, and the parser must work with it.
func TestGenerateGoEmbeddedPackageBuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a generated parser")
	}
	g, err := pego.ParseGrammar(`type Pair = { Key node[string]; Value node[string] }
@start
rule main Pair = k:[a-z]+ #atomic "=" v:[0-9]+ #atomic => Pair{Key: $k, Value: $v}`)
	if err != nil {
		t.Fatal(err)
	}
	files, err := gen.Go{Module: "example.com/embedded/v2", Types: true, Recognize: true, Document: true, Observability: true}.Generate(g, "main")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var runtimeFiles int
	for _, f := range files {
		if strings.HasPrefix(f.Path, "internal/runtime/") {
			runtimeFiles++
		}
		path := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, f.Content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if runtimeFiles == 0 {
		t.Fatal("the embedded mode wrote no runtime")
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Without the runtime module in go.mod, the package can only build if it imports nothing of it.
	write("go.mod", "module example.com/embedded/v2\n\ngo 1.27.1\n")
	write("embedded_test.go", `package embedded

import "testing"

func TestEmbedded(t *testing.T) {
	v, err := ParseAST("ab=12")
	if err != nil {
		t.Fatal(err)
	}
	if v.Key.Value != "ab" || v.Value.Value != "12" {
		t.Errorf("got %+v", v)
	}
	n, err := Parse("ab=1")
	if err != nil || n == nil {
		t.Fatal(n, err)
	}
	if _, err := Parse("ab"); err == nil {
		t.Error("an incomplete pair was accepted")
	}
}
`)
	for _, args := range [][]string{{"vet", "./..."}, {"test", "-count=1", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS="+core.GoFlags("-mod=mod -trimpath"), "GOWORK=off")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", args[0], err, out)
		}
	}
}
