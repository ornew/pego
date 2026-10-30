package gogen_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// Go string literals preserve invalid bytes; JSON input fixtures would replace
// them before the generated parser sees them.
func TestGeneratedInvalidUTF8Literals(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedInvalidUTF8Literals)
}

func testGeneratedInvalidUTF8Literals(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated parsers")
	}
	dir := t.TempDir()
	write := func(name string, content []byte) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", []byte(gentest.GoMod("literaltest")))
	var imports, want strings.Builder
	var blocks []string
	i := 0
	for _, tc := range gentest.InvalidLiteralCorpus() {
		g := gentest.LiteralCorpusGrammar(t, tc)
		prog, err := engine.Compile(g, compile.Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range []string{"untyped", "direct", "convert"} {
			pkg := fmt.Sprintf("g%d", i)
			i++
			opts := gen.GenOptions{Package: pkg, Start: "main", Recognize: true, Types: mode != "untyped", ConvertTypes: mode == "convert"}
			code, err := gentest.GenerateCode(t, g, opts)
			if err != nil {
				t.Fatal(err)
			}
			write(pkg+"/parser.go", code)
			fmt.Fprintf(&imports, "%s %q\n", pkg, "literaltest/"+pkg)
			for _, input := range tc.Inputs {
				for _, unit := range []engine.PositionUnit{engine.CodePoints, engine.Bytes} {
					var harness strings.Builder
					n, err := prog.ParseWith("main", input, engine.ParseOptions{Expectations: true, Unit: unit})
					want.WriteString(gentest.ResultJSON(n, err) + "\n")
					fmt.Fprintf(&harness, "{ input:=%q; unit:=%s.%s; n,err:=%s.Parse(input,%[4]s.WithUnit(unit)); rec:=%s.Recognize(input,%[4]s.WithUnit(unit)); if fmt.Sprint(rec)!=fmt.Sprint(err){panic(\"recognition differs\")}\n", input, pkg, map[engine.PositionUnit]string{engine.CodePoints: "CodePoints", engine.Bytes: "Bytes"}[unit], pkg, pkg)
					if mode != "untyped" {
						fmt.Fprintf(&harness, `ast,ae:=%s.ParseAST(input,%[1]s.WithUnit(unit)); if fmt.Sprint(ae)!=fmt.Sprint(err){panic("typed error differs")}; if err==nil {child:=n.Field("T").(*%s.Node); wantText:=input; if unit==%s.CodePoints{wantText=string([]rune(input))};if child.Text()!=wantText{panic("matched text differs from input")}; if ast==nil || ast.T==nil || ast.T.Start!=child.Start() || ast.T.End!=child.End() || ast.T.Value!=child.Text() {panic("typed literal value differs")}}`+"\n", pkg, pkg, pkg)
					}
					harness.WriteString(`out:=map[string]any{"node":n};if err!=nil{out["err"]=err.Error()};data,_:=json.Marshal(out);fmt.Println(string(data))}` + "\n")
					blocks = append(blocks, harness.String())
				}
			}
		}
	}
	// One function holding every block takes the compiler about a minute; functions of a bounded
	// number of blocks build in about a second.
	const blocksPerFunc = 40
	var harness, calls strings.Builder
	for i := 0; i < len(blocks); i += blocksPerFunc {
		fmt.Fprintf(&harness, "func f%d(){\n%s}\n", i/blocksPerFunc, strings.Join(blocks[i:min(i+blocksPerFunc, len(blocks))], ""))
		fmt.Fprintf(&calls, "f%d()\n", i/blocksPerFunc)
	}
	harness.WriteString("func main(){\n" + calls.String() + "}\n")
	write("main.go", []byte("package main\nimport(\"encoding/json\";\"fmt\";\n"+imports.String()+")\n"+harness.String()))
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != want.String() {
		t.Fatalf("generated Go: %v\ngot %s\nwant %s", err, out, want.String())
	}
}
