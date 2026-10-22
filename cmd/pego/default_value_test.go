package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDefaultValue(t *testing.T) {
	g := writeFile(t, "kv-action.pego", "type KV = { K node[string]; V node[string] }\n@start\nrule main KV = k:[a-z]+ #atomic \"=\" v:[0-9]+ #atomic => KV{K: $k, V: $v}")
	out, err := runCLI(t, "", "parse", "-g", g, "-i", "a=1", "-f", "sexpr")
	if want := `KV{K: "a", V: "1"}` + "\n"; err != nil || out != want {
		t.Errorf("got %q, %v", out, err)
	}
	out, err = runCLI(t, "", "parse", "-g", g, "-i", "a=1", "-f", "sexpr", "-default-value", "-source-info")
	if want := `(k:"a" "=" v:"1")@main` + "\n"; err != nil || out != want {
		t.Errorf("default value: got %q, %v", out, err)
	}
	// The option is a property of a compiled grammar.
	dv := filepath.Join(t.TempDir(), "dv.pegoc")
	if _, err := runCLI(t, "", "compile", "-g", g, "-default-value", "-source-info", "-o", dv); err != nil {
		t.Fatal(err)
	}
	if out, err := runCLI(t, "", "parse", "-g", dv, "-i", "a=1", "-f", "sexpr"); err != nil || out != `(k:"a" "=" v:"1")@main`+"\n" {
		t.Errorf("compiled: got %q, %v", out, err)
	}
	if out, err := runCLI(t, "", "parse", "-g", dv, "-i", "a=1", "-f", "sexpr", "-default-value"); err != nil || out != `(k:"a" "=" v:"1")@main`+"\n" {
		t.Errorf("compiled with the flag: got %q, %v", out, err)
	}
	plain := filepath.Join(t.TempDir(), "plain.pegoc")
	if _, err := runCLI(t, "", "compile", "-g", g, "-o", plain); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "", "parse", "-g", plain, "-i", "a=1", "-default-value"); err == nil || !strings.Contains(err.Error(), "compile it with -default-value") {
		t.Errorf("got %v", err)
	}
	// Generated parsers drop actions too, but not with typed output.
	if _, err := runGenGo(t, "-g", g, "-pkg", "p", "-default-value", "-types"); err == nil || !strings.Contains(err.Error(), "default values") {
		t.Errorf("gen -types: got %v", err)
	}
	if out, err := runGenGo(t, "-g", g, "-pkg", "p", "-default-value"); err != nil || !strings.Contains(out, "package p") {
		t.Errorf("gen: %v", err)
	}
}
