package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestOperatorsRuleWholeActionRejection(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testOperatorsRuleWholeActionRejection)
}

func testOperatorsRuleWholeActionRejection(t *testing.T) {
	const message = "whole-rule actions are not supported for Operators Rules"
	for _, action := range []string{"nil", "$1", "Unknown{}", "(nil)", "($1)", "(Unknown{})"} {
		t.Run(action, func(t *testing.T) {
			g, err := parseLegacyTest("rule main = #operators {\n    operand \"a\"\n} => " + action)
			if err != nil {
				t.Fatal(err)
			}
			data, err := grammar.MarshalJSON(g)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := grammar.UnmarshalJSON(data)
			if err != nil {
				t.Fatal(err)
			}
			for _, input := range []struct {
				name string
				g    *grammar.Grammar
			}{{"source AST", g}, {"JSON AST", decoded}} {
				t.Run(input.name, func(t *testing.T) {
					for name, run := range map[string]func() error{
						"compile":            func() error { _, err := Compile(input.g, compile.Options{}); return err },
						"recognizer compile": func() error { _, err := Compile(input.g, compile.Options{Recognize: true}); return err },
						"saved AST rebuild":  func() error { _, err := build(input.g, compile.Options{}, []compile.RuleFlags{{}}); return err },
						"Go generator": func() error {
							_, err := gentest.GenerateCode(t, input.g, gen.GenOptions{Start: "main", Package: "p"})
							return err
						},
						"typed Go generator": func() error {
							_, err := gentest.GenerateCode(t, input.g, gen.GenOptions{Start: "main", Package: "p", Types: true})
							return err
						},
						"TS generator": func() error { _, err := generateTSBundle(input.g, gen.GenOptions{Start: "main"}); return err },
						"typed TS generator": func() error {
							_, err := generateTSBundle(input.g, gen.GenOptions{Start: "main", Types: true})
							return err
						},
					} {
						t.Run(name, func(t *testing.T) {
							err := run()
							errs, ok := err.(compile.ErrorList)
							if !ok || len(errs) != 1 || !strings.Contains(errs[0].Msg, message) {
								t.Fatalf("got %v; want a whole-rule action compile error", err)
							}
							if input.g == g {
								wantPos := grammar.Pos{Line: 3, Col: 6}
								if strings.HasPrefix(action, "(") {
									wantPos.Col++
								}
								if action == "nil" || action == "(nil)" {
									// NilLit has no source position; use the declaration.
									wantPos = g.Rules()[0].Pos
								}
								if errs[0].Pos != wantPos {
									t.Fatalf("error position = %v; want %v", errs[0].Pos, wantPos)
								}
							}
						})
					}
					unchanged, err := grammar.MarshalJSON(input.g)
					if err != nil || string(unchanged) != string(data) {
						t.Fatalf("compile changed the input AST: %s, %v", unchanged, err)
					}
				})
			}
		})
	}
}

func TestOperatorsRuleExplicitDefaultAction(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testOperatorsRuleExplicitDefaultAction)
}

func testOperatorsRuleExplicitDefaultAction(t *testing.T) {
	const source = `rule main = #operators {
    operand "α"
    level { infix left "+" }
}`
	plain := mustCompile(t, source)
	for _, opts := range []compile.Options{{SourceInfo: true}} {
		g, err := parseTest(source + " => ...")
		if err != nil {
			t.Fatal(err)
		}
		if !g.Rules()[0].DefaultAction || g.Rules()[0].Action != nil {
			t.Fatal("explicit default lost its AST representation")
		}
		data, err := grammar.MarshalJSON(g)
		if err != nil {
			t.Fatal(err)
		}
		g, err = grammar.UnmarshalJSON(data)
		if err != nil {
			t.Fatal(err)
		}
		explicit, err := Compile(g, opts)
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				for _, input := range []string{"α", "α+α", ""} {
					parseOpts := ParseOptions{Backend: backend, Unit: unit}
					n, err := explicit.ParseWith("main", input, parseOpts)
					if input != "" && (n == nil || err != nil) || input == "" && err == nil {
						t.Fatalf("%s/%s input %q: node=%v error=%v", backend, unit, input, n, err)
					}
					if got, want := gentest.ResultJSON(n, err), gentest.ResultJSON(plain.ParseWith("main", input, parseOpts)); got != want {
						t.Fatalf("%s/%s input %q: explicit %s; omitted %s", backend, unit, input, got, want)
					}
				}
			}
		}
		for name, run := range map[string]func() error{
			"Go generator": func() error {
				_, err := gentest.GenerateCode(t, g, gen.GenOptions{Start: "main", Package: "p"})
				return err
			},
			"typed Go generator": func() error {
				_, err := gentest.GenerateCode(t, g, gen.GenOptions{Start: "main", Package: "p", Types: true})
				return err
			},
			"TS generator":       func() error { _, err := generateTSBundle(g, gen.GenOptions{Start: "main"}); return err },
			"typed TS generator": func() error { _, err := generateTSBundle(g, gen.GenOptions{Start: "main", Types: true}); return err },
		} {
			if err := run(); err != nil {
				t.Fatalf("%s rejected explicit default: %v", name, err)
			}
		}
	}
}
