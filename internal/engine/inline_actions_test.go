package engine

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// An action error names the inline action by the rule that holds it, and the explicit rule by its
// name: the two spellings of the same hidden rule.
var inlineWhere = regexp.MustCompile(`(\w+) \(inline action (\d+)\)`)

func hiddenWhere(s string) string { return inlineWhere.ReplaceAllString(s, "_${1}_${2}") }

// An inline action (e => A) is an anonymous rule embedded at its position. Each case gives the
// grammar written with inline actions and the same grammar with every inline action written as a
// rule, named as the compiler names the hidden rule (r_inline_n). The two must behave the same
// on every input, on every backend.
type inlineCase struct {
	name     string
	inline   string
	explicit string
	inputs   []string
}

func inlineActionCases() []inlineCase {
	return []inlineCase{
		{"sequence", `type A = { X string }
rule main = (x:"a" "b" => A{X: text($x)}) "c"`, `type A = { X string }
rule main = _main_1 "c"
rule _main_1 = x:"a" "b" => A{X: text($x)}`, []string{"abc", "ab", "ac", "xbc", ""}},
		{"whole rule", `rule main = (x:"a" => $x)`, `rule main = _main_1
rule _main_1 = x:"a" => $x`, []string{"a", "b", "aa", ""}},
		{"choice", `rule main = (x:("a" / "b") => $x)`, `rule main = _main_1
rule _main_1 = x:("a" / "b") => $x`, []string{"a", "b", "c", ""}},
		{"choice of inline actions", `rule main = (x:"a" => $x) / (y:"b" => $y)`, `rule main = _main_1 / _main_2
rule _main_1 = x:"a" => $x
rule _main_2 = y:"b" => $y`, []string{"a", "b", "c", ""}},
		{"nested", `rule main = (x:(y:"a" => $y) "b" => $x)`, `rule main = _main_2
rule _main_1 = y:"a" => $y
rule _main_2 = x:_main_1 "b" => $x`, []string{"ab", "a", "b", ""}},
		{"outer capture stays", `type P = { L string; R string }
rule main = l:"a" r:(x:"b" => text($x)) => P{L: text($l), R: $r}`, `type P = { L string; R string }
rule main = l:"a" r:_main_1 => P{L: text($l), R: $r}
rule _main_1 = x:"b" => text($x)`, []string{"ab", "a", "b", ""}},
		{"default action", `rule main = (x:"a" "b" => ...) "c"`, `rule main = _main_1 "c"
rule _main_1 = x:"a" "b" => ...`, []string{"abc", "ab", ""}},
		{"repetition", `rule main = (x:"a" => $x)* "b"`, `rule main = _main_1* "b"
rule _main_1 = x:"a" => $x`, []string{"b", "ab", "aaab", "aaa", ""}},
		{"optional", `rule main = (x:"a" => $x)? "b"`, `rule main = _main_1? "b"
rule _main_1 = x:"a" => $x`, []string{"b", "ab", "aab", ""}},
		{"left recursion", `type Add = { L Expr; R string }
type Num = { T string }
type Expr ~ Add | Num
rule main = e $$
rule e Expr = (l:e "+" r:"a" => Add{L: $l, R: text($r)}) / "a" => Num{T: "a"}`, `type Add = { L Expr; R string }
type Num = { T string }
type Expr ~ Add | Num
rule main = e $$
rule e Expr = _e_1 / "a" => Num{T: "a"}
rule _e_1 = l:e "+" r:"a" => Add{L: $l, R: text($r)}`, []string{"a", "a+a", "a+a+a", "a+", ""}},
		{"cut", `rule main = (x:"a" #cut "b" => $x) / "a" "c"`, `rule main = _main_1 / "a" "c"
rule _main_1 = x:"a" #cut "b" => $x`, []string{"ab", "ac", "a", ""}},
		{"recovery", `rule main = (x:[a-z]+ #atomic ";" => $x)* $$ #recover(skip=[^;]+ ";")`, `rule main = _main_1* $$ #recover(skip=[^;]+ ";")
rule _main_1 = x:[a-z]+ #atomic ";" => $x`, []string{"ab;cd;", "ab;1x;cd;", "1x;", "ab"}},
		{"recovery inside", `rule main = (x:[a-z]+ #atomic ";" #recover(skip=[^;]+ ";") => $x)+ $$`, `rule main = _main_1+ $$
rule _main_1 = x:[a-z]+ #atomic ";" #recover(skip=[^;]+ ";") => $x`, []string{"ab;cd;", "ab;1x;cd;", "1x;", "ab"}},
		{"action error", `rule main = (x:"a" => 1 / 0) "b"`, `rule main = _main_1 "b"
rule _main_1 = x:"a" => 1 / 0`, []string{"ab", "a"}},
		{"recover argument", `rule main = (x:[a-z]+ #atomic ";")* $$ #recover(skip=(y:[^;]+ => text($y)) ";")`, `rule main = (x:[a-z]+ #atomic ";")* $$ #recover(skip=_main_1 ";")
rule _main_1 = y:[^;]+ => text($y)`, []string{"ab;cd;", "ab;1x;cd;", "1x;", "ab"}},
		{"stream", `rule main = (x:[a-z]+ #atomic ";" => text($x))* #stream`, `rule main = _main_1* #stream
rule _main_1 = x:[a-z]+ #atomic ";" => text($x)`, []string{"", "ab;", "ab;cd;", "ab"}},
		{"operators rule operand", `type N = { T string }
rule main = #operators {
    operand (t:[0-9]+ #atomic => N{T: text($t)})
    level { infix left "+" }
}`, `type N = { T string }
rule main = #operators {
    operand _main_1
    level { infix left "+" }
}
rule _main_1 = t:[0-9]+ #atomic => N{T: text($t)}`, []string{"1", "1+2", "1+2+3", "+", ""}},
		{"nested in a whole rule", `rule main = (x:(y:"a" => text($y)) "b" => $x)`, `rule main = _main_2
rule _main_1 = y:"a" => text($y)
rule _main_2 = x:_main_1 "b" => $x`, []string{"ab", "a", ""}},
		{"captures of the enclosing rule after", `rule main = a:"a" (b:"b" => $b) => $a`, `rule main = a:"a" _main_1 => $a
rule _main_1 = b:"b" => $b`, []string{"ab", "a", "b"}},
	}
}

// inlineActionGenCases returns the inline cases for the corpus that compares the backends and the
// generated parsers.
func inlineActionGenCases() []genCase {
	var cases []genCase
	for _, tc := range inlineActionCases() {
		cases = append(cases, genCase{"inline action: " + tc.name, tc.inline, tc.inputs})
	}
	return cases
}

func TestInlineActionsMatchRules(t *testing.T) {
	for _, tc := range inlineCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			inline, explicit := mustCompile(t, tc.inline), mustCompile(t, tc.explicit)
			matched := false
			for _, input := range tc.inputs {
				if _, err := inline.Parse("main", input); err == nil {
					matched = true
				}
			}
			if !matched && tc.name != "action error" {
				t.Fatal("no input is accepted")
			}
			for _, input := range tc.inputs {
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					want := resultJSON(explicit.ParseWith("main", input, ParseOptions{Backend: backend}))
					got := hiddenWhere(resultJSON(inline.ParseWith("main", input, ParseOptions{Backend: backend})))
					if got != want {
						t.Errorf("%q on %s:\ninline:   %s\nexplicit: %s", input, backend, got, want)
					}
				}
			}
		})
	}
}

func inlineCases(t *testing.T) []inlineCase {
	t.Helper()
	return inlineActionCases()
}

func TestInlineActionsAreRejectedWhereTheirValueIsUnused(t *testing.T) {
	for _, src := range []string{
		`rule main = &(x:"a" => $x) "a"`,
		`rule main = !(x:"a" => $x) "b"`,
		`rule main = -(x:"a" => $x) "b"`,
		`rule main = &("a" (x:"b" => $x)) "a" "b"`,
	} {
		msg := compileError(t, src)
		if !strings.Contains(msg, "an inline action is not allowed in a lookahead or a discard") {
			t.Errorf("%s: %s", src, msg)
		}
	}
}

func TestInlineActionsSeeOnlyTheirOwnCaptures(t *testing.T) {
	msg := compileError(t, `rule main = y:"a" (x:"b" => $y)`)
	if !strings.Contains(msg, "undefined capture $y") {
		t.Fatal(msg)
	}
	// The inline action closes its captures: the enclosing rule cannot read them.
	msg = compileError(t, `rule main = (x:"a" => $x) "b" => $x`)
	if !strings.Contains(msg, "undefined capture $x") {
		t.Fatal(msg)
	}
}

func TestInlineActionsKeepBlocksUnsupported(t *testing.T) {
	msg := compileError(t, `rule main = (x:"a" => { y := $x; $y })`)
	if !strings.Contains(msg, "action statement blocks are not implemented") {
		t.Fatal(msg)
	}
}

// The type of an inline action is that of a rule: it is checked at compile time and appears in the
// typed values and the schema as the type of the item.
func TestInlineActionsAreTyped(t *testing.T) {
	msg := compileError(t, `type A = { X string }
rule main = (x:"a" => A{X: $x}) "b"`)
	if !strings.Contains(msg, "cannot use node[string] as string in field X of A") {
		t.Fatal(msg)
	}
	p := compileBare(t, `type A = { X string }
rule main = (x:"a" => A{X: text($x)}) "b"`)
	s, err := outputSchemaPrototype(p, "main", false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range s.Rules {
		if r.Name == "_main_1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the schema has no rule for the inline action: %+v", s.Rules)
	}
	hasA := false
	for _, typ := range s.Types {
		if typ.Name == "A" {
			hasA = true
		}
	}
	if !hasA {
		t.Fatalf("the schema has no type A: %+v", s.Types)
	}
}

// The grammar of a program is the grammar as written, and the compiled form keeps it: loading it
// gives the same results, with the inline actions still in the AST.
func TestInlineActionsCompiledRoundTrip(t *testing.T) {
	for _, tc := range inlineActionCases() {
		t.Run(tc.name, func(t *testing.T) {
			prog := mustCompile(t, tc.inline)
			written, err := syntax.Parse(tc.inline)
			if err != nil {
				t.Fatal(err)
			}
			if got := prog.SourceGrammar(); len(got.Rules()) != len(written.Rules()) || len(prog.Grammar.Rules()) <= len(written.Rules()) {
				t.Fatalf("the source grammar has %d rules, the lowered grammar %d, the written one %d", len(got.Rules()), len(prog.Grammar.Rules()), len(written.Rules()))
			}
			for _, mode := range valueModes {
				loaded := saveLoad(t, prog, "main", mode)
				for _, input := range tc.inputs {
					want, got := resultJSON(prog.Parse("main", input)), resultJSON(loaded.ParseWith("main", input, servedBy(mode, ParseOptions{})))
					if got != want {
						t.Errorf("%q (%s mode): %s / %s", input, mode, got, want)
					}
				}
				// A compiled grammar holds no grammar, so the hidden rules of inline actions are
				// only in its module.
				if loaded.SourceGrammar() != nil {
					t.Fatalf("the loaded program has a grammar of %d rules", len(loaded.SourceGrammar().Rules()))
				}
			}
		})
	}
}
