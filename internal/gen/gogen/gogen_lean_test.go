package gogen_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gen/gogen"
	"github.com/ornew/pego/internal/gentest"
)

// External rule-table entry and nested direct calls must count the same rule
// depth. A small generated limit exercises both routes without large inputs.
func TestGeneratedLeanDepth(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedLeanDepth)
}

func testGeneratedLeanDepth(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	const src = `var n int
rule main = one
rule one = two
rule two = three
rule three = four
rule four = five
rule five = "é" / "�" / "😀"
rule cap = x:"é" {text($x) == "é"}
rule bind = {n := 1} "é"
rule cut = "é" #cut "!"
rule recovering = "é" #recover(skip="?")
rule expression = #operators {
    operand "é"
    level { prefix "-" }
}
rule left = left "+" "é" / "é"
`
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	// The depth comparison below runs the recognizer table (recRules), so the route is checked there.
	taken := map[string]bool{}
	gh := &gogen.Hooks{}
	gentest.SetGenHooks(t, gh)
	gh.LeanRoute = func(table string, r *compile.Rule) { taken[table+"/"+r.Name] = true }
	code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "depth", Start: "main", Recognize: true})
	gh.LeanRoute = nil
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"main", "one", "two", "three", "four", "five"} {
		if !taken["recRules/"+name] {
			t.Fatalf("no direct route for %s in recRules", name)
		}
	}
	for _, name := range []string{"cap", "bind", "cut", "recovering", "expression", "left"} {
		if taken["recRules/"+name] {
			t.Fatalf("unsupported direct route for %s in recRules", name)
		}
	}
	if !strings.Contains(string(code), "var pegoGrammar = rt.NewGrammar(100000)") {
		t.Fatal("generated depth default not found")
	}
	code = []byte(strings.Replace(string(code), "var pegoGrammar = rt.NewGrammar(100000)", "var pegoGrammar = rt.NewGrammar(5)", 1))
	prog := mustCompile(t, src)
	var rows strings.Builder
	// Go literals preserve invalid UTF-8 bytes; the JSON-transport corpus would
	// replace them before generated parsers receive the input.
	for _, name := range []string{"main", "one", "two", "three", "four", "five", "cap", "bind", "cut", "recovering", "expression", "left"} {
		for _, input := range []string{"é", "?", "é!", "-é", "é+é", "", "\xff", "\xffé", "\xe2\x82", "�é", "😀"} {
			for _, unit := range []engine.PositionUnit{engine.CodePoints, engine.Bytes} {
				_, err := prog.ParseWith(name, input, engine.ParseOptions{Expectations: true, Unit: unit, MaxDepth: 5, Recognize: true})
				fmt.Fprintf(&rows, "{%q,%q,%s,%s},\n", name, input, map[engine.PositionUnit]string{engine.CodePoints: "CodePoints", engine.Bytes: "Bytes"}[unit], strconv.Quote(fmt.Sprint(err)))
			}
		}
	}
	tests := `package depth
import("fmt";"testing";pegort "github.com/ornew/pego/runtime")
func TestRoutesAndReuse(t *testing.T) {
 cases:=[]struct{name,input string; unit PositionUnit; want string}{` + rows.String() + `}
 for repeat:=0;repeat<3;repeat++ { for _,c:=range cases {
  var selected *pegort.Rule
  for _,r:=range recRules {if r.Name==c.name {selected=r;break}}
  if selected==nil {t.Fatal(c.name)}
  n,err:=pegort.RunParse(selected,c.input,[]ParseOption{WithExpectations(), WithUnit(c.unit)})
  if n!=nil {t.Fatalf("recognition built a value for %s",c.name)}
  if got:=fmt.Sprint(err);got!=c.want {t.Fatalf("%s %q unit%d: got %s want %s",c.name,c.input,c.unit,got,c.want)}
 }}
}
`
	dir := t.TempDir()
	for name, data := range map[string][]byte{"parser.go": code, "depth_test.go": []byte(tests), "go.mod": []byte(gentest.GoMod("depth"))} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
