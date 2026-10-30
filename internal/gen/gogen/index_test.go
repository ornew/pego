package gogen_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestIndexGeneratedGoParity(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testIndexGeneratedGoParity)
}

func testIndexGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	gentest.GeneratedParsersCorpus(t, goBin, gentest.IndexCases(), gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, gentest.IndexCases(), gen.GenOptions{})
}

// Generated parsers index the same with and without work limits, which select the
// work-accounting runtime.
func TestIndexGeneratedGoWork(t *testing.T) {
	t.Parallel()
	gentest.NeedsWorkControls(t)
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	g, err := parseTest(`type Out = { First node[string] }
rule main = xs:item+ $$ => Out{First: $xs[1]}
rule item = [a-z]`)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gentest.GoMod("indexwork")), 0644); err != nil {
		t.Fatal(err)
	}
	for _, types := range []bool{false, true} {
		pkg := fmt.Sprintf("index_%v", types)
		code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: pkg, Start: "main", Types: types})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, pkg)
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
		for name, data := range map[string][]byte{"parser.go": code, "index_test.go": []byte(strings.ReplaceAll(generatedGoIndexConsumer, "PACKAGE", pkg))} {
			if err := os.WriteFile(filepath.Join(path, name), data, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated indexing: %v\n%s", err, out)
	}
}

const generatedGoIndexConsumer = `package PACKAGE
import("fmt";"strings";"testing")
func TestIndex(t *testing.T){
 for _,opts:=range [][]ParseOption{nil,{WithLimits(Limits{MaxWork:1<<50})}}{
  n,err:=Parse("abc",opts...);if err!=nil || !strings.Contains(fmt.Sprintf("%+v",n),"b"){t.Fatal(n,err)}
  _,err=Parse("a",opts...);if err==nil || !strings.Contains(err.Error(),"index 1 out of range for 1 elements"){t.Fatal(err)}
 }
}
`
