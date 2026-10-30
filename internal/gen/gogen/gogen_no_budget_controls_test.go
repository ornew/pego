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

// A parser generated without budget controls holds no code that spends budget or reads a control, does not
// export the control options, and fails a parse that passes them instead of ignoring them.
func TestGeneratedWithoutBudgetControls(t *testing.T) {
	t.Parallel()
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
	gen := func(budgetControls bool, document ...bool) string {
		files, err := gentest.Generate(t, g, gen.GenOptions{Package: "wc", Module: "wc", Start: "main", Runtime: gen.RuntimeImport, Types: true, Recognize: true, BudgetControls: budgetControls, Document: len(document) > 0})
		if err != nil {
			t.Fatal(err)
		}
		return string(gentest.MergeGenerated(files))
	}
	with, without := gen(true), gen(false)
	for _, want := range []string{"p.Spend()", "p.Control.Take(", "func WithLimits", "func WithContext"} {
		if !strings.Contains(with, want) {
			t.Errorf("with budget controls: missing %q", want)
		}
		if strings.Contains(without, want) {
			t.Errorf("without budget controls: found %q", want)
		}
	}
	for _, bad := range []string{"p.Control", "BudgetClass", "LimitBudget"} {
		if strings.Contains(without, bad) {
			t.Errorf("without budget controls: found %q", bad)
		}
	}
	// The helpers with an Ordinary form belong to the form without budget controls and Documents only.
	for name, text := range map[string]string{"with budget controls": with, "Document with budget controls": gen(true, true)} {
		if strings.Contains(text, "Ordinary(") {
			t.Errorf("%s: a call of an Ordinary helper", name)
		}
	}
	if len(without) >= len(with) {
		t.Errorf("parser without budget controls is %d bytes, with %d: want smaller", len(without), len(with))
	}
	// With a Document the parser has the form that serves a Document, which has neither budget charges
	// nor control reads when the parser has no controls.
	documentWith, documentWithout := gen(true, true), gen(false, true)
	for _, bad := range []string{"p.Control", ".Spend()", ".SpendRead(", "BudgetClass", "LimitBudget"} {
		if strings.Contains(documentWithout, bad) {
			t.Errorf("a Document parser without budget controls has %q", bad)
		}
		if !strings.Contains(documentWith, bad) && bad != "BudgetClass" {
			t.Errorf("a Document parser with budget controls lacks %q: the check is void", bad)
		}
	}
	if !strings.Contains(documentWithout, "p.Document == nil") {
		t.Error("a Document parser without budget controls has no in-place read guard")
	}
	t.Logf("sizes: with controls %d, without %d; with Document: with controls %d, without %d", len(with), len(without), len(documentWith), len(documentWithout))
	if len(documentWithout) >= len(documentWith) {
		t.Errorf("Document parser without budget controls is %d bytes, with %d", len(documentWithout), len(documentWith))
	}
	tests := `package wc
import("strings";"testing";"context";pegort "github.com/ornew/pego/runtime")
func TestOptions(t *testing.T) {
 if _,err:=Parse("abc 12.5 x"); err!=nil {t.Fatal(err)}
 if err:=Recognize("abc 12.5 x"); err!=nil {t.Fatal(err)}
 if _,err:=Parse("abc ("); err==nil {t.Fatal("no error for bad input")}
 if _,err:=ParseAST("abc 12.5 x"); err!=nil {t.Fatal(err)}
 // A zero Limits and a nil context set no control, so they are accepted.
 if _,err:=Parse("abc",pegort.WithLimits(pegort.Limits{}),pegort.WithContext(nil)); err!=nil {t.Fatal(err)}
 for name,opt:=range map[string]pegort.ParseOption{"limits":pegort.WithLimits(pegort.Limits{Budget:1}),"context":pegort.WithContext(context.Background())} {
  for entry,call:=range map[string]func() error{
   "Parse":func() error {_,err:=Parse("abc",opt);return err},
   "Recognize":func() error {return Recognize("abc",opt)},
   "ParseAST":func() error {_,err:=ParseAST("abc",opt);return err},
   "ParseRule":func() error {_,err:=ParseRule("word","abc",opt);return err},
  } {
   err:=call()
   if err==nil || !strings.Contains(err.Error(),"without budget controls") {t.Errorf("%s %s: got %v",entry,name,err)}
  }
 }
}
`
	dir := t.TempDir()
	for name, data := range map[string][]byte{"parser.go": []byte(without), "wc_test.go": []byte(tests), "go.mod": []byte(gentest.GoMod("wc"))} {
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
