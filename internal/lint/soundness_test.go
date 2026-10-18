package lint

import (
	"flag"
	"fmt"
	"math/rand/v2"
	"regexp"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/syntax"
)

var soundnessGrammars = flag.Int("soundness", 3000, "number of random grammars TestSoundness checks")

// TestSoundness checks the findings that claim certainty against the engine. It generates random
// grammars, lints them, and checks each such finding in two ways, by changing the grammar and
// comparing the inputs it accepts, and the values it builds where the change keeps the value
// type of every rule, on every input of up to inputLen characters from the alphabet of the
// grammars:
//
//   - The claim: a change that is equivalent exactly when the claim holds, and that keeps the
//     cuts and the left calls of the grammar. What can never match is followed by _|_ (x becomes
//     x _|_), a lookahead that never succeeds becomes !(x / _), one that always succeeds &(x / _) or
//     !(x _|_), and x? that is the same as x becomes x.
//   - The fix: the change that the finding suggests (removing a shadowed alternative), where it
//     is safe: unless the alternative has a cut, which can still commit the choice, or the fix
//     cautions that it makes a left call into left recursion.
//
// The fix is compared without the messages of syntax errors: a dead alternative still adds what
// it expected to them. Run with -soundness=40000 for a longer search.
func TestSoundness(t *testing.T) {
	const inputLen = 4
	inputs := allInputs("ab\n", inputLen)
	rng := rand.New(rand.NewPCG(1, 2))
	checked := map[string]int{}
	for n := range *soundnessGrammars {
		src := randomGrammar(rng)
		g, err := syntax.Parse(src)
		if err != nil {
			t.Fatalf("generated grammar does not parse: %v\n%s", err, src)
		}
		prog, err := engine.Compile(g, engine.Options{})
		if err != nil {
			t.Fatalf("generated grammar does not compile: %v\n%s", err, src)
		}
		var want []string
		for _, in := range inputs {
			want = append(want, result(prog, in))
		}
		for _, f := range Run(g, "main", Options{}) {
			for _, fix := range []bool{false, true} {
				g2, _ := syntax.Parse(src)
				if !applyFinding(g2, f, fix) {
					continue
				}
				if fix {
					checked[f.Check+" (fix)"]++
				} else {
					checked[f.Check]++
				}
				prog2, err := engine.Compile(g2, engine.Options{})
				if err != nil {
					t.Fatalf("grammar %d: the change for %v does not compile: %v\n%s", n, f, err, src)
				}
				// A change can change the value type of a rule: removing an alternative can leave
				// a choice without a value (nil next to alternatives with one, () alone), and x?
				// is a node of its own where a group x adds its elements to the sequence around
				// it. Such a change keeps what the grammar accepts, not what it builds.
				typed := sameRuleTypes(prog, prog2, g)
				if typed {
					checked["values"]++
				}
				for i, in := range inputs {
					got, want := result(prog2, in), want[i]
					if !typed {
						got, want = outcome(prog2, in), outcome(prog, in)
					}
					if fix {
						// A dead alternative still adds what it expected to syntax errors.
						got, want = errorMessage.ReplaceAllString(got, ""), errorMessage.ReplaceAllString(want, "")
					}
					if got != want {
						t.Fatalf("grammar %d: %v is wrong (fix: %v): on %q the grammar gives\n  %s\nbut with the change\n  %s\n%s\nchanged:\n%s",
							n, f, fix, in, want, got, src, grammar.Format(g2))
					}
				}
			}
		}
	}
	t.Logf("checked findings: %v", checked)
	for _, c := range []string{CheckShadowedAlt, CheckShadowedAlt + " (fix)", CheckUselessLookahead, CheckRedundantOptional, CheckNeverMatches, "values"} {
		if checked[c] < 20 {
			t.Errorf("only %d findings of %s were checked; the generator should produce more", checked[c], c)
		}
	}
}

// errorMessage matches the message of an error value in Node.String.
var errorMessage = regexp.MustCompile(`error\("(?:[^"\\]|\\.)*"\)`)

func allInputs(alphabet string, n int) []string {
	out := []string{""}
	level := []string{""}
	for range n {
		var next []string
		for _, s := range level {
			for _, r := range alphabet {
				next = append(next, s+string(r))
			}
		}
		out = append(out, next...)
		level = next
	}
	return out
}

func result(prog *engine.Program, in string) string {
	n, err := prog.ParseWith("main", in, engine.ParseOptions{})
	s := "<nil>"
	if n != nil {
		s = n.String()
	}
	if err != nil {
		s += fmt.Sprintf(" %T", err) // a failure, or recovered errors
	}
	return s
}

// outcome is the result of parsing in without the value: whether the parse fails or recovers.
func outcome(prog *engine.Program, in string) string {
	_, err := prog.ParseWith("main", in, engine.ParseOptions{})
	return fmt.Sprintf("%T", err)
}

// sameRuleTypes reports whether every rule of g has the same value type in a and b.
func sameRuleTypes(a, b *engine.Program, g *grammar.Grammar) bool {
	for _, r := range g.Rules() {
		ta, _ := a.ValueType(r.Name)
		tb, _ := b.ValueType(r.Name)
		if ta != tb {
			return false
		}
	}
	return true
}

// randomGrammar returns the source of a random grammar of three rules over the characters a, b
// and the line feed.
func randomGrammar(rng *rand.Rand) string {
	rules := []string{"main", "r1", "r2"}
	var expr func(depth int) string
	atom := func() string {
		atoms := []string{`"a"`, `"b"`, `"ab"`, `"ba"`, `"aa"`, `""`, `"\n"`, `[a-b]`, `[^a]`, `[a]`, `.`, `_`,
			`$$`, `^^`, `$`, `^`, `_|_`, "r1", "r2", "r1", "r2"}
		return atoms[rng.IntN(len(atoms))]
	}
	expr = func(depth int) string {
		if depth <= 0 || rng.IntN(4) == 0 {
			return atom()
		}
		switch rng.IntN(11) {
		case 0, 1:
			n := 2 + rng.IntN(2)
			parts := make([]string, n)
			for i := range parts {
				parts[i] = expr(depth - 1)
			}
			if rng.IntN(8) == 0 {
				parts[rng.IntN(n)] += " #cut"
			}
			return "(" + strings.Join(parts, " ") + ")"
		case 2, 3, 4:
			n := 2 + rng.IntN(2)
			parts := make([]string, n)
			for i := range parts {
				parts[i] = expr(depth - 1)
			}
			return "(" + strings.Join(parts, " / ") + ")"
		case 5:
			return "(" + expr(depth-1) + ")" + []string{"*", "+", "?", " #repeat(0,1)", " #repeat(2)"}[rng.IntN(5)]
		case 6:
			return "!" + "(" + expr(depth-1) + ")"
		case 7:
			return "&" + "(" + expr(depth-1) + ")"
		case 8:
			return "(" + expr(depth-1) + ")?"
		case 9:
			if rng.IntN(2) == 0 {
				return "(" + expr(depth-1) + ") @error(message=\"m\")"
			}
			return "(" + expr(depth-1) + ") #recover(skip=" + []string{"[a-b]", "\"\\n\"", "\"\""}[rng.IntN(3)] + ")"
		}
		return "(" + expr(depth-1) + ") #atomic"
	}
	var b strings.Builder
	for _, r := range rules {
		fmt.Fprintf(&b, "rule %s = %s\n", r, expr(3))
	}
	return b.String()
}

func fail(e grammar.Expr) grammar.Expr {
	return &grammar.Seq{Items: []grammar.Expr{e, &grammar.Bottom{}}}
}

// applyFinding changes g as described for TestSoundness, and reports whether it did.
func applyFinding(g *grammar.Grammar, f Finding, fix bool) bool {
	done := false
	noBase := f.Check == CheckNeverMatches && strings.Contains(f.Message, "no base case")
	for _, r := range g.Rules() {
		if noBase && r.Name == f.Rule && !fix {
			r.Expr = fail(r.Expr)
			done = true
			continue
		}
		r.Expr = rewrite(r.Expr, func(e grammar.Expr) grammar.Expr {
			switch e := e.(type) {
			case *grammar.Choice:
				if f.Check != CheckShadowedAlt {
					return e
				}
				for j := 1; j < len(e.Alts); j++ {
					if posOf(e.Alts[j]) != f.Pos {
						continue
					}
					never := strings.Contains(f.Message, "never tried")
					end := j + 1
					if never {
						end = len(e.Alts)
					}
					if !fix {
						for k := j; k < end; k++ {
							e.Alts[k] = fail(e.Alts[k])
						}
						done = true
						return e
					}
					if strings.Contains(f.Fix, "left-recursive") || !never && hasLooseCut(e.Alts[j]) {
						return e
					}
					// A choice of one alternative is kept: a choice has a value even when its
					// alternative (a lookahead, say) has none.
					e.Alts = append(e.Alts[:j:j], e.Alts[end:]...)
					done = true
					return e
				}
			case *grammar.Not:
				if f.Check == CheckUselessLookahead && e.Pos == f.Pos && !fix {
					done = true
					if strings.Contains(f.Message, "never succeed") {
						return &grammar.Not{Expr: &grammar.Choice{Alts: []grammar.Expr{e.Expr, &grammar.Top{}}}}
					}
					return &grammar.Not{Expr: fail(e.Expr)}
				}
			case *grammar.And:
				if f.Check == CheckUselessLookahead && e.Pos == f.Pos && !fix {
					done = true
					return &grammar.And{Expr: &grammar.Choice{Alts: []grammar.Expr{e.Expr, &grammar.Top{}}}}
				}
			case *grammar.Optional:
				if f.Check == CheckRedundantOptional && e.Pos == f.Pos && !fix {
					done = true
					return e.Expr
				}
			case *grammar.Seq:
				if f.Check != CheckNeverMatches || noBase || fix {
					return e
				}
				for _, it := range e.Items {
					switch it.(type) {
					case *grammar.BeginInput, *grammar.EndInput, *grammar.BeginLine, *grammar.EndLine:
						if posOf(it) == f.Pos {
							done = true
							e.Items = append(e.Items, &grammar.Bottom{})
							return e
						}
					}
				}
			}
			return e
		})
	}
	return done
}

// rewrite replaces the subexpressions of e bottom-up with f.
func rewrite(e grammar.Expr, f func(grammar.Expr) grammar.Expr) grammar.Expr {
	switch x := e.(type) {
	case *grammar.Group:
		x.Expr = rewrite(x.Expr, f)
	case *grammar.Seq:
		for i := range x.Items {
			x.Items[i] = rewrite(x.Items[i], f)
		}
	case *grammar.Choice:
		for i := range x.Alts {
			x.Alts[i] = rewrite(x.Alts[i], f)
		}
	case *grammar.Repeat:
		x.Expr = rewrite(x.Expr, f)
	case *grammar.Optional:
		x.Expr = rewrite(x.Expr, f)
	case *grammar.And:
		x.Expr = rewrite(x.Expr, f)
	case *grammar.Not:
		x.Expr = rewrite(x.Expr, f)
	case *grammar.Atomic:
		x.Expr = rewrite(x.Expr, f)
	case *grammar.Discard:
		x.Expr = rewrite(x.Expr, f)
	case *grammar.Capture:
		x.Expr = rewrite(x.Expr, f)
	case *grammar.Attributed:
		x.Expr = rewrite(x.Expr, f)
	}
	return f(e)
}
