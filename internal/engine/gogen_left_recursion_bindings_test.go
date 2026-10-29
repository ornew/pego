package engine

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
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/value"
)

// typedLRBindingSource is lrBindingSource with typed values, so that ParseAST runs the typed
// runtime: step binds k before its left call of R, which reads k in seed.
const typedLRBindingSource = `type Doc = { N int }
var k int
@start
rule main Doc = e:R $$ => $e
rule R Doc = step / seed
rule step Doc = {k := k + 1} p:R "x" => Doc{N: $p.N + 1}
rule seed Doc = {k < 1000} "x" => Doc{N: 1}
`

var lrBindingInputs = []string{"x", "xxx", "xxxxxxxxxx", "", "y"}

// TestGeneratedLeftRecursionGuardIgnoresVariables is TestLeftRecursionGuardIgnoresVariables
// for generated Go parsers: Parse, Recognize and the typed ParseAST of a left-recursive rule
// re-entered with other variable values match the engine instead of nesting until the limit.
func TestGeneratedLeftRecursionGuardIgnoresVariables(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedLeftRecursionGuardIgnoresVariables)
}

func testGeneratedLeftRecursionGuardIgnoresVariables(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gentest.GoMod("gentest")), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := gen.GenOptions{Start: "main", Recognize: true, MaxDepth: 400, LeftRecursionBindings: true}
	gentest.GeneratePackage(t, dir, "g", mustParse(lrBindingSource), opts)
	opts.Types = true
	if code := gentest.GeneratePackage(t, dir, "tg", mustParse(typedLRBindingSource), opts); !strings.Contains(code, "TParse(trules") {
		t.Fatal("typed runtime is not exercised")
	}
	prog := compileBare(t, lrBindingSource, compile.Options{LeftRecursionBindings: true})
	typed := compileBare(t, typedLRBindingSource, compile.Options{LeftRecursionBindings: true})
	controls := gentest.FixtureWorkControls(t)
	var limited, typedLimited string
	if controls {
		limited, typedLimited = "g.WithWorkLimits(g.WorkLimits{MaxSteps: 1 << 40})", "tg.WithWorkLimits(tg.WorkLimits{MaxSteps: 1 << 40})"
	}
	var want strings.Builder
	var quoted []string
	for _, in := range lrBindingInputs {
		quoted = append(quoted, strconv.Quote(in))
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			o := ParseOptions{Unit: unit, MaxDepth: 400}
			n, err := prog.ParseWith("main", in, o)
			_, recErr := prog.ParseWith("main", in, ParseOptions{Unit: unit, MaxDepth: 400, Recognize: true})
			v, typedErr := typed.ParseWith("main", in, o)
			line := fmt.Sprintf("%s %v %v | %v\n", gentest.ResultJSON(n, err), recErr, typedErr, typedN(v))
			want.WriteString(line)
			if controls {
				// The same under a step limit, which takes the controlled memo lookups.
				want.WriteString(line)
			}
		}
	}
	limitedRun := ""
	if controls {
		limitedRun = "\n\t\t\trun(in, []g.ParseOption{g.WithUnit(u), " + limited + "}, []tg.ParseOption{tg.WithUnit(tu), " + typedLimited + "})"
	}
	main := `package main

import (
	"encoding/json"
	"fmt"

	"gentest/g"
	"gentest/tg"
)

func run(in string, o []g.ParseOption, to []tg.ParseOption) {
	n, err := g.Parse(in, o...)
	out := map[string]any{"node": n}
	if err != nil {
		out["err"] = err.Error()
	}
	b, _ := json.Marshal(out)
	recErr := g.Recognize(in, o...)
	v, typedErr := tg.ParseAST(in, to...)
	fmt.Printf("%s %v %v | ", b, recErr, typedErr)
	if v == nil {
		fmt.Println("<nil>")
	} else {
		fmt.Println(v.N)
	}
}

func main() {
	for _, in := range []string{` + strings.Join(quoted, ", ") + `} {
		for _, bytes := range []bool{false, true} {
			u, tu := g.CodePoints, tg.CodePoints
			if bytes {
				u, tu = g.Bytes, tg.Bytes
			}
			run(in, []g.ParseOption{g.WithUnit(u)}, []tg.ParseOption{tg.WithUnit(tu)})` + limitedRun + `
		}
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(goBin, "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	if string(out) != want.String() {
		t.Fatalf("generated\n%s\nengine\n%s", out, want.String())
	}
	if !strings.Contains(want.String(), "| 10\n") {
		t.Fatalf("the engine does not grow the typed seed:\n%s", want.String())
	}
}

// typedN returns the field N of an engine value of typedLRBindingSource.
func typedN(n *value.Node) string {
	if n == nil {
		return "<nil>"
	}
	return fmt.Sprint(n.Field("N"))
}

// TestGeneratedTSLeftRecursionGuardIgnoresVariables is TestLeftRecursionGuardIgnoresVariables
// for generated TypeScript parsers.
func TestGeneratedTSLeftRecursionGuardIgnoresVariables(t *testing.T) {
	t.Parallel()
	var script strings.Builder
	script.WriteString("import { parse, recognize } from \"./parser.ts\";\n")
	var want strings.Builder
	prog := compileBare(t, lrBindingSource, compile.Options{LeftRecursionBindings: true})
	for _, in := range lrBindingInputs {
		fmt.Fprintf(&script, "{ const r = parse(%q); const e = recognize(%[1]q); console.log(r.error === null ? String(r.node) : \"error: \" + r.error.message, e === null ? \"null\" : e.message); }\n", in)
		n, err := prog.ParseWith("main", in, ParseOptions{MaxDepth: 400})
		_, recErr := prog.ParseWith("main", in, ParseOptions{MaxDepth: 400, Recognize: true})
		if err != nil {
			fmt.Fprintf(&want, "error: %v %v\n", err, recErr)
		} else {
			fmt.Fprintf(&want, "%s null\n", n.String())
		}
	}
	out := runTSGrammarScriptWith(t, mustParse(lrBindingSource), script.String(), gen.GenOptions{Start: "main", Recognize: true, MaxDepth: 400, LeftRecursionBindings: true})
	if out != want.String() {
		t.Fatalf("generated\n%s\nengine\n%s", out, want.String())
	}
}
