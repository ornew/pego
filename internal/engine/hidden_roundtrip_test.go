package engine

import "testing"

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
			for _, opts := range []Options{{}, {DefaultValue: true}, {SourceInfo: true}} {
				prog := compileBare(t, s.src, opts)
				for _, omit := range []bool{false, true} {
					data, err := prog.MarshalBinaryWith("main", MarshalOptions{OmitAST: omit})
					if err != nil {
						t.Fatal(err)
					}
					loaded, _, err := LoadProgram(data, Options{})
					if err != nil {
						t.Fatalf("%+v: %v", opts, err)
					}
					for _, input := range s.inputs {
						for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
							if omit && backend == Closure {
								continue // the closure backend needs the grammar
							}
							want := resultJSON(prog.ParseWith("main", input, ParseOptions{Backend: backend}))
							got := resultJSON(loaded.ParseWith("main", input, ParseOptions{Backend: backend}))
							if got != want {
								t.Errorf("%+v omit AST %v %q on %s:\nloaded:   %s\ncompiled: %s", opts, omit, input, backend, got, want)
							}
						}
					}
				}
			}
		})
	}
}
