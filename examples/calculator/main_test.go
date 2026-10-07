package main

import (
	"testing"

	"github.com/ornew/pego"
)

func TestEval(t *testing.T) {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		t.Fatal(err)
	}
	for src, want := range map[string]float64{
		"1 + 2 * 3":       7,
		"(1 + 2) * 3":     9,
		"10 - 4 - 3":      3,
		"2 ^ 3 ^ 2":       512,
		"-2 ^ 2":          -4,
		"2 ^ -1":          0.5,
		"7 % 4 + 1.5":     4.5,
		" 1+-+-1 ":        2,
		"((((42))))":      42,
		"1 / 4 * 2 - 0.5": 0,
	} {
		got, err := eval(p, src)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if got != want {
			t.Errorf("%q = %v, want %v", src, got, want)
		}
	}
	if _, err := eval(p, "1 +"); err == nil {
		t.Error("expected a syntax error")
	}
}
