package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/zeroparse"
)

func TestParseCheckCompiledRecognition(t *testing.T) {
	p, err := zp.CompileSource(`@start(default)
rule main = "a" $$
@start
rule other = "é" $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	data, err := p.Marshal(zp.CompiledRecognize)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "grammar.zpc")
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
		t.Fatal("closure backend on a compiled grammar succeeded")
	}
	if _, err := runCLI(t, "", "parse", "-g", path, "-i", "a"); err == nil || !strings.Contains(err.Error(), "needs parse mode") {
		t.Fatalf("value parse on a recognize-mode file: %v", err)
	}
	if _, err := runCLI(t, "", "parse", "-g", path, "-check", "-stream"); err == nil {
		t.Fatal("check/stream combination accepted")
	}
}
