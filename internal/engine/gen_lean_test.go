package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// External rule-table entry and nested direct calls must count the same rule
// depth. A small generated limit exercises both routes without large inputs.
func TestGeneratedLeanDepth(t *testing.T) {
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
rule assign = {n := 1} "é"
rule cut = "é" #cut "!"
rule recovering = "é" #recover(skip="?")
rule expression = operators {
    operand "é"
    level { prefix "-" }
}
rule left = left "+" "é" / "é"
`
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Generate(g, GenOptions{Package: "depth", Start: "main", Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"main", "one", "two", "three", "four", "five"} {
		if !strings.Contains(string(code), "// "+name+", called as by invokePlain (body inlined)") {
			t.Fatalf("no direct route for %s", name)
		}
	}
	for _, name := range []string{"cap", "assign", "cut", "recovering", "expression", "left"} {
		if strings.Contains(string(code), "// "+name+", called as by invokePlain (body inlined)") {
			t.Fatalf("unsupported direct route for %s", name)
		}
	}
	if !strings.Contains(string(code), "var pegoGrammar = NewGrammar(100000)") {
		t.Fatal("generated depth default not found")
	}
	code = []byte(strings.Replace(string(code), "var pegoGrammar = NewGrammar(100000)", "var pegoGrammar = NewGrammar(5)", 1))
	prog := compile(t, src)
	var rows strings.Builder
	// Go literals preserve invalid UTF-8 bytes; the JSON-transport corpus would
	// replace them before generated parsers receive the input.
	for _, name := range []string{"main", "one", "two", "three", "four", "five", "cap", "assign", "cut", "recovering", "expression", "left"} {
		for _, input := range []string{"é", "?", "é!", "-é", "é+é", "", "\xff", "\xffé", "\xe2\x82", "�é", "😀"} {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				_, err := prog.ParseWith(name, input, ParseOptions{Unit: unit, MaxDepth: 5, Recognize: true})
				fmt.Fprintf(&rows, "{%q,%q,%s,%s},\n", name, input, map[PositionUnit]string{CodePoints: "CodePoints", Bytes: "Bytes"}[unit], strconv.Quote(fmt.Sprint(err)))
			}
		}
	}
	tests := `package depth
import("fmt";"testing")
func TestRoutesAndReuse(t *testing.T) {
 cases:=[]struct{name,input string; unit PositionUnit; want string}{` + rows.String() + `}
 for repeat:=0;repeat<3;repeat++ { for _,c:=range cases {
  var selected *Rule
  for _,r:=range recRules {if r.Name==c.name {selected=r;break}}
  if selected==nil {t.Fatal(c.name)}
  n,err:=RunParse(selected,c.input,[]ParseOption{WithUnit(c.unit)})
  if n!=nil {t.Fatalf("recognition built a value for %s",c.name)}
  if got:=fmt.Sprint(err);got!=c.want {t.Fatalf("%s %q unit%d: got %s want %s",c.name,c.input,c.unit,got,c.want)}
 }}
}
`
	dir := t.TempDir()
	for name, data := range map[string][]byte{"parser.go": code, "depth_test.go": []byte(tests), "go.mod": []byte("module depth\n\ngo 1.27.1\n")} {
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
