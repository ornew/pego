package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A parser generated without work controls holds only the ordinary form of its functions, does not
// export the control options, and fails a parse that passes them instead of ignoring them.
func TestGeneratedWithoutWorkControls(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	const src = `rule main = item+ !.
rule item = word / num / " "
rule word = [a-zA-Z_ä-ö]+
rule num = [0-9]+ ("." [0-9]+)?
`
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	gen := func(workControls bool, document ...bool) string {
		files, err := Generate(g, GenOptions{Package: "wc", Module: "wc", Start: "main", Runtime: RuntimeImport, Types: true, Recognize: true, WorkControls: workControls, Document: len(document) > 0})
		if err != nil {
			t.Fatal(err)
		}
		return string(mergeGenerated(files))
	}
	with, without := gen(true), gen(false)
	for _, want := range []string{"p.StepWork()", "if p.Control == nil", "func WithWorkLimits", "func WithContext"} {
		if !strings.Contains(with, want) {
			t.Errorf("with work controls: missing %q", want)
		}
		if strings.Contains(without, want) {
			t.Errorf("without work controls: found %q", want)
		}
	}
	for _, bad := range []string{"p.Control", "_ordinary", "WorkClass", "WorkLimit"} {
		if strings.Contains(without, bad) {
			t.Errorf("without work controls: found %q", bad)
		}
	}
	if len(without)*3 >= len(with)*2 {
		t.Errorf("parser without work controls is %d bytes, with %d: want under two thirds", len(without), len(with))
	}
	// With a Document the parser keeps a second form of its functions for the Document, which has
	// neither work charges nor control reads, since the parser has no controls.
	documentWith, documentWithout := gen(true, true), gen(false, true)
	for _, bad := range []string{"p.Control", "StepWork", "InputWork", "WorkClass", "WorkLimit"} {
		if strings.Contains(documentWithout, bad) {
			t.Errorf("a Document parser without work controls has %q", bad)
		}
		if !strings.Contains(documentWith, bad) && bad != "WorkClass" {
			t.Errorf("a Document parser with work controls lacks %q: the check is void", bad)
		}
	}
	if !strings.Contains(documentWithout, "p.Document == nil") {
		t.Error("a Document parser without work controls has no ordinary form apart from the Document's")
	}
	t.Logf("sizes: with controls %d, without %d; with Document: with controls %d, without %d", len(with), len(without), len(documentWith), len(documentWithout))
	if len(documentWithout) >= len(documentWith) {
		t.Errorf("Document parser without work controls is %d bytes, with %d", len(documentWithout), len(documentWith))
	}
	tests := `package wc
import("strings";"testing";"context";pegort "github.com/ornew/pego/runtime")
func TestOptions(t *testing.T) {
 if _,err:=Parse("abc 12.5 x"); err!=nil {t.Fatal(err)}
 if err:=Recognize("abc 12.5 x"); err!=nil {t.Fatal(err)}
 if _,err:=Parse("abc ("); err==nil {t.Fatal("no error for bad input")}
 if _,err:=ParseAST("abc 12.5 x"); err!=nil {t.Fatal(err)}
 // A zero WorkLimits and a nil context set no control, so they are accepted.
 if _,err:=Parse("abc",pegort.WithWorkLimits(pegort.WorkLimits{}),pegort.WithContext(nil)); err!=nil {t.Fatal(err)}
 for name,opt:=range map[string]pegort.ParseOption{"limits":pegort.WithWorkLimits(pegort.WorkLimits{MaxSteps:1}),"context":pegort.WithContext(context.Background())} {
  for entry,call:=range map[string]func() error{
   "Parse":func() error {_,err:=Parse("abc",opt);return err},
   "Recognize":func() error {return Recognize("abc",opt)},
   "ParseAST":func() error {_,err:=ParseAST("abc",opt);return err},
   "ParseRule":func() error {_,err:=ParseRule("word","abc",opt);return err},
  } {
   err:=call()
   if err==nil || !strings.Contains(err.Error(),"without work controls") {t.Errorf("%s %s: got %v",entry,name,err)}
  }
 }
}
`
	dir := t.TempDir()
	for name, data := range map[string][]byte{"parser.go": []byte(without), "wc_test.go": []byte(tests), "go.mod": []byte(testGoMod("wc"))} {
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
