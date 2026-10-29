package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestGeneratedExpectationRecords(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedExpectationRecords)
}

func testGeneratedExpectationRecords(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	g, err := parseTest(`type N = node[string]
rule main N = ("é") #atomic`)
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("core/expectation_records_test.go")
	if err != nil {
		t.Fatal(err)
	}
	// The fixture is also a test of the core package; the generated runtime exports the names it
	// uses except for the expectation record itself, which the runtime keeps unexported.
	fixture = []byte(strings.NewReplacer("package core", "package engine", "&Parser{", "&pegort.Parser{", "ExpID", "pegort.ExpID", "ExpMark", "pegort.ExpMark", "MsgBit", "pegort.MsgBit",
		"p.ExpBase", "p.expBase", "p.Expect(", "p.Expect(", "p.Exp", "p.exp", "p.Farthest", "p.farthest").Replace(string(fixture)))
	fixture = []byte(strings.Replace(string(fixture), "import (", "import (\n\tpegort \"github.com/ornew/pego/runtime\"", 1))
	for _, mode := range []string{"plain", "direct", "conversion"} {
		t.Run(mode, func(t *testing.T) {
			code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "engine", Start: "main", Types: mode != "plain", ConvertTypes: mode == "conversion"})
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
			for name, data := range map[string][]byte{"parser.go": code, "records_test.go": gentest.ExportNames(append(append([]byte(nil), fixture...), []byte(qualifyFixtureNames.Replace(extra))...)), "go.mod": []byte(gentest.GoMod("records"))} {
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

// qualifyFixtureNames qualifies the runtime names that the code of the fixtures above uses.
var qualifyFixtureNames = strings.NewReplacer("TParser", "pegort.TParser", "Parser:", "Parser:", "Parser", "pegort.Parser", "ExpID", "pegort.ExpID", "MsgBit", "pegort.MsgBit")
