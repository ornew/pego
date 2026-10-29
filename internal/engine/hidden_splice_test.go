package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
)

// A hidden rule without an action makes no node of its own: a call of it contributes what its
// expression would, written in its place as a group. Each case gives the grammar written with
// the hidden rule and the same grammar with its expression written where it is called.
type spliceCase struct {
	name    string
	hidden  string
	inlined string
	inputs  []string
}

func spliceCases() []spliceCase {
	return []spliceCase{
		{"sequence", `rule main = "a" _m "d"
rule _m = "b" "c"`, `rule main = "a" ("b" "c") "d"`, []string{"abcd", "abd", "acd", "ad", "abc", ""}},
		{"one element", `rule main = "a" _m "d"
rule _m = "b"`, `rule main = "a" ("b") "d"`, []string{"abd", "ad", "abcd", ""}},
		{"choice", `rule main = "a" _m "d"
rule _m = "b" / "c" "e"`, `rule main = "a" ("b" / "c" "e") "d"`, []string{"abd", "aced", "acd", "ad", ""}},
		{"nested", `rule main = "a" _m "d"
rule _m = "b" _n
rule _n = "c" "e"`, `rule main = "a" ("b" ("c" "e")) "d"`, []string{"abced", "abcd", "ad", ""}},
		{"repetition", `rule main = "a" _m* "d"
rule _m = "b" "c"`, `rule main = "a" ("b" "c")* "d"`, []string{"ad", "abcd", "abcbcd", "abd", ""}},
		{"optional", `rule main = "a" _m? "d"
rule _m = "b" "c"`, `rule main = "a" ("b" "c")? "d"`, []string{"ad", "abcd", "abd", "abcbcd", ""}},
		{"whole body", `rule main = _m
rule _m = "b" "c"`, `rule main = ("b" "c")`, []string{"bc", "b", ""}},
		{"hidden captures", `rule main = "a" _m "d"
rule _m = x:"b" "c"`, `rule main = "a" ("b" "c") "d"`, []string{"abcd", "abd", ""}},
		{"captured call", `rule main = x:_m "d"
rule _m = "b" "c"`, `rule main = x:("b" "c") "d"`, []string{"bcd", "bd", ""}},
		{"no value", `rule main = "a" _m "b"
rule _m = &"b"`, `rule main = "a" (&"b") "b"`, []string{"ab", "a", ""}},
		{"lookahead in body", `rule main = "a" _m "c"
rule _m = !"x" "b"`, `rule main = "a" (!"x" "b") "c"`, []string{"abc", "axc", "ac", ""}},
		{"used twice", `rule main = _m "-" _m
rule _m = "a" "b"`, `rule main = ("a" "b") "-" ("a" "b")`, []string{"ab-ab", "ab-a", "ab", ""}},
		{"in a choice", `rule main = _m / "x"
rule _m = "a" "b"`, `rule main = ("a" "b") / "x"`, []string{"ab", "x", "a", ""}},
		{"in a repetition body", `rule main = (_m "-")*
rule _m = "a" "b"`, `rule main = (("a" "b") "-")*`, []string{"", "ab-", "ab-ab-", "ab"}},
		{"recovery", `rule main = (_m ";")* $$ #recover(skip=[^;]+ ";")
rule _m = [a-z]+ #atomic`, `rule main = (([a-z]+ #atomic) ";")* $$ #recover(skip=[^;]+ ";")`, []string{"ab;cd;", "ab;1x;cd;", "1x;", "ab"}},
	}
}

func TestHiddenRulesSpliceTheirExpression(t *testing.T) {
	for _, tc := range spliceCases() {
		t.Run(tc.name, func(t *testing.T) {
			for _, opts := range []compile.Options{{}, {DefaultValue: true}, {SourceInfo: true}, {DisableMemo: true}} {
				hidden, inlined := compileBare(t, tc.hidden, opts), compileBare(t, tc.inlined, opts)
				matched := false
				for _, input := range tc.inputs {
					if _, err := inlined.Parse("main", input); err == nil {
						matched = true
					}
					for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
						want := resultJSON(inlined.ParseWith("main", input, ParseOptions{Backend: backend}))
						got := resultJSON(hidden.ParseWith("main", input, ParseOptions{Backend: backend}))
						if got != want {
							t.Errorf("%+v %q on %s:\nhidden:  %s\ninlined: %s", opts, input, backend, got, want)
						}
					}
				}
				if !matched {
					t.Fatal("no input is accepted")
				}
			}
		})
	}
}

// The elements of a hidden rule are the elements of its caller whatever the nesting.
func TestHiddenRulesSpliceInPlace(t *testing.T) {
	check(t, `rule main = "a" _m "d"
rule _m = x:"b" "c"`, ok("abcd", `("a" "b" "c" "d")@main`))
	check(t, `rule main = _l
rule _l = "x" _l / "x"`, ok("xxx", `("x" ("x" "x"))`), fails("", `expected "x"`))
	check(t, `rule main = _e $$
rule _e = _e "+" "a" / "a"`, ok("a+a+a", `((("a" "+" "a") "+" "a"))@main`))
}

// A hidden rule is called as a rule: it is memoized, and it may recur.
func TestHiddenRulesAreCalledAsRules(t *testing.T) {
	p := mustCompile(t, `rule main = _m "!" / _m "?"
rule _m = "a" "b"`)
	if got := result(p, "ab?"); got != `("a" "b" "?")@main` {
		t.Fatal(got)
	}
}

// A hidden rule whose elements contain themselves has no fixed number of them.
func TestHiddenRulesCannotContainThemselvesFlat(t *testing.T) {
	for _, src := range []string{
		"rule main = _a\nrule _a = \"x\" _a",
		"rule main = _a\nrule _a = \"x\" _b\nrule _b = _a \"y\"",
	} {
		msg := compileError(t, src)
		if !strings.Contains(msg, "contains itself") {
			t.Errorf("%s: %s", src, msg)
		}
	}
}

// A cut in a hidden rule commits the choices of the rule, not those around the call.
func TestHiddenRulesScopeTheirCuts(t *testing.T) {
	check(t, `rule main = _m / "a" "c"
rule _m = "a" #cut "b"`, ok("ab", `("a" "b")@main`), ok("ac", `("a" "c")@main`))
	check(t, `rule main = ("a" #cut "b") / "a" "c"`, ok("ab", `("a" "b")@main`), fails("ac", `expected "b"`))
}

// spliceGenCases returns the hidden-rule cases for the corpus that compares the backends and the
// generated parsers.
func spliceGenCases() []genCase {
	var cases []genCase
	for _, tc := range spliceCases() {
		cases = append(cases, genCase{"hidden rule: " + tc.name, tc.hidden, tc.inputs})
	}
	return append(cases,
		genCase{"hidden rule: right recursion", `rule main = _l
rule _l = "x" _l / "x"`, []string{"x", "xxx", "", "xy"}},
		genCase{"hidden rule: left recursion", `rule main = _e $$
rule _e = _e "+" "a" / "a"`, []string{"a", "a+a+a", "a+", ""}},
		genCase{"hidden rule: optional recursion", `rule main = _t
rule _t = "(" _t? ")"`, []string{"()", "(())", "(()", ""}},
		genCase{"hidden rule: cut", `rule main = _m / "a" "c"
rule _m = "a" #cut "b"`, []string{"ab", "ac", "a", ""}},
	)
}
