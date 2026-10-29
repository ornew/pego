package engine

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/syntax"
)

// An action error names the inline action by the rule that holds it, and the explicit rule by its
// name: the two spellings of the same hidden rule.
var inlineWhere = regexp.MustCompile(`(\w+) \(inline action (\d+)\)`)

func hiddenWhere(s string) string { return inlineWhere.ReplaceAllString(s, "_${1}_${2}") }

func TestInlineActionsMatchRules(t *testing.T) {
	t.Parallel()
	for _, tc := range inlineCases(t) {
		t.Run(tc.Name, func(t *testing.T) {
			inline, explicit := mustCompile(t, tc.Inline), mustCompile(t, tc.Explicit)
			matched := false
			for _, input := range tc.Inputs {
				if _, err := inline.Parse("main", input); err == nil {
					matched = true
				}
			}
			if !matched && tc.Name != "action error" {
				t.Fatal("no input is accepted")
			}
			for _, input := range tc.Inputs {
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

func inlineCases(t *testing.T) []gentest.InlineCase {
	t.Helper()
	return gentest.InlineActionCases()
}

func TestInlineActionsAreRejectedWhereTheirValueIsUnused(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	msg := compileError(t, `rule main = (x:"a" => { y := $x; $y })`)
	if !strings.Contains(msg, "action statement blocks are not implemented") {
		t.Fatal(msg)
	}
}

// The type of an inline action is that of a rule: it is checked at compile time and appears in the
// typed values and the schema as the type of the item.
func TestInlineActionsAreTyped(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	for _, tc := range gentest.InlineActionCases() {
		t.Run(tc.Name, func(t *testing.T) {
			prog := mustCompile(t, tc.Inline)
			written, err := syntax.Parse(tc.Inline)
			if err != nil {
				t.Fatal(err)
			}
			if got := prog.SourceGrammar(); len(got.Rules()) != len(written.Rules()) || len(prog.Grammar.Rules()) <= len(written.Rules()) {
				t.Fatalf("the source grammar has %d rules, the lowered grammar %d, the written one %d", len(got.Rules()), len(prog.Grammar.Rules()), len(written.Rules()))
			}
			for _, mode := range valueModes {
				loaded := saveLoad(t, prog, "main", mode)
				for _, input := range tc.Inputs {
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
