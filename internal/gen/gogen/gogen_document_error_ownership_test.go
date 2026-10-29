package gogen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestGenerateDocumentDiagnosticOwnership(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGenerateDocumentDiagnosticOwnership)
}

func testGenerateDocumentDiagnosticOwnership(t *testing.T) {
	if testing.Short() {
		t.Skip("builds standalone generated parsers")
	}
	g, err := parseTest(`type Token = { Text string }
type Box = { Token Token | error; Any _ }
rule main []Box = xs:item* => $xs.Value
rule item Box = t:(token #recover(skip=[^;]+ ";")) => Box{Token: $t}
rule token Token = t:("a;" @error(message="want token")) #atomic => Token{Text: text($t)}`)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	export := os.Getenv("PEGO_DOCUMENT_DIAGNOSTIC_BENCH_DIR")
	if export != "" {
		dir = export
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gentest.GoMod("documentdiagnostics")), 0644); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []struct {
		name  string
		types bool
	}{{"node", false}, {"conversion", true}} {
		code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: variant.name, Start: "main", Document: true, Types: variant.types})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, variant.name)
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "parser.go"), code, 0644); err != nil {
			t.Fatal(err)
		}
		methods := `func(d *Document)(error){_,err:=d.Parse();return err},`
		if variant.types {
			methods += `func(d *Document)(error){_,err:=d.ParseAST();return err},`
		}
		source := strings.ReplaceAll(documentErrorOwnershipTest, "PACKAGE", variant.name)
		source = strings.ReplaceAll(source, "METHODS", methods)
		if err := os.WriteFile(filepath.Join(path, "parser_test.go"), []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
		if export != "" {
			benchmark := strings.ReplaceAll(documentDiagnosticBenchmark, "PACKAGE", variant.name)
			method := "Parse"
			if variant.types {
				method = "ParseAST"
			}
			benchmark = strings.ReplaceAll(benchmark, "METHOD", method)
			if err := os.WriteFile(filepath.Join(path, "diagnostics_bench_test.go"), []byte(benchmark), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Export identical fixtures from candidate and baseline runtimes. Baseline
	// ownership tests intentionally fail before the fix, so export does not run them.
	if export != "" {
		return
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod -trimpath")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated diagnostic ownership: %v\n%s", err, out)
	}
}

const documentErrorOwnershipTest = `package PACKAGE
import("reflect";"testing")
func TestRecoveredDiagnosticsAreOwned(t *testing.T){
 for _,unit:=range []PositionUnit{CodePoints,Bytes}{
  for _,parse:=range []func(*Document)error{METHODS}{
   d,err:=NewDocument("?é;",WithUnit(unit));if err!=nil{t.Fatal(err)}
   _,want:=Parse(d.Text(),WithUnit(unit))
   first:=parse(d)
   errors,ok:=first.(SyntaxErrors);if !ok||len(errors)!=1{t.Fatalf("recovery: %v",first)}
   e:=errors[0]
   e.Pos,e.Line,e.Col=-1,-1,-1
   if len(e.Expected)>0{e.Expected[0]="mutated"};e.Expected=append(e.Expected,"mutated")
   if len(e.Messages)>0{e.Messages[0]="mutated"};e.Messages=append(e.Messages,"mutated")
   errors[0]=nil
   second:=parse(d)
   if !reflect.DeepEqual(second,want){t.Fatalf("caller changed memo diagnostics: %#v / %#v",second,want)}
   retained:=second.(SyntaxErrors)
   // Editing and another parse must not overwrite a diagnostic already returned.
   end:=3;if unit==Bytes{end=4}
   if err:=d.Edit(0,end,"??;");err!=nil{t.Fatal(err)}
   _=parse(d)
   if !reflect.DeepEqual(SyntaxErrors(retained),want){t.Fatal("later parse changed retained diagnostics")}
  }
 }
}
func TestOrdinaryFailureIsOwned(t *testing.T){
 for _,unit:=range []PositionUnit{CodePoints,Bytes}{
  for _,parse:=range []func(*Document)error{METHODS}{
   d,err:=NewDocument("??",WithUnit(unit));if err!=nil{t.Fatal(err)}
   _,want:=Parse(d.Text(),WithUnit(unit))
   first:=parse(d)
   e,ok:=first.(*SyntaxError);if !ok{t.Fatalf("ordinary failure: %T",first)}
   e.Pos,e.Line,e.Col=-1,-1,-1
   if len(e.Expected)>0{e.Expected[0]="mutated"};e.Messages=[]string{"mutated"}
   if second:=parse(d);!reflect.DeepEqual(second,want){t.Fatalf("failure mutated memo: %v / %v",second,want)}
  }
 }
}
`

const documentDiagnosticBenchmark = `package PACKAGE
import("testing";"strings";"strconv")
var diagnosticBenchmarkError error
func BenchmarkDocumentRecovered(b *testing.B){
 for _,count:=range []int{1,128}{
  for _,unit:=range []struct{name string; unit PositionUnit}{{"CodePoints",CodePoints},{"Bytes",Bytes}}{
   b.Run(strconv.Itoa(count)+"/"+unit.name,func(b *testing.B){
    input:=strings.Repeat("?;",count)
    d,err:=NewDocument(input,WithUnit(unit.unit));if err!=nil{b.Fatal(err)}
    if _,err:=d.METHOD();err==nil{b.Fatal("missing recovered diagnostics")}
    b.ReportAllocs()
    for b.Loop(){
     _,err:=d.METHOD()
     if e,ok:=err.(SyntaxErrors);!ok||len(e)!=count{b.Fatal("invalid recovered result")}
     diagnosticBenchmarkError=err
    }
   })
  }
 }
}
`
