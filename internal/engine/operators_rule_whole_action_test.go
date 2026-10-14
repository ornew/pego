package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

func TestOperatorsRuleWholeAction(t *testing.T) {
	for _, action := range []string{"nil", "$1", "Unknown{}", "(nil)", "($1)", "(Unknown{})"} {
		t.Run(action, func(t *testing.T) {
			g, err := syntax.Parse("rule main = operators {\n    operand \"a\"\n} => " + action)
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
					invalid := strings.Contains(action, "Unknown")
					checks := map[string]func() error{
						"compile":           func() error { _, err := Compile(input.g, Options{}); return err },
						"unchecked compile": func() error { _, err := Compile(input.g, Options{NoTypeCheck: true}); return err },
						"saved AST rebuild": func() error { _, err := build(input.g, Options{}, []ruleFlags{{}}); return err },
					}
					if !invalid {
						checks["recognizer compile"] = func() error { _, err := Compile(input.g, Options{recognize: true}); return err }
						checks["Go generator"] = func() error { _, err := Generate(input.g, GenOptions{Start: "main", Package: "p"}); return err }
						checks["typed Go generator"] = func() error {
							_, err := Generate(input.g, GenOptions{Start: "main", Package: "p", Types: true})
							return err
						}
						checks["TS generator"] = func() error { _, err := GenerateTS(input.g, GenOptions{Start: "main"}); return err }
						checks["typed TS generator"] = func() error { _, err := GenerateTS(input.g, GenOptions{Start: "main", Types: true}); return err }
					}
					for name, run := range checks {
						t.Run(name, func(t *testing.T) {
							err := run()
							if invalid {
								if err == nil || !strings.Contains(err.Error(), "Unknown is not a struct type") {
									t.Fatalf("got %v; want unknown type error", err)
								}
							} else if err != nil {
								t.Fatal(err)
							}
						})
					}
					if !invalid {
						p, err := Compile(input.g, Options{})
						if err != nil {
							t.Fatal(err)
						}
						for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
							n, err := p.ParseWith("main", "a", ParseOptions{Backend: backend})
							if err != nil {
								t.Fatal(err)
							}
							wantNil := strings.Contains(action, "nil")
							if (n == nil) != wantNil {
								t.Fatalf("backend %v: node = %v", backend, n)
							}
						}
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

func TestOperatorsRuleWholeActionChecksFinalDeclaredType(t *testing.T) {
	const src = `type Wrap struct { Value node }
rule main Wrap = operators {
    operand "a"
    level { infix left "+" }
} => Wrap{Value: $1}`
	p := compile(t, src)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		n, err := p.ParseWith("main", "a+a", ParseOptions{Backend: backend})
		if err != nil {
			t.Fatal(err)
		}
		if n == nil || n.Type() != "Wrap" || n.Start != 0 || n.End != 3 {
			t.Fatalf("backend %v: result = %v", backend, n)
		}
		folded, ok := n.Field("Value").(*Node)
		if !ok || folded.Type() != TypeOperator {
			t.Fatalf("backend %v: folded value = %v", backend, n.Field("Value"))
		}
	}
	bad := strings.Replace(src, "=> Wrap{Value: $1}", "=> $1", 1)
	if err := compileError(t, bad); !strings.Contains(err, "not assignable to Wrap") {
		t.Fatalf("final action type error = %s", err)
	}
}

func TestOperatorsRuleWholeActionChecksInternalFoldType(t *testing.T) {
	const src = `type Box struct { X Match }
type Out struct { X node }
rule e Out = operators {
    operand "a"
    level { infix left "+" => Box{X: $lhs} }
} => Out{X: $1}`
	if err := compileError(t, src); !strings.Contains(err, "cannot use") || !strings.Contains(err, "in field X of Box") {
		t.Fatalf("internal fold type error = %s", err)
	}
}

func TestOperatorsRuleExplicitDefaultAction(t *testing.T) {
	const source = `rule main = operators {
    operand "α"
    level { infix left "+" }
}`
	plain := compile(t, source)
	for _, opts := range []Options{{}, {NoTypeCheck: true}} {
		g, err := syntax.Parse(source + " => ...")
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
			for _, unit := range []Unit{CodePoints, Bytes} {
				for _, input := range []string{"α", "α+α", ""} {
					parseOpts := ParseOptions{Backend: backend, Unit: unit}
					n, err := explicit.ParseWith("main", input, parseOpts)
					if input != "" && (n == nil || err != nil) || input == "" && err == nil {
						t.Fatalf("%s/%s input %q: node=%v error=%v", backend, unit, input, n, err)
					}
					if got, want := resultJSON(n, err), resultJSON(plain.ParseWith("main", input, parseOpts)); got != want {
						t.Fatalf("%s/%s input %q: explicit %s; omitted %s", backend, unit, input, got, want)
					}
				}
			}
		}
		for name, run := range map[string]func() error{
			"Go generator":       func() error { _, err := Generate(g, GenOptions{Start: "main", Package: "p"}); return err },
			"typed Go generator": func() error { _, err := Generate(g, GenOptions{Start: "main", Package: "p", Types: true}); return err },
			"TS generator":       func() error { _, err := GenerateTS(g, GenOptions{Start: "main"}); return err },
			"typed TS generator": func() error { _, err := GenerateTS(g, GenOptions{Start: "main", Types: true}); return err },
		} {
			if err := run(); err != nil {
				t.Fatalf("%s rejected explicit default: %v", name, err)
			}
		}
	}
}
