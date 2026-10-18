package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func TestGenerateTypedDocument(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a standalone generated parser")
	}
	g, err := syntax.Parse(typedFilterIdentityGrammar)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Generate(g, GenOptions{Package: "fixture", Start: "main", Types: true, Document: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(code), "func (d *Document) ParseAST(") || !strings.Contains(string(code), "tparse(trules") {
		t.Fatal("typed Document conversion or existing direct batch path missing")
	}
	dir := t.TempDir()
	for name, source := range map[string]string{
		"go.mod":    "module typeddocument\n\ngo 1.27.1\n",
		"parser.go": string(code),
		"parser_test.go": `package fixture
import (
 "context"
 "errors"
 "reflect"
 "testing"
)
func TestTypedDocumentResultAndOwnership(t *testing.T) {
 for _, unit := range []PositionUnit{CodePoints, Bytes} {
  calls := 0
  d, err := NewDocument("é", func(o *parseOptions) { calls++; o.unit = unit })
  if err != nil || calls != 1 { t.Fatalf("construction: %v, calls %d", err, calls) }
  old, err := d.ParseAST()
  fresh, freshErr := ParseAST("é", WithUnit(unit))
  if err != nil || freshErr != nil || !reflect.DeepEqual(old, fresh) { t.Fatalf("initial: %v / %v", err, freshErr) }
  if old.First == nil || len(old.Items) != 2 || old.First != old.Items[0] || old.First != old.Items[1] { t.Fatal("typed aliases were lost") }
  if len(old.Optional) != 4 || old.Optional[0] != nil || old.Optional[1] != old.First || old.Optional[2] != nil || old.Optional[3] != old.First { t.Fatal("optional aliases changed") }
  end := 1
  if unit == Bytes { end = 2 }
  if err := d.Edit(0, end, "b"); err != nil { t.Fatal(err) }
  next, err := d.ParseAST()
  fresh, freshErr = ParseAST("b", WithUnit(unit))
  if err != nil || freshErr != nil || !reflect.DeepEqual(next, fresh) { t.Fatalf("edited: %v / %v", err, freshErr) }
  if old.First.Text != "é" || old.Items[0] != old.First || next.First.Text != "b" { t.Fatal("later edit changed a retained typed value") }
  if calls != 1 { t.Fatalf("constructor option re-evaluated: %d", calls) }
  invocationCalls := 0
  _, err = d.ParseAST(func(o *parseOptions) { invocationCalls++; o.unit = unit })
  if err != nil || invocationCalls != 1 { t.Fatalf("invocation option: %v, calls %d", err, invocationCalls) }
  if _, err := d.ParseAST(WithUnit(PositionUnit(1-int(unit)))); err == nil { t.Fatal("unit change accepted") }
  if _, err := d.ParseAST(WithMaxDepth(-1)); err == nil { t.Fatal("negative depth accepted") }
  cancelled, cancel := context.WithCancel(context.Background()); cancel()
  if v, err := d.ParseAST(WithContext(cancelled)); v != nil || !errors.Is(err, context.Canceled) { t.Fatalf("cancellation: %v %v", v, err) }
  if v, err := d.ParseAST(); v == nil || err != nil { t.Fatalf("retry: %v", err) }
 }
}

func TestTypedDocumentConversionBudget(t *testing.T) {
 d, err := NewDocument("é")
 if err != nil { t.Fatal(err) }
 // The Node parse can reuse its result, while typed conversion must still be
 // charged to the same invocation controller and abandon interrupted work.
 if _, err := d.Parse(); err != nil { t.Fatal(err) }
 if v, err := d.ParseAST(WithWorkLimits(WorkLimits{MaxSteps: 1})); v != nil || !errors.Is(err, ErrWorkLimit) { t.Fatalf("conversion budget: %v %v", v, err) }
 if v, err := d.ParseAST(); v == nil || v.First == nil || err != nil { t.Fatalf("retry after conversion interruption: %v", err) }
}
`,
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod -trimpath")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated typed Document build/test: %v\n%s", err, out)
	}
}

func TestGenerateTypedDocumentNilAndRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("builds standalone generated parsers")
	}
	g, err := syntax.Parse(namedASTGrammar)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module typeddocument\n\ngo 1.27.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct{ pkg, start string }{{"optional", "optional"}, {"recovery", "recoverTerminal"}} {
		code, err := Generate(g, GenOptions{Package: entry.pkg, Start: entry.start, Types: true, Document: true})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, entry.pkg)
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "parser.go"), code, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const consumer = `package typeddocument_test
import (
 "reflect"
 "testing"
 optional "typeddocument/optional"
 recovery "typeddocument/recovery"
)
func isRecoveryError(v any) bool { _, ok := v.(*recovery.Error); return ok }
func TestNilAndRecovery(t *testing.T) {
 for _, unit := range []optional.PositionUnit{optional.CodePoints, optional.Bytes} {
  d, err := optional.NewDocument("", optional.WithUnit(unit))
  if err != nil { t.Fatal(err) }
  if value, err := d.ParseAST(); value != nil || err != nil { t.Fatalf("nil success: %v %v", value, err) }
  if err := d.Edit(0, 0, "é"); err != nil { t.Fatal(err) }
  got, gotErr := d.ParseAST()
  want, wantErr := optional.ParseAST("é", optional.WithUnit(unit))
  if gotErr != nil || wantErr != nil || !reflect.DeepEqual(got, want) { t.Fatalf("edited optional: %v / %v", gotErr, wantErr) }
 }
 for _, unit := range []recovery.PositionUnit{recovery.CodePoints, recovery.Bytes} {
  d, err := recovery.NewDocument("?", recovery.WithUnit(unit))
  if err != nil { t.Fatal(err) }
  got, gotErr := d.ParseAST()
  want, wantErr := recovery.ParseAST("?", recovery.WithUnit(unit))
  if _, ok := gotErr.(recovery.SyntaxErrors); !ok || !reflect.DeepEqual(got, want) || got == nil || !isRecoveryError(got.T) || gotErr.Error() != wantErr.Error() {
   t.Fatalf("recovered result: %#v / %#v, %v / %v", got, want, gotErr, wantErr)
  }
  if err := d.Edit(0, 1, "é"); err != nil { t.Fatal(err) }
  got, gotErr = d.ParseAST()
  want, wantErr = recovery.ParseAST("é", recovery.WithUnit(unit))
  if gotErr != nil || wantErr != nil || !reflect.DeepEqual(got, want) { t.Fatalf("recovered retry: %v / %v", gotErr, wantErr) }
 }
}

`
	if err := os.WriteFile(filepath.Join(dir, "typed_document_test.go"), []byte(consumer), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod -trimpath")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated typed Document nil/recovery build/test: %v\n%s", err, out)
	}
}

func TestGenerateTypedDocumentNameCollisions(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a standalone generated parser")
	}
	g, err := syntax.Parse(`type Document terminal
type NewDocument terminal
type ParseStats terminal
rule main Document = "a"`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Generate(g, GenOptions{Package: "fixture", Start: "main", Types: true, Document: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Document_", "NewDocument_", "ParseStats_"} {
		if !strings.Contains(string(code), "type "+name+" struct") {
			t.Fatalf("generated type %s collides with Document API", name)
		}
	}
	dir := t.TempDir()
	for name, source := range map[string]string{
		"go.mod":    "module typedcollision\n\ngo 1.27.1\n",
		"parser.go": string(code),
		"parser_test.go": `package fixture
import "testing"
func TestCollision(t *testing.T) {
 d, err := NewDocument("a")
 if err != nil { t.Fatal(err) }
 got, err := d.ParseAST()
 var typed *Document_ = got
 if err != nil || typed.Text != "a" { t.Fatal(got, err) }
 var _ *NewDocument_
 var _ *ParseStats_
}
`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod -trimpath")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated typed Document collision build/test: %v\n%s", err, out)
	}
}

func TestGenerateObservedDocumentNameCollisions(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a standalone generated parser")
	}
	g, err := syntax.Parse(`type ErrDocumentBusy terminal
rule main ErrDocumentBusy = "a"`)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := Generate(g, GenOptions{Package: "fixture", Start: "main", Types: true, Document: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plain), "type ErrDocumentBusy struct") {
		t.Fatal("unobserved Document renamed a grammar type it does not clash with")
	}
	code, err := Generate(g, GenOptions{Package: "fixture", Start: "main", Types: true, Document: true, Observability: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(code), "type ErrDocumentBusy_ struct") {
		t.Fatal("generated type ErrDocumentBusy collides with the observed Document API")
	}
	dir := t.TempDir()
	for name, source := range map[string]string{
		"go.mod":    "module observedcollision\n\ngo 1.27.1\n",
		"parser.go": string(code),
		"parser_test.go": `package fixture
import ("errors"; "testing")
func TestCollision(t *testing.T) {
 d, err := NewDocument("a")
 if err != nil { t.Fatal(err) }
 got, err := d.ParseAST()
 var typed *ErrDocumentBusy_ = got
 if err != nil || typed.Text != "a" { t.Fatal(got, err) }
 var inner error
 d.ParseAST(WithTrace(func(TraceEvent) { if inner == nil { _, inner = d.ParseAST(WithMaxDepth(-1)) } }))
 if !errors.Is(inner, ErrDocumentBusy) { t.Fatal("busy Document checked options first", inner) }
}
`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod -trimpath")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated observed Document collision build/test: %v\n%s", err, out)
	}
}
