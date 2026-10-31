package engine

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
	"github.com/ornew/zeroparse/internal/value"
)

// Every compiler path accepts a whole-rule action of an Operators Rule, from source and from a
// JSON AST, and reports an invalid one; none changes the input AST.
func TestOperatorsRuleWholeActionCompiles(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testOperatorsRuleWholeActionCompiles)
}

func testOperatorsRuleWholeActionCompiles(t *testing.T) {
	type outcome struct {
		message string // the compile error, or "" if the action is valid
		pos     grammar.Pos
	}
	for action, want := range map[string]outcome{
		"nil":          {},
		"$value":       {},
		"(nil)":        {},
		"($value)":     {},
		"Unknown{}":    {"Unknown", grammar.Pos{Line: 3, Col: 6}},
		"(Unknown{})":  {"Unknown", grammar.Pos{Line: 3, Col: 7}},
		"$1":           {"positional capture $1 is removed", grammar.Pos{Line: 3, Col: 6}},
		"$lhs":         {"undefined capture $lhs", grammar.Pos{Line: 3, Col: 6}},
		"text($value)": {},
	} {
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
						"TS generator": func() error { _, err := gentest.GenerateTSBundle(input.g, gen.GenOptions{Start: "main"}); return err },
						"typed TS generator": func() error {
							_, err := gentest.GenerateTSBundle(input.g, gen.GenOptions{Start: "main", Types: true})
							return err
						},
					} {
						t.Run(name, func(t *testing.T) {
							err := run()
							if name == "recognizer compile" && want.message != "" {
								// The recognizer checks the action as it checks that of an ordinary rule.
								o, perr := parseLegacyTest("rule main = \"a\" => " + action)
								if perr != nil {
									t.Fatal(perr)
								}
								_, oerr := Compile(o, compile.Options{Recognize: true})
								if (err == nil) != (oerr == nil) || err != nil && !strings.Contains(err.Error(), strings.SplitN(oerr.Error(), ": ", 2)[1]) {
									t.Fatalf("got %v; an ordinary rule gives %v", err, oerr)
								}
								return
							}
							if want.message == "" {
								if err != nil {
									t.Fatalf("got %v; want no error", err)
								}
								return
							}
							errs, ok := err.(compile.ErrorList)
							if !ok || len(errs) == 0 || !strings.Contains(errs[0].Msg, want.message) {
								t.Fatalf("got %v; want an error containing %q", err, want.message)
							}
							if input.g == g && errs[0].Pos != want.pos {
								t.Fatalf("error position = %v; want %v", errs[0].Pos, want.pos)
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

// The action runs once per call, after the complete fold: an intermediate reduction never holds
// its result, while a call from an operand part (the parentheses) holds its own.
func TestOperatorsRuleWholeActionRunsOncePerCall(t *testing.T) {
	t.Parallel()
	const src = `type Wrap = { Value _ }
rule main Wrap = #operators {
    skip " "*
    operand [a-z]
    operand "(" x:main ")" => $x
    level { infix left "+" }
    level { infix right "^" }
} => Wrap{Value: $value}`
	prog := mustCompile(t, src)
	for _, tc := range []struct{ input, want string }{
		{"a+b+c", `Wrap{Value: (lhs:(lhs:"a"@main op:"+" rhs:"b"@main)@main op:"+" rhs:"c"@main)@main}`},
		{"a^b^c", `Wrap{Value: (lhs:"a"@main op:"^" rhs:(lhs:"b"@main op:"^" rhs:"c"@main)@main)@main}`},
		{"(a+b)+c", `Wrap{Value: (lhs:Wrap{Value: (lhs:"a"@main op:"+" rhs:"b"@main)@main} op:"+" rhs:"c"@main)@main}`},
		{"((a))", `Wrap{Value: Wrap{Value: Wrap{Value: "a"@main}}}`},
	} {
		for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
			if got := resultWith(prog, tc.input, ParseOptions{Backend: b}); got != tc.want {
				t.Errorf("%v: %q\n got  %s\n want %s", b, tc.input, got, tc.want)
			}
		}
		checkBackends(t, prog, "main", tc.input)
	}
}

// A node that the action makes covers the rule's complete match, including the leading skip text;
// the folded value keeps its own range.
func TestOperatorsRuleWholeActionRange(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `type Wrap = { Value _ }
rule main Wrap = #operators {
    skip " "*
    operand [a-z]
    level { infix left "+" }
} => Wrap{Value: $value}`)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		n, err := prog.ParseWith("main", "  a+b", ParseOptions{Backend: b})
		if err != nil {
			t.Fatal(err)
		}
		folded := n.Field("Value").(*value.Node)
		if value.D(n).Start != 0 || value.D(n).End != 5 || value.D(folded).Start != 2 || value.D(folded).End != 5 {
			t.Errorf("%v: action node %d-%d, folded value %d-%d; want 0-5 and 2-5", b, value.D(n).Start, value.D(n).End, value.D(folded).Start, value.D(folded).End)
		}
	}
}

// The declared type is the type of the action's result: the fold has its own inferred type, which
// $lhs and $rhs have, and the action's result is checked against the declared type.
func TestOperatorsRuleWholeActionTypes(t *testing.T) {
	t.Parallel()
	const src = `type Wrap = { Value _ }
rule main Wrap = #operators {
    operand "a"
    level { infix left "+" }
} => Wrap{Value: $value}`
	mustCompile(t, src)
	if err := compileError(t, strings.Replace(src, "=> Wrap{Value: $value}", "=> $value", 1)); !strings.Contains(err, "not assignable to Wrap") {
		t.Errorf("final action type error = %s", err)
	}
	// An operator action is checked against the fold, not against the declared type.
	if err := compileError(t, `type Box = { X node[string] }
type Out = { X _ }
rule main Out = #operators {
    operand "a"
    level { infix left "+" => Box{X: $lhs} }
} => Out{X: $value}`); !strings.Contains(err, "in field X of Box") {
		t.Errorf("internal fold type error = %s", err)
	}
	// A typed fold: the operator actions make Bin from $lhs and $rhs of the fold's type.
	mustCompile(t, `type Num = node[string]
type Bin = { L Expr; R Expr }
type Expr ~ Num | Bin
type Out = { E Expr }
rule main Out = #operators {
    operand n:[0-9] => Num{$n}
    level { infix left "+" => Bin{L: $lhs, R: $rhs} }
} => Out{E: $value}`)
	// Without a declared type, the rule's type is the action's.
	prog := mustCompile(t, `type Out = { E _ }
rule main = #operators {
    operand "a"
    level { infix left "+" }
} => Out{E: $value}`)
	if typ, ok := prog.RuleType("main"); !ok || typ != "Out" {
		t.Errorf("rule type %q, %v; want Out", typ, ok)
	}
	// $value is a local of the whole-rule action only.
	if err := compileError(t, `rule main = #operators {
    operand "a"
    level { infix left "+" => $value }
}`); !strings.Contains(err, "$value is only available in the whole-rule action of an Operators Rule") {
		t.Errorf("$value in an operator action: %s", err)
	}
	if err := compileError(t, `rule main = "a" => $value`); !strings.Contains(err, "$value is only available in the whole-rule action of an Operators Rule") {
		t.Errorf("$value in an ordinary rule: %s", err)
	}
}

// With a named node type N, the action's result is converted to N and the operators are not.
func TestOperatorsRuleWholeActionNamedNodeType(t *testing.T) {
	t.Parallel()
	prog := compileBare(t, `type Expr = node[_]
rule main Expr = #operators {
    operand [0-9]
    level { infix left "+" }
} => Expr{$value}`)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		n, err := prog.ParseWith("main", "1+2", ParseOptions{Backend: b})
		if err != nil {
			t.Fatal(err)
		}
		if n.Type() != "Expr" || len(value.D(n).Children) != 3 {
			t.Fatalf("%v: result %s of type %s", b, n, n.Type())
		}
		if value.D(n).Children[0].Type() == "Expr" {
			t.Errorf("%v: an operand was converted", b)
		}
	}
	n, err := prog.ParseWith("main", "1+2+3", ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if inner := value.D(n).Children[0]; inner.Type() == "Expr" {
		t.Errorf("an intermediate operator node was converted: %s", inner.Type())
	}
}

// An action error fails the parse on every backend, with the rule's name, as for an ordinary rule.
func TestOperatorsRuleWholeActionError(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `type R = { N int }
rule main R = #operators {
    operand [0-9]
    level { infix left "+" }
} => R{N: 10 / (len(text($value)) - 3)}`)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		_, err := prog.ParseWith("main", "1+2", ParseOptions{Backend: b})
		if err == nil || !strings.Contains(err.Error(), "main") || !strings.Contains(err.Error(), "division by zero") {
			t.Errorf("%v: error %v", b, err)
		}
		if _, err := prog.ParseWith("main", "1+2+3", ParseOptions{Backend: b}); err != nil {
			t.Errorf("%v: %v", b, err)
		}
	}
}

// Default values drop the whole-rule action with the others.
func TestOperatorsRuleWholeActionDefaultValue(t *testing.T) {
	t.Parallel()
	const body = `rule main = #operators {
    operand [0-9]
    level { infix left "+" }
}`
	plain := mustCompile(t, body)
	prog := mustCompile(t, "type Out = { E _ }\n"+body+" => Out{E: $value}", compile.Options{DefaultValue: true})
	if got, want := result(prog, "1+2"), result(plain, "1+2"); got != want {
		t.Errorf("default value %s; want %s", got, want)
	}
}

// A compiled grammar, saved and loaded in every mode, runs the action as the compiled program does.
func TestOperatorsRuleWholeActionCompiledRoundTrip(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`type Wrap = { Value _ }
rule main Wrap = #operators {
    skip " "*
    operand [a-z]
    operand "(" x:main ")" => $x
    level { infix left "+" }
} => Wrap{Value: $value}`,
		`type Expr = node[_]
rule main Expr = #operators {
    operand [0-9]
    level { infix left "+" }
} => Expr{$value}`,
	} {
		checkCompiledRoundTrip(t, mustCompile(t, src), []string{"a", "a+(b+c)", "1+2", "", "a+"})
	}
}

// $value is the folded value only in the whole-rule action of an Operators Rule: a capture named
// value is reserved there, and $value anywhere else is an error that says where it is available.
// An ordinary rule's value capture is an ordinary capture.
func TestOperatorsRuleValueNameReserved(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"operand capture", "rule main = #operators {\n    operand value:\"a\"\n    level { infix left \"+\" }\n}", "capture name value is reserved for the whole-rule action of an Operators Rule"},
		{"operator capture", "rule main = #operators {\n    operand \"a\"\n    level { infix left value:\"+\" }\n}", "capture name value is reserved for the whole-rule action of an Operators Rule"},
		{"operand action", "rule main = #operators {\n    operand \"a\" => $value\n}", "$value is only available in the whole-rule action of an Operators Rule"},
		{"operator action", "rule main = #operators {\n    operand \"a\"\n    level { infix left \"+\" => $value }\n}", "$value is only available in the whole-rule action of an Operators Rule"},
		{"ordinary rule", "rule main = \"a\" => $value", "$value is only available in the whole-rule action of an Operators Rule"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := compileError(t, tc.src); !strings.Contains(err, tc.want) {
				t.Errorf("error %q; want it to contain %q", err, tc.want)
			}
		})
	}
	// An ordinary rule's value capture stays an ordinary capture, and so does a value capture of a
	// rule that calls an Operators Rule.
	prog := mustCompile(t, `rule main = value:"a" => $value
rule e = #operators {
    operand "b"
    level { infix left "+" }
} => text($value)`)
	if got := result(prog, "a"); got != `"a"` {
		t.Errorf("ordinary value capture: %s", got)
	}
}
