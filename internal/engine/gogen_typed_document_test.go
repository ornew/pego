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

func TestGenerateTypedDocument(t *testing.T) {
	t.Parallel()
	gentest.NeedsWorkControls(t)
	if testing.Short() {
		t.Skip("builds a standalone generated parser")
	}
	g, err := parseTest(gentest.TypedFilterIdentityGrammar)
	if err != nil {
		t.Fatal(err)
	}
	code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "fixture", Start: "main", Types: true, Document: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(code), "func (d *Document) ParseAST(") || !strings.Contains(string(code), "TParse(trules") {
		t.Fatal("typed Document conversion or existing direct batch path missing")
	}
	dir := t.TempDir()
	for name, source := range map[string]string{
		"go.mod":    gentest.GoMod("typeddocument"),
		"parser.go": string(code),
		"parser_test.go": `package fixture
import (
	pegort "github.com/ornew/pego/runtime"
 "context"
 "errors"
 "reflect"
 "testing"
)
func TestTypedDocumentResultAndOwnership(t *testing.T) {
 for _, unit := range []PositionUnit{CodePoints, Bytes} {
  calls := 0
  d, err := NewDocument("é", func(o *pegort.X_parseOptions) { calls++; o.X_unit = unit })
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
  _, err = d.ParseAST(func(o *pegort.X_parseOptions) { invocationCalls++; o.X_unit = unit })
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
	t.Parallel()
	gentest.EachMode(t, testGenerateTypedDocumentNilAndRecovery)
}

func testGenerateTypedDocumentNilAndRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("builds standalone generated parsers")
	}
	// The union S | node does not generate in Document mode: its conversion returns any, which does
	// not implement the union interface (S | Seq did). This test is about nil results and
	// recovery, so the grammar uses a string node member instead.
	source := strings.NewReplacer("type Mixed ~ S | node[_]\n", "type Mixed ~ S | node[string]\n", "rule mixed Mixed = cst\n", "rule mixed Mixed = \"t\"\n").Replace(gentest.NamedASTGrammar)
	g, err := parseTest(source)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gentest.GoMod("typeddocument")), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct{ pkg, start string }{{"optional", "optional"}, {"recovery", "recoverTerminal"}} {
		gentest.GeneratePackageIn(t, dir, "typeddocument", entry.pkg, g, gen.GenOptions{Start: entry.start, Types: true, Document: true})
	}
	const consumer = `package typeddocument_test
import (
 "reflect"
 "testing"
 optional "typeddocument/optional"
 recovery "typeddocument/recovery"
 recoverytypes "typeddocument/recovery/types"
)
func isRecoveryError(v any) bool { _, ok := v.(*recoverytypes.Error); return ok }
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

// A union with an open member, such as node, has a Go interface of its own that the open
// values implement; a Document clones them through that interface.
func TestGenerateTypedDocumentOpenUnion(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGenerateTypedDocumentOpenUnion)
}

func testGenerateTypedDocumentOpenUnion(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a standalone generated parser")
	}
	g, err := parseTest(`type S = { A node[string] }
type Mixed ~ S | node[_]
type First ~ node[_] | S
type Out = { M []Mixed; F []First }
rule main Out = m:item* ";" f:first* => Out{M: concat($m), F: concat($f)}
rule item Mixed = s / c
rule first First = c / s
rule s S = a:"s" => S{A: $a}
rule c = "c" "d"`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "openunion", Start: "main", Types: true, Document: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":    gentest.GoMod("openunion"),
		"parser.go": string(code),
		"open_union_test.go": `package openunion
import (
 "reflect"
 "testing"
)
func TestOpenUnion(t *testing.T) {
 d, err := NewDocument("scds;cdscd")
 if err != nil { t.Fatal(err) }
 for _, edit := range []struct{ start, end int; text string }{{0, 0, ""}, {1, 1, "s"}, {6, 6, "cd"}, {0, 1, "cd"}} {
  if err := d.Edit(edit.start, edit.end, edit.text); err != nil { t.Fatal(err) }
  got, gotErr := d.ParseAST()
  want, wantErr := ParseAST(d.Text())
  if gotErr != nil || wantErr != nil || !reflect.DeepEqual(got, want) { t.Fatalf("%q: %#v / %#v, %v / %v", d.Text(), got, want, gotErr, wantErr) }
  for _, v := range append(append([]any{}, anys(got.M)...), anysF(got.F)...) {
   if v == nil { t.Fatalf("%q: a nil element", d.Text()) }
  }
 }
}
func anys(xs []Mixed) []any { out := []any{}; for _, x := range xs { out = append(out, x) }; return out }
func anysF(xs []First) []any { out := []any{}; for _, x := range xs { out = append(out, x) }; return out }
`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod -trimpath")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated typed Document with an open union: %v\n%s", err, out)
	}
}

func TestGenerateTypedDocumentNameCollisions(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGenerateTypedDocumentNameCollisions)
}

func testGenerateTypedDocumentNameCollisions(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a standalone generated parser")
	}
	g, err := parseTest(`type Document = { Text string }
type NewDocument = { Text string }
type ParseStats = { Text string }
rule main Document = t:("a") #atomic => Document{Text: text($t)}`)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	gentest.GeneratePackageIn(t, dir, "typedcollision", "fixture", g, gen.GenOptions{Start: "main", Types: true, Document: true})
	typesSource, err := os.ReadFile(filepath.Join(dir, "fixture", "types", "types.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Document", "NewDocument", "ParseStats"} {
		if !strings.Contains(string(typesSource), "type "+name+" struct") {
			t.Fatalf("generated type %s is not declared under its name", name)
		}
	}
	for name, source := range map[string]string{
		"go.mod": gentest.GoMod("typedcollision"),
		"fixture/parser_test.go": `package fixture
import ("testing"; "typedcollision/fixture/types")
func TestCollision(t *testing.T) {
 d, err := NewDocument("a")
 if err != nil { t.Fatal(err) }
 got, err := d.ParseAST()
 var typed *types.Document = got
 if err != nil || typed.Text != "a" { t.Fatal(got, err) }
 var _ *types.NewDocument
 var _ *types.ParseStats
 var _ *Document = d
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
	t.Parallel()
	gentest.EachMode(t, testGenerateObservedDocumentNameCollisions)
}

func testGenerateObservedDocumentNameCollisions(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a standalone generated parser")
	}
	g, err := parseTest(`type ErrDocumentBusy = { Text string }
rule main ErrDocumentBusy = t:("a") #atomic => ErrDocumentBusy{Text: text($t)}`)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	gentest.GeneratePackageIn(t, dir, "observedcollision", "plain", g, gen.GenOptions{Start: "main", Types: true, Document: true})
	gentest.GeneratePackageIn(t, dir, "observedcollision", "fixture", g, gen.GenOptions{Start: "main", Types: true, Document: true, Observability: true})
	for _, pkg := range []string{"plain", "fixture"} {
		typesSource, err := os.ReadFile(filepath.Join(dir, pkg, "types", "types.go"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(typesSource), "type ErrDocumentBusy struct") {
			t.Fatalf("%s: the grammar type ErrDocumentBusy is not declared under its name", pkg)
		}
	}
	for name, source := range map[string]string{
		"go.mod": gentest.GoMod("observedcollision"),
		"fixture/parser_test.go": `package fixture

import pegort "github.com/ornew/pego/runtime"
import ("errors"; "testing"; "observedcollision/fixture/types")
func TestCollision(t *testing.T) {
 d, err := NewDocument("a")
 if err != nil { t.Fatal(err) }
 got, err := d.ParseAST()
 var typed *types.ErrDocumentBusy = got
 if err != nil || typed.Text != "a" { t.Fatal(got, err) }
 var inner error
 d.ParseAST(pegort.WithTrace(func(pegort.TraceEvent) { if inner == nil { _, inner = d.ParseAST(WithMaxDepth(-1)) } }))
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
