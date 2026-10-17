package main

import (
	"os"
	"strings"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/grammar"
)

func TestFmtSyntax(t *testing.T) {
	const src = `type T = {}
rule main T = "x" #atomic @error(message="x") => T{}`
	g, err := pego.ParseGrammar(src)
	if err != nil {
		t.Fatal(err)
	}
	want := grammar.FormatSyntax(g)
	file := writeFile(t, "grammar.pego", src)
	for _, args := range [][]string{{"fmt", "-syntax"}, {"fmt", "-syntax", file}} {
		out, err := runCLI(t, src, args...)
		if err != nil || out != want {
			t.Fatalf("%v: output %q, error %v", args, out, err)
		}
	}
	for _, args := range [][]string{{"fmt", "-syntax", "-w", file}, {"fmt", "-syntax", "-l", file}, {"fmt", "-syntax", "-w"}} {
		if _, err := runCLI(t, src, args...); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
			t.Fatalf("%v: %v", args, err)
		}
	}
	b, err := os.ReadFile(file)
	if err != nil || string(b) != src {
		t.Fatalf("source overwritten: %q, %v", b, err)
	}
}
