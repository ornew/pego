package engine

import (
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestFormatPreservesMinusSemantics(t *testing.T) {
	for _, src := range []string{
		`def main=- -"a" $$`,
		`def main=(- -"a" / "b") $$`,
		`def main=(- -- "a" "b" / "a" "c") $$`,
		`def main="a" [(- -1) == 1] $$`,
		"type P struct { N int }\ndef main:P=\"a\" $$ -> new P{N:- -1}",
	} {
		t.Run(src, func(t *testing.T) {
			for _, options := range []Options{{}, {DisableMemo: true}} {
				before := compile(t, src, options)
				after := compile(t, grammar.Format(before.Grammar), options)
				for _, input := range []string{"", "a", "b", "aa", "ab", "ac"} {
					checkBackends(t, before, "main", input)
					checkBackends(t, after, "main", input)
					for _, unit := range []Unit{CodePoints, Bytes} {
						for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
							opts := ParseOptions{Backend: backend, Unit: unit}
							n1, err1 := before.ParseWith("main", input, opts)
							n2, err2 := after.ParseWith("main", input, opts)
							if resultJSON(n1, err1) != resultJSON(n2, err2) {
								t.Errorf("%s/%s on %q: formatting changes %s to %s", backend, unit, input, resultJSON(n1, err1), resultJSON(n2, err2))
							}
						}
					}
				}
			}
		})
	}
}
