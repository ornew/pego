package engine

import (
	"testing"

	"github.com/ornew/pego/internal/compile"
)

// A compiled grammar keeps hidden rules and inline actions, with and without default values, and
// loads to a program that gives the same results on every backend.
func TestHiddenRulesCompiledRoundTrip(t *testing.T) {
	type source struct {
		name, src string
		inputs    []string
	}
	var sources []source
	for _, tc := range spliceCases() {
		sources = append(sources, source{"hidden " + tc.name, tc.hidden, tc.inputs})
	}
	for _, tc := range inlineActionCases() {
		sources = append(sources, source{"inline " + tc.name, tc.inline, tc.inputs})
	}
	for _, s := range sources {
		t.Run(s.name, func(t *testing.T) {
			for _, opts := range []compile.Options{{}, {DefaultValue: true}, {SourceInfo: true}} {
				prog := compileBare(t, s.src, opts)
				for _, mode := range valueModes {
					loaded := saveLoad(t, prog, "main", mode)
					for _, input := range s.inputs {
						for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
							if backend == Closure {
								_, err := loaded.ParseWith("main", input, servedBy(mode, ParseOptions{Backend: backend}))
								wantClosureRefused(t, err) // the closure backend needs the grammar
								continue
							}
							want := resultJSON(prog.ParseWith("main", input, ParseOptions{Backend: backend}))
							got := resultJSON(loaded.ParseWith("main", input, servedBy(mode, ParseOptions{Backend: backend})))
							if got != want {
								t.Errorf("%+v %s mode %q on %s:\nloaded:   %s\ncompiled: %s", opts, mode, input, backend, got, want)
							}
						}
					}
				}
			}
		})
	}
}
