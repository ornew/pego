package engine

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// The tests here check the typed rules printed from Flow IR (gen_flow.go), generating every corpus
// grammar in the modes of flowModes. The engine oracle of the corpus tests checks their values.

// flowModes are the generator options under which the default prints rules from Flow IR, lean and
// with work controls; frames reports that it also prints the bodies and lines under runtime-owned
// frames.
var flowModes = []struct {
	name   string
	opts   GenOptions
	frames bool
}{
	{"lean typed-only", GenOptions{Types: true, TypedOnly: true}, true},
	{"lean with nodes", GenOptions{Types: true}, true},
	{"lean recognizer", GenOptions{Types: true, Recognize: true}, true},
	{"lean unguarded", GenOptions{Types: true, TypedOnly: true, disableGuardedClasses: true}, true},
	{"lean straight terminals", GenOptions{Types: true, TypedOnly: true, disableTerminalPrograms: true}, true},
	{"lean boxed", GenOptions{Types: true, TypedOnly: true, disableLiteralTextDispatch: true, boxedComparisons: true}, true},
	{"controlled typed-only", GenOptions{Types: true, TypedOnly: true, WorkControls: true}, true},
	{"controlled with nodes", GenOptions{Types: true, WorkControls: true}, true},
	{"controlled recognizer", GenOptions{Types: true, Recognize: true, WorkControls: true}, true},
	{"controlled unguarded", GenOptions{Types: true, TypedOnly: true, WorkControls: true, disableGuardedClasses: true}, true},
	{"controlled boxed", GenOptions{Types: true, TypedOnly: true, WorkControls: true, disableLiteralTextDispatch: true, boxedComparisons: true}, true},
	{"Document", GenOptions{Types: true, Document: true}, true},
	{"Document with work controls", GenOptions{Types: true, Document: true, WorkControls: true}, true},
	{"observable typed-only", GenOptions{Types: true, TypedOnly: true, Observability: true}, true},
	{"observable with nodes", GenOptions{Types: true, Observability: true}, true},
	{"observable controlled", GenOptions{Types: true, TypedOnly: true, Observability: true, WorkControls: true}, true},
	{"observable Document", GenOptions{Types: true, Observability: true, Document: true}, true},
}

// flowUnreadLine matches the line that assigns the variables no statement reads to the blank
// identifier, at the start of a direct rule's body or matcher. flowDiff leaves that line out of
// the comparison; the compilation of the generated parsers checks it.
var flowUnreadLine = regexp.MustCompile(`(?m)^\t+_(, _)* = [a-z0-9]+(, [a-z0-9]+)*\n`)

// flowDiff is the determinism check: it returns the lines around the first difference between
// two generations of the same grammar and options, apart from the lines of unread variables, or
// "" if they are the same.
func flowDiff(want, got string) string {
	w, g := flowUnreadLine.ReplaceAllString(want, ""), flowUnreadLine.ReplaceAllString(got, "")
	if w == g {
		return ""
	}
	return firstDiff(w, g)
}

// TestFlowDiffDetectsMutations checks that flowDiff reports a parser that differs from a second
// generation of the same options in a statement, also next to the line of unread
// variables that it leaves out, while it ignores a change of that line alone.
func TestFlowDiffDetectsMutations(t *testing.T) {
	opts := GenOptions{Types: true, TypedOnly: true}
	mutations := []struct {
		name   string
		mutate func(string) (string, bool)
		found  bool // whether flowDiff must report the mutated parser
	}{
		{"position step", func(s string) (string, bool) {
			return strings.Replace(s, "p.Pos++\n", "p.Pos += 2\n", 1), strings.Contains(s, "p.Pos++\n")
		}, true},
		{"failure jump dropped", func(s string) (string, bool) {
			i := strings.Index(s, "\t\tgoto fail\n")
			if i < 0 {
				return s, false
			}
			return s[:i] + s[i+len("\t\tgoto fail\n"):], true
		}, true},
		{"statement after the unread line", func(s string) (string, bool) {
			loc := flowUnreadLine.FindStringIndex(s)
			if loc == nil {
				return s, false
			}
			return s[:loc[1]] + "\tp.Pos = 0\n" + s[loc[1]:], true
		}, true},
		{"unread line changed", func(s string) (string, bool) {
			loc := flowUnreadLine.FindStringIndex(s)
			if loc == nil {
				return s, false
			}
			return s[:loc[0]] + "\t_ = zz9\n" + s[loc[1]:], true
		}, false},
	}
	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			applied := 0
			for _, gc := range append(schemaCorpus(t), coreExtraGrammars...)[:60] {
				want, _, _, ok := flowGenerate(gc.src, opts)
				if !ok {
					continue
				}
				got, n, _, _ := flowGenerate(gc.src, opts)
				if n == 0 || flowDiff(want, got) != "" {
					continue
				}
				mutated, ok := m.mutate(got)
				if !ok {
					continue
				}
				applied++
				if found := flowDiff(want, mutated) != ""; found != m.found {
					t.Errorf("%s: difference reported %v, want %v", gc.name, found, m.found)
				}
			}
			if applied < 10 {
				t.Errorf("mutation applied to %d grammars", applied)
			}
		})
	}
}

// flowGenerate generates the parser of the grammar source with the options, joined into one
// text, and returns the numbers of rules and of framed bodies and lines printed from Flow IR, or
// reports false if the generator rejects the grammar.
func flowGenerate(src string, opts GenOptions) (string, int, int, bool) {
	g, err := parseTest(src)
	if err != nil {
		return "", 0, 0, false
	}
	var gen *generator
	generatorHook = func(x *generator) { gen = x }
	defer func() { generatorHook = nil }()
	opts.Module, opts.keepComments = "example.com/p", true
	files, err := generate(g, opts)
	if err != nil {
		return "", 0, 0, false
	}
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "== %s\n", f.Path)
		b.Write(f.Content)
	}
	n, frames := 0, 0
	if gen != nil {
		n, frames = gen.flowRules, gen.flowFrames
	}
	return b.String(), n, frames, true
}

// TestFlowPrintsTypedRules checks that every corpus grammar generates in every flowModes mode with
// its typed rules printed from Flow IR, and counts the bodies and lines printed under runtime-owned
// frames where the mode inlines them.
func TestFlowPrintsTypedRules(t *testing.T) {
	cases := append(schemaCorpus(t), coreExtraGrammars...)
	for _, mode := range flowModes {
		t.Run(mode.name, func(t *testing.T) {
			compared, grammars, printed, framed := 0, 0, 0, 0
			for _, gc := range cases {
				_, n, frames, ok := flowGenerate(gc.src, mode.opts)
				if !ok {
					t.Errorf("%s: rejected", gc.name)
					continue
				}
				compared++
				if n > 0 {
					grammars++
					printed += n
				}
				framed += frames
			}
			t.Logf("generated %d grammars, %d with %d rules printed from Flow IR; %d framed bodies and lines", compared, grammars, printed, framed)
			if compared < 100 || grammars < 100 {
				t.Errorf("generated %d grammars, %d with rules printed from Flow IR", compared, grammars)
			}
			if mode.frames && framed < 50 || !mode.frames && framed != 0 {
				t.Errorf("%d framed bodies and lines printed from Flow IR", framed)
			}
		})
	}
}

// firstDiff returns the lines around the first difference of a and b.
func firstDiff(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	i := 0
	for i < len(al) && i < len(bl) && al[i] == bl[i] {
		i++
	}
	var out bytes.Buffer
	from := max(0, i-8)
	for k := from; k < i+8; k++ {
		x, y := "", ""
		if k < len(al) {
			x = al[k]
		}
		if k < len(bl) {
			y = bl[k]
		}
		mark := " "
		if x != y {
			mark = "!"
		}
		fmt.Fprintf(&out, "%s %4d a: %s\n%s      b: %s\n", mark, k+1, x, mark, y)
	}
	return out.String()
}

// TestFlowTypedCorpus builds the parsers of the typed corpus with and without work controls, with
// the default printing rules from Flow IR, and checks that they parse every input as the engine does.
func TestFlowTypedCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	eachMode(t, func(t *testing.T) {
		testGeneratedTypesCorpus(t, goBin, generatedTypesCases(t), GenOptions{convertTypes: true})
	})
}

// TestFlowTypedObservable builds observable typed parsers printed from Flow IR, with nodes and
// typed-only, for the typed test grammars (a bounded part of the corpus), and checks them against
// the parsers without observability and against the engine's errors.
func TestFlowTypedObservable(t *testing.T) { eachMode(t, testFlowTypedObservable) }

func testFlowTypedObservable(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	var cases []genCase
	for _, c := range generatedTypesCases(t) {
		if strings.HasPrefix(c.name, "typed/") {
			cases = append(cases, c)
		}
	}
	if len(cases) < 40 {
		t.Fatalf("only %d typed test grammars", len(cases))
	}
	for _, mode := range []struct {
		name string
		opts GenOptions
	}{
		{"with nodes", GenOptions{Observability: true}},
		{"typed-only", GenOptions{Observability: true, TypedOnly: true}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			printed, framed := 0, 0
			for _, c := range cases {
				opts := mode.opts
				opts.Types = true
				_, n, frames, _ := flowGenerate(c.src, opts)
				printed, framed = printed+n, framed+frames
			}
			t.Logf("%d rules and %d framed bodies printed from Flow IR", printed, framed)
			if printed < 80 || framed < 10 {
				t.Fatalf("%d rules and %d framed bodies printed from Flow IR", printed, framed)
			}
			testGeneratedTypesCorpus(t, goBin, cases, mode.opts)
		})
	}
}

// TestFlowTypedDocument builds typed Document parsers with the default printing rules from Flow IR
// and checks them as TestGenerateDirectTypedDocument does.
func TestFlowTypedDocument(t *testing.T) {
	testDirectTypedDocument(t, GenOptions{})
}
