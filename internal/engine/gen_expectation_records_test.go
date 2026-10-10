package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// Large failure sets traverse memo, lookahead, label and recovery scopes;
// generated parsers compare complete public values and diagnostics.
func wideExpectationsGrammar() string {
	var choices []string
	for i := 0; i < 80; i++ {
		choices = append(choices, fmt.Sprintf("%q", fmt.Sprintf("%02d", i)))
	}
	return `type Token terminal
def main = ("s" &(wide "!") wide "!" / "m" (wide "!" / wide "?") / "r" (wide #recover(skip=(?^;)+ ";"))+ / "e" (wide #error(message="expected a word"))) $$
def wide: Token = @("é" (` + strings.Join(choices, " / ") + `))`
}

func TestGeneratedExpectationRecords(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	g, err := syntax.Parse(`type N terminal
def main: N = "é"`)
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("expectation_records_test.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"plain", "direct", "conversion"} {
		t.Run(mode, func(t *testing.T) {
			code, err := Generate(g, GenOptions{Package: "engine", Start: "main", Types: mode != "plain", convertTypes: mode == "conversion"})
			if err != nil {
				t.Fatal(err)
			}
			extra := `
func TestExpectationPoolReuse(t *testing.T) {
 p := &parser{}
 for id := expID(0); id < 160; id++ { p.expect(0,id) }
 p.release()
 if p.expBits != 0 || len(p.exp) != 0 { t.Fatal("plain release retained record") }
 for _,unit := range []Unit{CodePoints,Bytes} {
  for i:=0;i<3;i++ {
   if _,err:=Parse("!",unit);err==nil { t.Fatal("invalid input accepted") }
   if n,err:=Parse("é",unit);err!=nil || n.Text!="é" { t.Fatalf("plain reuse: %v",err) }
  }
 }
}
`
			if mode == "direct" {
				extra += `
func TestTypedExpectationPoolReuse(t *testing.T) {
 p := &tparser{parser:&parser{}}
 for id := expID(0); id < 160; id++ { p.expect(0,id|msgBit) }
 p.recycle()
 if p.expBits != 0 || len(p.exp) != 0 { t.Fatal("typed recycle retained record") }
 for _,unit := range []Unit{CodePoints,Bytes} {
  for i:=0;i<3;i++ {
   if _,err:=ParseAST("!",unit);err==nil { t.Fatal("invalid input accepted") }
   if n,err:=ParseAST("é",unit);err!=nil || n.Text!="é" { t.Fatalf("typed reuse: %v",err) }
  }
 }
}
`
			}
			dir := t.TempDir()
			for name, data := range map[string][]byte{"parser.go": code, "records_test.go": append(append([]byte(nil), fixture...), []byte(extra)...), "go.mod": []byte("module records\n\ngo 1.27.1\n")} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("go", "test", "-count=1", "./...")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		})
	}
}
