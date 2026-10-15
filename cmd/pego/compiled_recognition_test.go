package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ornew/pego"
)

func TestParseCheckCompiledRecognition(t *testing.T) {
	p, err := pego.CompileSource(`rule main = "a" $$
rule other = "é" $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	data, err := p.Marshal(pego.WithoutAST())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "grammar.pegoc")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, backend := range []string{"", "bytecode", "bytecode-iterative"} {
		for _, unit := range []string{"codepoints", "bytes"} {
			for _, start := range []string{"main", "other"} {
				input := "a"
				if start == "other" {
					input = "é"
				}
				args := []string{"parse", "-g", path, "-check", "-backend", backend, "-unit", unit, "-i", input}
				if start != "main" {
					args = append(args, "-s", start)
				}
				got, err := runCLI(t, "", args...)
				if err != nil || got != "ok\n" {
					t.Fatalf("%v: %q %v", args, got, err)
				}
			}
		}
	}
	if got, err := runCLI(t, "", "parse", "-g", path, "-i", "ab", "-check"); err == nil || got != "" {
		t.Fatalf("failure printed success: %q %v", got, err)
	}
	if _, err := runCLI(t, "", "parse", "-g", path, "-i", "a", "-check", "-backend", "closure"); err == nil {
		t.Fatal("AST-less closure succeeded")
	}
	if _, err := runCLI(t, "", "parse", "-g", path, "-check", "-stream"); err == nil {
		t.Fatal("check/stream combination accepted")
	}
}
