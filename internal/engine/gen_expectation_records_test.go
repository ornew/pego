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
	return `type Token = { Text string }
rule main = ("s" &(wide "!") wide "!" / "m" (wide "!" / wide "?") / "r" (wide #recover(skip=[^;]+ ";"))+ / "e" (wide @error(message="expected a word"))) $$
rule wide Token = t:("é" (` + strings.Join(choices, " / ") + `)) #atomic => Token{Text: text($t)}`
}

func TestGeneratedExpectationRecords(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	g, err := syntax.Parse(`type N = node[string]
rule main N = ("é") #atomic`)
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("expectation_records_test.go")
	if err != nil {
		t.Fatal(err)
	}
	// The fixture is also an engine test; the generated runtime exports the names it uses.
	fixture = []byte(strings.NewReplacer("*parser", "*Parser", "&parser{", "&Parser{", "expID", "ExpID", "expMark", "ExpMark", "msgBit", "MsgBit",
		".keep(", ".Keep(", ".silent", ".Silent", ".mergeExpected(", ".MergeExpected(", ".unisolate(", ".Unisolate(", ".isolate(", ".Isolate(", ".expect(", ".Expect(").Replace(string(fixture)))
	for _, mode := range []string{"plain", "direct", "conversion"} {
		t.Run(mode, func(t *testing.T) {
			code, err := Generate(g, GenOptions{Package: "engine", Start: "main", Types: mode != "plain", convertTypes: mode == "conversion"})
			if err != nil {
				t.Fatal(err)
			}
			extra := `
func TestExpectationPoolReuse(t *testing.T) {
 p := &Parser{}
 for id := ExpID(0); id < 160; id++ { p.Expect(0,id) }
 p.release()
 if p.expBits != 0 || len(p.exp) != 0 { t.Fatal("plain release retained record") }
 for _,unit := range []PositionUnit{CodePoints,Bytes} {
  for i:=0;i<3;i++ {
   if _,err:=Parse("!",WithUnit(unit));err==nil { t.Fatal("invalid input accepted") }
   if n,err:=Parse("é",WithUnit(unit));err!=nil || n.RawText!="é" { t.Fatalf("plain reuse: %v",err) }
  }
 }
}
`
			if mode == "direct" {
				extra += `
func TestTypedExpectationPoolReuse(t *testing.T) {
 p := &TParser{Parser:&Parser{}}
 for id := ExpID(0); id < 160; id++ { p.Expect(0,id|MsgBit) }
 p.recycle()
 if p.expBits != 0 || len(p.exp) != 0 { t.Fatal("typed recycle retained record") }
 for _,unit := range []PositionUnit{CodePoints,Bytes} {
  for i:=0;i<3;i++ {
   if _,err:=ParseAST("!",WithUnit(unit));err==nil { t.Fatal("invalid input accepted") }
   if n,err:=ParseAST("é",WithUnit(unit));err!=nil || n.Value!="é" { t.Fatalf("typed reuse: %v",err) }
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
