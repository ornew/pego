package gen_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// Generated parsers retain negative maxima directly in their implementation-int
// loops. Check their accepted hand-built AST contract beside bytecode lowering.
func TestGeneratedNegativeMaximum(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedNegativeMaximum)
}

func testGeneratedNegativeMaximum(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated parsers")
	}
	large := int64(-1) << 32
	maximum := int(large)
	if strconv.IntSize < 64 {
		maximum = -2
	}
	g := gentest.GrammarWithNegativeMaximum(t, `rule main []node[string] = v1:"é"* $$ => $v1.Value`, maximum)
	p, err := engine.Compile(g, compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var expected []string
	for _, input := range []string{"", "é", "ééé", "x", "éx"} {
		for _, unit := range []engine.PositionUnit{engine.CodePoints, engine.Bytes} {
			n, err := p.ParseWith("main", input, engine.ParseOptions{Unit: unit, Expectations: true})
			expected = append(expected, gentest.ResultJSON(n, err))
		}
	}
	want := strings.Join(expected, "\n") + "\n"
	for _, typed := range []bool{false, true} {
		t.Run(fmt.Sprintf("Go typed=%v", typed), func(t *testing.T) {
			code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "main", Start: "main", Types: typed})
			if err != nil {
				t.Fatal(err)
			}
			astCheck := ""
			if typed {
				astCheck = `ast, ae := ParseAST(input, WithUnit(unit)); if fmt.Sprint(ae) != fmt.Sprint(err) || err == nil && len(ast) != len([]rune(input)) { panic("typed negative-maximum result differs") }`
			}
			harness := `package main
import ("encoding/json";"fmt")
func main() {
 for _, input := range []string{"","é","ééé","x","éx"} {
  for _, unit := range []PositionUnit{CodePoints,Bytes} {
   n, err := Parse(input,WithUnit(unit))
   ` + astCheck + `
   out := map[string]any{"node":n}
   if err != nil {out["err"]=err.Error()}
   data,_ := json.Marshal(out);fmt.Println(string(data))
  }
 }
}`
			dir := t.TempDir()
			for name, content := range map[string]string{"go.mod": gentest.GoMod("negative"), "parser.go": string(code), "main.go": harness} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
			out, err := cmd.CombinedOutput()
			if err != nil || string(out) != want {
				t.Fatalf("generated Go: %v\ngot %s\nwant %s", err, out, want)
			}
		})
	}
	t.Run("TypeScript", func(t *testing.T) {
		got := gentest.RunTSGrammarScript(t, g, `import { parse, CodePoints, Bytes } from "./parser.ts";
for (const input of ["","é","ééé","x","éx"]) {
 for (const unit of [CodePoints, Bytes]) {
  const result=parse(input,unit);
  const out: Record<string,unknown>={};
  if(result.error) out["err"]=result.error.message;
  out["node"]=result.node;
  console.log(JSON.stringify(out));
 }
}`)
		if got != want {
			t.Fatalf("generated TS:\ngot %s\nwant %s", got, want)
		}
	})
}
