package engine

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/ir/core"
)

// The guards of the Core IR (core.Guards) skip an alternative of a choice when the next character
// cannot begin it, and every parser printed from Flow IR relies on that. The decision goldens freeze
// what the guards are; they do not say that a guard is sound. This test does, with the engine as the
// oracle: a guard is sound if the engine never matches its alternative at a position where the guard
// rejects the character there. It is a property of the grammar, so it holds for every input; the test
// checks it on the positions of the inputs of the corpus and of the checked-in grammars.
//
// The engine cannot be asked whether it matched one alternative in a rule, so each guarded alternative
// becomes a rule of its own, "rule guardprobeN = (alternative) .*", added to the grammar: the rule matches
// at a position if and only if the alternative does, and the rest of the input is consumed by .*.
// An alternative that does not compile alone (it reads a capture of the sequence around it) is not
// probed, and the test counts how many are.

// guardAccepts reports whether the guard f lets the character ch begin its alternative.
func guardAccepts(f *core.First, ch rune) bool {
	if f.Lit {
		return ch == f.Rune || f.Fold && ch == f.Rune-('a'-'A')
	}
	in := false
	for _, r := range f.Class.Ranges {
		if r.Lo <= ch && ch <= r.Hi {
			in = true
		}
	}
	return in != f.Class.Negated
}

// guardNarrowed returns the character that the mutant of the guard f stops accepting: the first
// character of its literal or of its first range. It reports false for a guard it does not narrow.
func guardNarrowed(f *core.First) (rune, bool) {
	switch {
	case f.Lit:
		return f.Rune, true
	case !f.Class.Negated && len(f.Class.Ranges) > 0:
		return f.Class.Ranges[0].Lo, true
	}
	return 0, false
}

// guardProbe is a guarded alternative and the rule that matches it.
type guardProbe struct {
	rule  string
	guard *core.First
}

// guardProbeInputs returns the inputs of a case at the positions the guards are checked at: every
// suffix of an input that starts at a character, at most guardProbePositions evenly spread per input,
// with the empty suffix.
func guardProbeInputs(inputs []string) []string {
	const guardProbePositions = 120
	var out []string
	for _, in := range inputs {
		var offs []int
		for i := range in {
			offs = append(offs, i)
		}
		step := 1
		if len(offs) > guardProbePositions {
			step = len(offs) / guardProbePositions
		}
		for i := 0; i < len(offs); i += step {
			out = append(out, in[offs[i]:])
		}
		out = append(out, "")
	}
	return out
}

// guardProbes returns the guarded alternatives of the cases (Core IR of one grammar, in several
// forms) with the source of the grammar extended by one rule for each distinct alternative, and the
// number of alternatives that do not compile alone.
func guardProbes(t *testing.T, src string, cases []*coreGenCase) (*Program, []guardProbe, int) {
	t.Helper()
	type alt struct {
		text  string
		guard *core.First
	}
	var alts []alt
	seen := map[string]bool{}
	for _, c := range cases {
		for _, cr := range c.m.Rules {
			core.Walk(cr.Body, func(n core.Node) {
				ch, ok := n.(*core.Choice)
				if !ok {
					return
				}
				for i, a := range ch.Alts {
					e, ok := c.altOrigin(ch, i)
					if !ok || a.Guard == nil {
						continue
					}
					text := grammar.FormatExpr(e)
					key := fmt.Sprintf("%s\x00%v", text, *a.Guard)
					if !seen[key] {
						seen[key] = true
						alts = append(alts, alt{text, a.Guard})
					}
				}
			})
		}
	}
	build := func(idx []int) (*Program, error) {
		var b strings.Builder
		b.WriteString(src + "\n")
		for _, i := range idx {
			fmt.Fprintf(&b, "rule guardprobe%d = (%s) .*\n", i, alts[i].text)
		}
		g, err := parseTest(b.String())
		if err != nil {
			return nil, err
		}
		return Compile(g, compile.Options{})
	}
	var idx []int
	for i := range alts {
		idx = append(idx, i)
	}
	prog, err := build(idx)
	skipped := 0
	if err != nil {
		// Some alternative does not compile alone: keep those that do.
		var ok []int
		for _, i := range idx {
			if _, err := build([]int{i}); err == nil {
				ok = append(ok, i)
			}
		}
		skipped = len(idx) - len(ok)
		idx = ok
		if prog, err = build(idx); err != nil {
			t.Fatalf("probes that compile alone do not compile together: %v", err)
		}
	}
	var probes []guardProbe
	for _, i := range idx {
		probes = append(probes, guardProbe{fmt.Sprintf("guardprobe%d", i), alts[i].guard})
	}
	return prog, probes, skipped
}

// guardViolations returns the positions at which the engine matches a probe whose guard, as narrowed
// by narrow (nil for none), rejects the character there, and the number of positions checked with the
// engine (those the guard rejects: where it accepts, it cannot be unsound).
func guardViolations(prog *Program, probes []guardProbe, inputs []string, narrow func(*core.First) (rune, bool)) (bad []string, checked int) {
	for _, in := range inputs {
		ch, size := utf8.DecodeRuneInString(in)
		if size == 0 {
			ch = -1
		}
		for _, p := range probes {
			accepts := ch >= 0 && guardAccepts(p.guard, ch)
			if accepts && narrow != nil {
				if r, ok := narrow(p.guard); ok && r == ch {
					accepts = false
				}
			}
			if accepts {
				continue
			}
			checked++
			if _, err := prog.Parse(p.rule, in); err == nil {
				bad = append(bad, fmt.Sprintf("%s matches at %q, which its guard rejects", p.rule, firstRunes(in, 12)))
			}
		}
	}
	return bad, checked
}

// firstRunes returns at most n characters of s.
func firstRunes(s string, n int) string {
	i := 0
	for j := range s {
		if i == n {
			return s[:j] + "..."
		}
		i++
	}
	return s
}

// TestCoreGuardsAreSound checks, for every guarded alternative of the Core IR of the typed rules and of
// the recognizers of the corpus and the checked-in grammars, that the engine matches it only where its
// guard accepts the first character, and that a mutant that narrows the guards fails the check.
func TestCoreGuardsAreSound(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("probes the corpus with the engine")
	}
	var guarded, skipped, checked, mutantBad, mutantGrammars int
	for _, gc := range append(genCorpus(t), grammarCases(t)...) {
		if len(gc.inputs) == 0 {
			continue
		}
		var cases []*coreGenCase
		if gen, ok := coreGenerate(gc.src, gen.GenOptions{Types: true, TypedOnly: true}); ok {
			if c, err := coreTypedCase(gc.name, "lean", gen, nil); err == nil {
				cases = append(cases, c)
			}
		}
		if gen, ok := coreGenerate(gc.src, gen.GenOptions{Recognize: true}); ok {
			if c, err := coreRecognizeCase(gc.name, "lean", gen); err == nil {
				cases = append(cases, c)
			}
		}
		if len(cases) == 0 {
			continue
		}
		prog, probes, skip := guardProbes(t, gc.src, cases)
		if len(probes) == 0 {
			continue
		}
		guarded += len(probes)
		skipped += skip
		inputs := guardProbeInputs(gc.inputs)
		bad, n := guardViolations(prog, probes, inputs, nil)
		checked += n
		for _, b := range bad {
			t.Errorf("%s: %s", gc.name, b)
		}
		if bad, _ := guardViolations(prog, probes, inputs, guardNarrowed); len(bad) > 0 {
			mutantBad += len(bad)
			mutantGrammars++
		}
	}
	t.Logf("%d guarded alternatives probed, %d not probed (they read the captures around them); %d positions checked with the engine; the narrowing mutant fails %d times in %d grammars", guarded, skipped, checked, mutantBad, mutantGrammars)
	if guarded < 200 || checked < 5000 {
		t.Errorf("only %d guarded alternatives and %d positions: the check exercises too little", guarded, checked)
	}
	if mutantGrammars < 5 || mutantBad < 20 {
		t.Errorf("the mutant that narrows the guards fails %d times in %d grammars: the check does not detect an unsound guard", mutantBad, mutantGrammars)
	}
}
