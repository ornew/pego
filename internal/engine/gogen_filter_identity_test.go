package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

const typedFilterIdentityGrammar = `type Word = { Text string }
type Alias ~ Word
type Result = { First *Alias; Items []Alias; Optional []*Alias; Empty []Alias; NilInput []Alias }
rule main Result = w:word $$ => Result{
    First: $w,
    Items: filter(list($w, $w), (item) => true),
    Optional: filter(list(nil, $w, nil, $w), (item) => true),
    Empty: filter(list($w), (item) => false),
    NilInput: filter(nil, (item) => true)
}
rule empty Result = $$ => Result{
    First: nil,
    Items: filter(nil, (item) => true),
    Optional: filter(list(nil, nil), (item) => true),
    Empty: filter(nil, (item) => true),
    NilInput: filter(nil, (item) => true)
}
rule word Word = t:([a-zé]+) #atomic => Word{Text: text($t)}`

// A separate typed consumer observes pointer identity rather than comparing
// serialized duplicate values. Both direct construction and Node conversion
// must retain the same element objects through filtering and aliases.
func TestGeneratedTypedFilterIdentity(t *testing.T) {
	t.Parallel()
	eachMode(t, testGeneratedTypedFilterIdentity)
}

func testGeneratedTypedFilterIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated typed consumers")
	}
	grammar, err := parseTest(typedFilterIdentityGrammar)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name string, data []byte) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", []byte(testGoMod("filteridentity")))
	var imports, consumers strings.Builder
	for i, conversion := range []bool{false, true} {
		for _, start := range []string{"main", "empty"} {
			pkg := fmt.Sprintf("p%d%s", i, start)
			code, err := generateCode(t, grammar, gen.GenOptions{Package: pkg, Start: start, Types: true, ConvertTypes: conversion})
			if err != nil {
				t.Fatalf("conversion=%t start=%s: %v", conversion, start, err)
			}
			if !conversion && !strings.Contains(string(code), "TParse(trules") {
				t.Fatalf("%s did not select the typed runtime", pkg)
			}
			write(pkg+"/parser.go", code)
			fmt.Fprintf(&imports, "\t%s \"filteridentity/%s\"\n", pkg, pkg)
			body := `
func TestPACKAGEIdentity(t *testing.T) {
 for _, unit := range []PACKAGE.PositionUnit{PACKAGE.CodePoints, PACKAGE.Bytes} {
  result, err := PACKAGE.ParseAST("é", PACKAGE.WithUnit(unit))
  if err != nil || result == nil { t.Fatalf("parse: %v", err) }
  first := result.First
  var alias PACKAGE.Alias = first
  if first == nil || len(result.Items) != 2 || result.Items[0] != first || result.Items[1] != alias {
   t.Fatalf("filter did not preserve aliased element pointer identity: first=%T %p %v, items=%T %v, alias=%T %p", first, first, first, result.Items, result.Items, alias, alias)
  }
  if len(result.Optional) != 4 || result.Optional[0] != nil || result.Optional[1] != first || result.Optional[2] != nil || result.Optional[3] != first {
   t.Fatal("filter changed nil elements, order or identity")
  }
  if result.Empty == nil || len(result.Empty) != 0 || result.NilInput == nil || len(result.NilInput) != 0 {
   t.Fatal("empty or nil input did not produce a nonnil empty typed list")
  }
  if _, err := PACKAGE.ParseAST("b", PACKAGE.WithUnit(unit)); err != nil { t.Fatal(err) }
  if first.Text != "é" || result.Items[0] != first || result.Items[1] != first {
   t.Fatal("later parsing changed a retained filtered value")
  }
 }
}
`
			if start == "empty" {
				body = `
func TestPACKAGEEmpty(t *testing.T) {
 for _, unit := range []PACKAGE.PositionUnit{PACKAGE.CodePoints, PACKAGE.Bytes} {
  result, err := PACKAGE.ParseAST("", PACKAGE.WithUnit(unit))
  if err != nil || result == nil { t.Fatalf("empty parse: %v", err) }
  if result.First != nil || result.Items == nil || len(result.Items) != 0 || result.Empty == nil || len(result.Empty) != 0 || result.NilInput == nil || len(result.NilInput) != 0 {
   t.Fatal("nil-input or empty-list representation changed")
  }
  if len(result.Optional) != 2 || result.Optional[0] != nil || result.Optional[1] != nil {
   t.Fatal("all-nil input elements were dropped")
  }
 }
}
`
			}
			body = strings.ReplaceAll(body, "TestPACKAGE", "TestP"+pkg[1:])
			consumers.WriteString(strings.ReplaceAll(body, "PACKAGE", pkg))
		}
	}
	write("identity_test.go", []byte("package filteridentity\n\nimport (\n\t\"testing\"\n"+imports.String()+")\n"+consumers.String()))
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "test", "-count=1", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("typed filter identity consumer: %v\n%s", err, output)
	}
}
