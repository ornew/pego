package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/syntax"
)

// resultCheckGrammarLegacy is resultCheckGrammar with a terminal type Int, in the retired syntax that
// only syntax.ParseLegacy accepts: grammars that reach the engine as JSON ASTs may still use it.
func resultCheckGrammarLegacy(typ, rt string) string {
	return `type Int terminal
type Alias ~ Int
type Box = { Value Int }
type S = { F ` + typ + ` }
type W = { V ` + rt + ` }
rule main W = ma / mb / mc / md / me / mp / mq
rule ma W = &(. ".") v:rA "." => W{V: $v}
rule mb W = &(. ",") v:rB "," => W{V: $v}
rule mc W = &(. ":") v:rC ":" => W{V: $v}
rule md W = &(. "+") v:rD "+" => W{V: $v}
rule me W = &(. "=") v:rE => W{V: $v}
rule mp W = &(. "!") v:rA {$v == nil || $v != nil} "!" => W{V: $v}
rule mq W = &(. "~") v:rB {$v == nil || $v != nil} "~" => W{V: $v}
rule rA ` + rt + ` = v:val => $v.(` + typ + `)
rule rB ` + rt + ` = v:val => $v.(` + typ + `)
rule rC ` + rt + ` = #operators {
    operand v:val => $v.(` + typ + `)
}
rule rD ` + rt + ` = #operators {
    operand v:val => $v.(` + typ + `)
}
rule rE ` + rt + ` = v:sc "=" => $v.(` + typ + `)
rule sc _ = si / ss / sb
rule si = "i" => 1
rule ss = "s" => "s"
rule sb = "b" => true
rule val _ = vm / vt / vl / vk / ve / vn / vq / vr / vw / vp / vz
rule vm = "m"
rule vt = "t" => Int{"t"}
rule vl = "l" => []Int{Int{"a"}}
rule vk = k:"k" => list($k)
rule ve = "e" => list(nil)
rule vn = "n" => list()
rule vq = "q" => Box{Value: Int{"q"}}
rule vr = ("r" ";") #recover(skip="?")
rule vw = "w" => [][]Int{[]Int{Int{"w"}}}
rule vp = "p" => list(Int{"a"}, nil)
rule vz = "z" => nil
`
}

// resultCheckRoutes maps an input's route character to the rule it reaches.
var resultCheckRoutes = map[byte]string{'.': "rA", ',': "rB", ':': "rC", '+': "rD", '=': "rE", '!': "rA", '~': "rB"}

// TestRuleResultChecks checks that a cast of a rule result to the rule's declared type accepts the
// values that fit, on every backend, in every program form, and
// that the values it accepts give the same results as when the rules are declared _.
func TestRuleResultChecks(t *testing.T) {
	t.Parallel()
	// The terminal model is the same grammar in the retired terminal type syntax, which
	// syntax.ParseLegacy reads as the text structs it means: it accepts the same values.
	for _, model := range []struct {
		name    string
		grammar func(typ, rt string) string
		parse   func(string) (*grammar.Grammar, error)
		fields  map[string]string
	}{
		{"text struct", gentest.ResultCheckGrammar, parseTest, gentest.FieldCheckFields},
		{"terminal", resultCheckGrammarLegacy, parseLegacyTest, gentest.FieldCheckFields},
	} {
		t.Run(model.name, func(t *testing.T) {
			for _, typ := range gentest.ResultCheckTypesOf(model.fields) {
				t.Run(typ, func(t *testing.T) { checkRuleResultType(t, typ, model.grammar, model.parse, model.fields) })
			}
		})
	}
}

func checkRuleResultType(t *testing.T, typ string, resultGrammar func(typ, rt string) string, parse func(string) (*grammar.Grammar, error), fields map[string]string) {
	{
		{
			g, err := parse(resultGrammar(typ, typ))
			if err != nil {
				t.Fatal(err)
			}
			checked, err := Compile(g, compile.Options{})
			if err != nil {
				t.Fatal(err)
			}
			loaded := saveLoad(t, checked, "main", CompiledDocument)
			loadedBare := saveLoad(t, checked, "main", CompiledParse)
			for _, p := range []*Program{loaded, loadedBare} {
				mode, _ := p.Mode()
				_, err := p.ParseWith("main", "", servedBy(mode, ParseOptions{Backend: Closure}))
				wantClosureRefused(t, err)
			}
			ug, err := parse(resultGrammar(typ, "_"))
			if err != nil {
				t.Fatal(err)
			}
			untyped, err := Compile(ug, compile.Options{})
			if err != nil {
				t.Fatal(err)
			}
			limits := limits.WorkLimits{MaxSteps: 1 << 30}
			declared := checked.Typed.Fields["S"][0].T.String() // as the value checker resolves it
			for _, in := range gentest.ResultCheckInputs() {
				value := gentest.FieldCheckValues[in[:1]]
				rule := resultCheckRoutes[in[1]]
				accepted := strings.Contains(fields[typ], in[:1])
				runs := []struct {
					name string
					prog *Program
					o    ParseOptions
				}{
					{"closure", checked, ParseOptions{Backend: Closure}},
					{"bytecode", checked, ParseOptions{Backend: Bytecode}},
					{"iterative", checked, ParseOptions{Backend: BytecodeIterative}},
					{"limited closure", checked, ParseOptions{Backend: Closure, WorkLimits: limits}},
					{"limited bytecode", checked, ParseOptions{Backend: Bytecode, WorkLimits: limits}},
					{"document bytecode", loaded, ParseOptions{Backend: Bytecode}},
					{"document iterative", loaded, ParseOptions{Backend: BytecodeIterative}},
					{"limited document bytecode", loaded, ParseOptions{Backend: Bytecode, WorkLimits: limits}},
					{"parse bytecode", loadedBare, ParseOptions{Backend: Bytecode}},
					{"limited parse bytecode", loadedBare, ParseOptions{Backend: Bytecode, WorkLimits: limits}},
				}
				var want string
				for _, unit := range []PositionUnit{CodePoints, Bytes} {
					for _, run := range runs {
						o := run.o
						o.Unit = unit
						mode, isLoaded := run.prog.Mode()
						po := o
						if isLoaded {
							po = servedBy(mode, o)
						}
						got := resultWith(run.prog, in, po)
						if want == "" {
							want = got
						} else if got != want {
							t.Fatalf("%q %s %s: %s\nwant %s", in, run.name, unit, got, want)
						}
						d, err := run.prog.NewDocumentWith("main", in, o)
						if isLoaded && mode != CompiledDocument {
							if err == nil {
								t.Fatalf("%s: Document on a %s-mode file accepted", run.name, mode)
							}
							continue
						}
						if err != nil {
							t.Fatal(err)
						}
						if n, err := d.Parse(); err != nil {
							got = "error: " + err.Error()
						} else {
							got = n.String()
						}
						if got != want {
							t.Fatalf("%q %s %s Document: %s\nwant %s", in, run.name, unit, got, want)
						}
					}
				}
				// Recognition evaluates only the rules whose values a predicate reads, rA and rB: on the
				// predicate routes it checks their results as parsing does. Elsewhere it may skip the
				// values, and with them the checks; the other rules never check.
				rec := resultWith(checked, in, ParseOptions{Recognize: true})
				valueErr := strings.Contains(want, "cannot cast") || strings.Contains(want, "error: action in")
				switch {
				case in[0] == '?' && valueErr && strings.Contains(rec, "syntax error"):
					// The recognizer skips the recovered value, and with it the check; the error
					// of the recovery remains.
				case in[1] == '!' || in[1] == '~' || !valueErr:
					if strings.HasPrefix(rec, "error: ") != strings.HasPrefix(want, "error: ") {
						t.Errorf("%q recognized: %s\nparsed: %s", in, rec, want)
					}
				case rule == "rA" || rule == "rB":
					if rec != "nil" && rec != want {
						t.Errorf("%q recognized: %s\nparsed: %s", in, rec, want)
					}
				case rec != "nil":
					t.Errorf("%q recognized: %s", in, rec)
				}
				name := fieldCheckValueName(value)
				msg := fmt.Sprintf("action in %s: cannot cast %s to %s", rule, name, declared)
				switch {
				case accepted:
					if strings.Contains(want, "cannot cast") {
						t.Errorf("%q (%s) rejected: %s", in, value, want)
					}
					got := want
					if in[1] == ':' || in[1] == '+' {
						// An Operators Rule of a terminal type labels its lines' values with its name.
						got = strings.ReplaceAll(want, "@"+rule, "")
					}
					if any := resultWith(untyped, in, ParseOptions{}); any != got {
						t.Errorf("%q (%s): %s\ndeclared _: %s", in, value, want, any)
					}
				case !strings.Contains(want, msg):
					t.Errorf("%q (%s) accepted or misreported: %s\nwant %s", in, value, want, msg)
				}
			}
		}
	}
}

// TestOperatorResultChecks checks the values of operator lines with actions, which are finished
// by the operator application rather than by the rule, and the checks of rules that compiling
// copies, in every program form.
func TestOperatorResultChecks(t *testing.T) {
	t.Parallel()
	g, err := parseTest(gentest.OperatorResultCheckGrammar)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := Compile(g, compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := prog.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadProgram(data)
	if err != nil {
		t.Fatal(err)
	}
	_, err = loaded.ParseWith("main", "", ParseOptions{Backend: Closure})
	wantClosureRefused(t, err)
	for _, in := range gentest.OperatorResultCheckInputs {
		want := resultWith(prog, in, ParseOptions{})
		rejected := !strings.Contains(in, "l")
		if got := strings.Contains(want, "cannot cast "); got != rejected {
			t.Errorf("%q: %s", in, want)
		}
		limits := limits.WorkLimits{MaxSteps: 1 << 30}
		for _, run := range []struct {
			name string
			prog *Program
			o    ParseOptions
		}{
			{"closure", prog, ParseOptions{Backend: Closure}},
			{"bytecode", prog, ParseOptions{Backend: Bytecode}},
			{"iterative", prog, ParseOptions{Backend: BytecodeIterative}},
			{"limited closure", prog, ParseOptions{Backend: Closure, WorkLimits: limits}},
			{"loaded bytecode", loaded, ParseOptions{Backend: Bytecode}},
			{"loaded iterative", loaded, ParseOptions{Backend: BytecodeIterative}},
			{"limited loaded bytecode", loaded, ParseOptions{Backend: Bytecode, WorkLimits: limits}},
		} {
			if got := resultWith(run.prog, in, run.o); got != want {
				t.Errorf("%q %s: %s\nwant %s", in, run.name, got, want)
			}
		}
	}
}

// terminalOperatorsRuleGrammar has an Operators Rule of a terminal type without an action, which
// converted its value to the terminal like any such rule. A text struct has no such conversion, so
// pego fix asks for an action instead of rewriting it.
const terminalOperatorsRuleGrammar = `type Int terminal
type S = { F Int }
rule main = v:e => S{F: $v}
rule e Int = #operators {
    operand v:val => $v
    level { infix left "+" v:val => $v }
}
rule val _ = "m"
`

func TestTerminalOperatorsRuleNeedsAction(t *testing.T) {
	t.Parallel()
	for _, src := range []string{terminalOperatorsRuleGrammar, `type Num terminal
rule main = e
rule e Num = #operators {
    operand [0-9]+
    level { infix left "+" }
}
`} {
		if _, err := syntax.Fix(src); err == nil || !strings.Contains(err.Error(), "is an operators rule; give it an action before running pego fix") {
			t.Errorf("Fix: %v\n%s", err, src)
		}
	}
	// The text struct counterpart builds the struct in its actions.
	prog := mustCompile(t, gentest.TextStructOperatorsRuleGrammar)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		o := ParseOptions{Backend: b}
		for _, in := range gentest.TerminalOperatorsRuleInputs {
			want := resultWith(prog, in, ParseOptions{})
			if strings.Contains(want, "cannot use") {
				t.Fatalf("%q: %s", in, want)
			}
			if got := resultWith(prog, in, o); got != want {
				t.Errorf("%q %s: %s\nwant %s", in, b, got, want)
			}
		}
	}
}
