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

// The tests here compare the typed rules printed from Flow IR (gen_flow.go) with the direct rules
// of dgen, generating every corpus grammar with and without GenOptions.flowTyped.

// flowModes are the generator options under which flowTyped prints rules from Flow IR, lean and
// with work controls; frames
// reports that it also prints the bodies and lines under runtime-owned frames.
var flowModes = []struct {
	name   string
	opts   GenOptions
	frames bool
}{
	{"lean typed-only", GenOptions{Types: true, TypedOnly: true}, true},
	{"lean with nodes", GenOptions{Types: true}, true},
	{"lean recognizer", GenOptions{Types: true, Recognize: true}, true},
	{"lean charged", GenOptions{Types: true, TypedOnly: true, disableOrdinaryWorkSpecialization: true}, true},
	{"lean unguarded", GenOptions{Types: true, TypedOnly: true, disableGuardedClasses: true}, true},
	{"lean straight terminals", GenOptions{Types: true, TypedOnly: true, disableTerminalPrograms: true}, true},
	{"lean boxed", GenOptions{Types: true, TypedOnly: true, disableLiteralTextDispatch: true, boxedComparisons: true}, true},
	{"lean without LR bodies", GenOptions{Types: true, TypedOnly: true, disableTypedLRBodies: true}, true},
	{"lean without framed cuts", GenOptions{Types: true, TypedOnly: true, disableTypedFramedCuts: true}, true},
	{"lean without recovery bodies", GenOptions{Types: true, TypedOnly: true, disableTypedRecoveryBodies: true}, true},
	{"lean without OperatorsRule bodies", GenOptions{Types: true, TypedOnly: true, disableTypedOperatorsRuleBodies: true}, true},
	{"lean general frame layouts", GenOptions{Types: true, TypedOnly: true, disableTypedFrameLayouts: true}, false},
	{"controlled typed-only", GenOptions{Types: true, TypedOnly: true, WorkControls: true}, true},
	{"controlled with nodes", GenOptions{Types: true, WorkControls: true}, true},
	{"controlled recognizer", GenOptions{Types: true, Recognize: true, WorkControls: true}, true},
	{"controlled unguarded", GenOptions{Types: true, TypedOnly: true, WorkControls: true, disableGuardedClasses: true}, true},
	{"controlled boxed", GenOptions{Types: true, TypedOnly: true, WorkControls: true, disableLiteralTextDispatch: true, boxedComparisons: true}, true},
	{"Document", GenOptions{Types: true, Document: true}, true},
	{"Document with work controls", GenOptions{Types: true, Document: true, WorkControls: true}, true},
}

// flowUnreadLine matches the first line of a direct rule's body or matcher, also of the copy
// that work controls specialize for parses without a control, which assigns the variables
// that no statement reads to the blank identifier. dgen tracks reads where it writes the names
// of variables and Flow IR where an expression uses them, which differ in a few spots (a count
// compared with the minimum of a repetition), so the lines are left out of the comparison; the
// compilation of the generated parsers checks them.
var flowUnreadLine = regexp.MustCompile(`(?m)^\t+_(, _)* = [a-z0-9]+(, [a-z0-9]+)*\n`)

// flowGenerate generates the parser of the grammar source with the options, joined into one
// text, and returns the numbers of rules and of framed bodies and lines printed from Flow IR, or
// reports false if the generator rejects the grammar.
func flowGenerate(src string, opts GenOptions) (string, int, int, bool) {
	g, err := parseTest(src)
	if err != nil {
		return "", 0, 0, false
	}
	dec := &genDecisions{}
	opts.Module, opts.keepComments, opts.decisions = "example.com/p", true, dec
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
	if dec.gen != nil {
		n, frames = dec.gen.flowRules, dec.gen.flowFrames
	}
	return b.String(), n, frames, true
}

// TestFlowMatchesDirectRules checks that printing the typed rules from Flow IR writes the parser
// that dgen writes, apart from the lines of unread variables, and that the comparison covers rules
// and, where the mode inlines them, framed bodies and lines printed from Flow IR.
func TestFlowMatchesDirectRules(t *testing.T) {
	cases := append(schemaCorpus(t), coreExtraGrammars...)
	for _, mode := range flowModes {
		t.Run(mode.name, func(t *testing.T) {
			compared, grammars, printed, framed := 0, 0, 0, 0
			for _, gc := range cases {
				want, n, frames, ok := flowGenerate(gc.src, mode.opts)
				if !ok {
					continue
				}
				if n != 0 || frames != 0 {
					t.Fatalf("%s: %d rules and %d framed bodies printed from Flow IR without flowTyped", gc.name, n, frames)
				}
				opts := mode.opts
				opts.flowTyped = true
				got, n, frames, ok := flowGenerate(gc.src, opts)
				if !ok {
					t.Errorf("%s: rejected with flowTyped", gc.name)
					continue
				}
				compared++
				if n > 0 {
					grammars++
					printed += n
				}
				framed += frames
				w, g := flowUnreadLine.ReplaceAllString(want, ""), flowUnreadLine.ReplaceAllString(got, "")
				if w != g {
					t.Errorf("%s: output differs:\n%s", gc.name, firstDiff(w, g))
				}
			}
			t.Logf("compared %d grammars, %d with %d rules printed from Flow IR; %d framed bodies and lines", compared, grammars, printed, framed)
			if compared < 100 || grammars < 100 {
				t.Errorf("compared %d grammars, %d with rules printed from Flow IR", compared, grammars)
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
		fmt.Fprintf(&out, "%s %4d dgen: %s\n%s      flow: %s\n", mark, k+1, x, mark, y)
	}
	return out.String()
}

// TestFlowTypedCorpus builds the parsers of the typed corpus with and without work controls, with
// flowTyped printing rules from Flow IR, and checks that they parse every input as those of dgen
// do: the comparison of their text leaves out the lines of unread variables, which the compiler
// checks here.
func TestFlowTypedCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	for _, ordinary := range []string{"", "1"} {
		t.Run("ordinary="+ordinary, func(t *testing.T) {
			t.Setenv("PEGO_TEST_ORDINARY", ordinary)
			testGeneratedTypesCorpus(t, goBin, generatedTypesCases(t), GenOptions{flowTyped: true})
		})
	}
}

// TestFlowTypedDocument builds typed Document parsers with flowTyped printing rules from Flow IR
// and checks them as TestGenerateDirectTypedDocument does.
func TestFlowTypedDocument(t *testing.T) {
	testDirectTypedDocument(t, GenOptions{flowTyped: true})
}

// TestFlowTypedModes pins the options under which flowTyped leaves every rule on dgen and the
// general code: the parser is the one written without it.
func TestFlowTypedModes(t *testing.T) {
	cases := append(schemaCorpus(t), coreExtraGrammars...)
	for _, mode := range []coreGenMode{
		{"observable", GenOptions{Types: true, TypedOnly: true, Observability: true}},
		{"general cuts", GenOptions{Types: true, TypedOnly: true, disableTypedCuts: true}},
		{"frame layouts", GenOptions{Types: true, TypedOnly: true, disableTypedLocalLayouts: true}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			compared := 0
			for _, gc := range cases {
				want, _, _, ok := flowGenerate(gc.src, mode.opts)
				if !ok {
					continue
				}
				opts := mode.opts
				opts.flowTyped = true
				got, n, frames, _ := flowGenerate(gc.src, opts)
				if n != 0 || frames != 0 || got != want {
					t.Errorf("%s: %d rules and %d framed bodies printed from Flow IR, same output %v", gc.name, n, frames, got == want)
				}
				compared++
			}
			if compared < 100 {
				t.Errorf("compared %d grammars", compared)
			}
		})
	}
}
