package engine

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/syntax"
)

// resultCheckGrammar declares the rules rA to rE with the type rt and passes them the dynamically
// typed values of fieldCheckValues, which each rule casts to typ: input "<value><route>" reaches
// rA (route "."), whose action forwards the value; rB (","), which does so with a second capture;
// the Operators Rule operand lines rC (":") and rD ("+"); rE, which forwards a scalar value (input
// "<i|s|b>="); and, through a predicate that reads the value, rA and rB ("!" and "~"). S gives the
// type typ a field, for the expected messages; W wraps the result in a field of type rt, so that
// generated parsers use the typed runtime when rt is representable.
func resultCheckGrammar(typ, rt string) string {
	return `type Int = { Text string }
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
rule vt = "t" => Int{Text: "t"}
rule vl = "l" => []Int{Int{Text: "a"}}
rule vk = k:"k" => list($k)
rule ve = "e" => list(nil)
rule vn = "n" => list()
rule vq = "q" => Box{Value: Int{Text: "q"}}
rule vr = ("r" ";") #recover(skip="?")
rule vw = "w" => [][]Int{[]Int{Int{Text: "w"}}}
rule vp = "p" => list(Int{Text: "a"}, nil)
rule vz = "z" => nil
`
}

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

func resultCheckInputs() []string {
	var in []string
	for _, v := range "mtlkenq?wpz" {
		for _, r := range ".,:+!~" {
			in = append(in, string(v)+string(r))
		}
	}
	return append(in, "i=", "s=", "b=")
}

// resultCheckTypes are the types of fieldCheckFields that a rule can declare.
func resultCheckTypes() []string { return resultCheckTypesOf(fieldCheckFields) }

func resultCheckTypesOf(fields map[string]string) []string {
	var types []string
	for _, typ := range fieldCheckTypesOf(fields) {
		switch typ {
		case "int", "string", "bool", "Int | string", "*(Box | bool)":
		default:
			types = append(types, typ)
		}
	}
	return types
}

// TestRuleResultChecks checks that a cast of a rule result to the rule's declared type accepts the
// values that fit, on every backend, in every program form, and
// that the values it accepts give the same results as when the rules are declared _.
func TestRuleResultChecks(t *testing.T) {
	// The terminal model is the same grammar in the retired terminal type syntax, which
	// syntax.ParseLegacy reads as the text structs it means: it accepts the same values.
	for _, model := range []struct {
		name    string
		grammar func(typ, rt string) string
		parse   func(string) (*grammar.Grammar, error)
		fields  map[string]string
	}{
		{"text struct", resultCheckGrammar, parseTest, fieldCheckFields},
		{"terminal", resultCheckGrammarLegacy, parseLegacyTest, fieldCheckFields},
	} {
		t.Run(model.name, func(t *testing.T) {
			for _, typ := range resultCheckTypesOf(model.fields) {
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
			checked, err := Compile(g, Options{})
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
			untyped, err := Compile(ug, Options{})
			if err != nil {
				t.Fatal(err)
			}
			limits := limits.WorkLimits{MaxSteps: 1 << 30}
			declared := checked.typed.fields["S"][0].t.String() // as the value checker resolves it
			for _, in := range resultCheckInputs() {
				value := fieldCheckValues[in[:1]]
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

// TestRuleResultChecksAreSelective checks that a result is never checked when it runs: a result
// not statically known to fit the rule's declared type is rejected when the grammar is compiled,
// whether or not the rule has an action, and a cast is the only check, which compiled files and
// generated parsers keep.
func TestRuleResultChecksAreSelective(t *testing.T) { eachMode(t, testRuleResultChecksAreSelective) }

func testRuleResultChecksAreSelective(t *testing.T) {
	const decls = `type Int = { Text string }
type S = { F Int }
type O = { X Int }
rule static Int = "s" => Int{Text: "s"}
rule val _ = "v" => Int{Text: "v"}
`
	for _, tc := range []struct{ name, rules, want string }{
		{"an action", "rule main = s:static d:dynamic => S{F: $d}\nrule dynamic Int = v:val => $v\n",
			"rule dynamic produces _, which is not assignable to Int: the type of the value is not known; convert it with a cast, as in value.(Int)"},
		{"no action", "rule main = s:static d:dynamic => S{F: $d}\nrule dynamic Int = val\n",
			"rule dynamic produces _, which is not assignable to Int"},
		{"a node type", "rule main = s:static p:pass => S{F: Int{Text: \"\"}}\nrule pass node[_] = val\n",
			"rule pass produces _, which is not assignable to node[_]"},
		{"an Operators Rule operand", "rule main = s:static o:ops => S{F: Int{Text: \"\"}}\nrule ops Int | O = #operators {\n    operand val\n    operand \"o\" => Int{Text: \"o\"}\n    level { infix left \"+\" => Int{Text: \"+\"} }\n}\n",
			"produces _, which is not assignable to Int | O"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := compileMessage(t, decls+tc.rules); !strings.Contains(got, tc.want) {
				t.Fatalf("got %q; want an error containing %q", got, tc.want)
			}
		})
	}
	src := decls + `rule main = s:static d:dynamic p:pass o:ops => S{F: $d}
rule dynamic Int = v:val => $v.(Int)
rule pass node[_] = v:val => $v.(node[_])
rule ops Int | O = #operators {
    operand v:val => $v.(Int | O)
    operand "o" => Int{Text: "o"}
    level { infix left "+" => Int{Text: "+"} }
}
`
	prog := mustCompile(t, src)
	d := prog.Module().Disassemble()
	if n := strings.Count(d, "check="); n != 0 {
		t.Fatalf("a result is checked when it runs:\n%s", d)
	}
	if n := strings.Count(d, "ECAST"); n != 3 {
		t.Fatalf("disassembly has %d casts, want 3:\n%s", n, d)
	}
	data, err := prog.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadProgram(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{"svvv", "svvo+o", "svvv+o"} {
		want := resultWith(prog, in, ParseOptions{})
		if strings.Contains(want, `"err"`) {
			t.Fatalf("%q: %s", in, want)
		}
		for _, b := range []Backend{Closure, Bytecode} {
			got := resultWith(loaded, in, ParseOptions{Backend: b})
			if b == Closure {
				if !strings.Contains(got, "closure backend needs the grammar source") {
					t.Fatalf("%q loaded %s: %s, want the closure backend refused", in, b, got)
				}
				continue
			}
			if got != want {
				t.Fatalf("%q loaded %s: %s\nwant %s", in, b, got, want)
			}
		}
	}
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	code, err := generateCode(g, gen.GenOptions{Package: "p", Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(code), "CheckResult("); n != 0 {
		t.Fatalf("generated %d result checks", n)
	}
	if n := strings.Count(string(code), "c.Cast("); n != 3 {
		t.Fatalf("generated %d casts, want 3", n)
	}
}

// operatorResultCheckGrammar has operator lines with actions whose values are dynamically typed:
// input "o<value>^" applies the postfix operator, "!<value>o" the prefix one and "o+<value>o" the
// infix one, each forwarding the value it captures. Input "<<value>#" reaches the left-recursive
// rule lr, which generated typed parsers call like Operators Rules, through their rule table, and
// "@<value>@" the rule rr, whose recovery expression keeps it from a direct typed body. Input
// "%<value>%" constructs W in a rule with a recovery expression, which compiling copies.
const operatorResultCheckGrammar = `type Int = { Text string }
type W = { V []Int }
rule main W = ml / mr / mf / me
rule mf W = "%" v:val ("%" #recover(skip="%")) => W{V: $v.([]Int)}
rule mr W = "@" v:rr => W{V: $v}
rule rr []Int = v:val ("@" #recover(skip="@")) => $v.([]Int)
rule ml W = "<" v:lr => W{V: $v}
rule me W = v:e => W{V: $v}
rule lr []Int = (v:lr "^" / v:val "#") => $v.([]Int)
rule e []Int = #operators {
    operand "o" => []Int{Int{Text: "o"}}
    level { infix left "+" v:val => $v.([]Int) }
    level { prefix "!" v:val => $v.([]Int) }
    level { postfix v:val "^" => $v.([]Int) }
}
rule val _ = vm / vt / vl / vz
rule vm = "m"
rule vt = "t" => Int{Text: "t"}
rule vl = "l" => []Int{Int{Text: "l"}}
rule vz = "z" => nil
`

var operatorResultCheckInputs = []string{"om^", "ot^", "ol^", "oz^", "!mo", "!lo", "!zo", "o+mo", "o+lo", "o+to", "ol^+lo", "<l#", "<m#", "<l#^", "<t#^", "<z#", "@l@", "@m@", "@t@", "%l%", "%m%", "%z%"}

// TestOperatorResultChecks checks the values of operator lines with actions, which are finished
// by the operator application rather than by the rule, and the checks of rules that compiling
// copies, in every program form.
func TestOperatorResultChecks(t *testing.T) {
	g, err := parseTest(operatorResultCheckGrammar)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := Compile(g, Options{})
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
	for _, in := range operatorResultCheckInputs {
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

// textStructOperatorsRuleGrammar is the text struct counterpart of terminalOperatorsRuleGrammar:
// the Operators Rule produces the text struct through its actions.
const textStructOperatorsRuleGrammar = `type Int = { Text string }
type S = { F Int }
rule main = v:e => S{F: $v}
rule e Int = #operators {
    operand v:val => $v
    level { infix left "+" v:val => $v }
}
rule val Int = t:"m" #atomic => Int{Text: text($t)}
`

var terminalOperatorsRuleInputs = []string{"m", "m+m", "m+", ""}

func TestTerminalOperatorsRuleNeedsAction(t *testing.T) {
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
	prog := mustCompile(t, textStructOperatorsRuleGrammar)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		o := ParseOptions{Backend: b}
		for _, in := range terminalOperatorsRuleInputs {
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

func resultCheckCases() []genCase {
	var cases []genCase
	for _, typ := range resultCheckTypes() {
		cases = append(cases, genCase{"result check " + typ, resultCheckGrammar(typ, typ), resultCheckInputs()})
	}
	return append(cases,
		genCase{"operator result check", operatorResultCheckGrammar, operatorResultCheckInputs},
		genCase{"text struct Operators Rule", textStructOperatorsRuleGrammar, terminalOperatorsRuleInputs})
}

func TestGeneratedRuleResultChecks(t *testing.T) { eachMode(t, testGeneratedRuleResultChecks) }

func testGeneratedRuleResultChecks(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	cases := resultCheckCases()
	testGeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{})
	testGeneratedTypesCorpus(t, goBin, cases, gen.GenOptions{ConvertTypes: true})
	if node, err := exec.LookPath("node"); err == nil {
		testGeneratedTSParsersCorpus(t, node, cases, gen.GenOptions{})
	} else {
		t.Error("Node is required for result-check parity")
	}
}

// TestGeneratedCheckTypeName checks that a grammar type named like the generated check type
// is renamed in typed TypeScript.
func TestGeneratedCheckTypeName(t *testing.T) {
	src := `type Int = { Text string }
type FieldType = { Text string }
type S = { F Int }
rule main = v:val f:ft => S{F: $v.(Int)}
rule ft FieldType = "f" => FieldType{Text: "f"}
rule val _ = "v" => Int{Text: "v"}
`
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `const ok = p.parseAST("vf");
check(ok.error === null && ok.value !== null && stable(ok.value.F).length > 0, "constructed S");
const no = p.parseAST("v");
check(no.error !== null, "missing f");
`
	runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main"}, "")
}
