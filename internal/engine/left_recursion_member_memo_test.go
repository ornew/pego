package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/gentest"
)

// nestedLeftRecursionEvals parses the input on every backend and returns the number of rule
// evaluations of each, which must agree between the bytecode backends.
func nestedLeftRecursionEvals(t *testing.T, prog *Program, input string) map[Backend]int {
	t.Helper()
	got := map[Backend]int{}
	var want string
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		var pr Profile
		n, err := prog.ParseWith("", input, ParseOptions{Backend: b, Trace: pr.Trace})
		if err != nil {
			t.Fatalf("%s: %v", b, err)
		}
		if want == "" {
			want = n.String()
		} else if n.String() != want {
			t.Fatalf("%s: result differs from the first backend", b)
		}
		got[b] = pr.Evals
	}
	return got
}

// TestLeftRecursionMemberMemoLinear checks that the members of a left-recursion cycle that are
// not its leader are memoized: a member reached from two call sites at one position is evaluated
// again only while a seed it read is still growing, so the evaluations grow in proportion to the
// input where every call used to be evaluated again.
func TestLeftRecursionMemberMemoLinear(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		src   string
		input func(n int) string
	}{
		{"two members", `@start
rule main = A $$
rule A = C "+" / "n"
rule C = "(" C ")" "y" / "(" C ")" "z" / A`,
			func(n int) string { return strings.Repeat("(", n) + "n" + strings.Repeat(")z", n) + "+" }},
		// Three members, each reached from two call sites of the one before it.
		{"three members", `@start
rule main = A $$
rule A = C "+" / "n"
rule C = "(" C ")" "y" / "(" C ")" "z" / D
rule D = "[" D "]" "y" / "[" D "]" "z" / E
rule E = "{" E "}" "y" / "{" E "}" "z" / F
rule F = "<" F ">" "y" / "<" F ">" "z" / A`,
			func(n int) string {
				return strings.Repeat("(", n) + strings.Repeat("[", n) + strings.Repeat("{", n) + strings.Repeat("<", n) + "n" +
					strings.Repeat(">z", n) + strings.Repeat("}z", n) + strings.Repeat("]z", n) + strings.Repeat(")z", n) + "+"
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prog := mustCompile(t, c.src)
			sizes := []int{8, 16, 32, 64}
			evals := map[int]map[Backend]int{}
			for _, n := range sizes {
				evals[n] = nestedLeftRecursionEvals(t, prog, c.input(n))
			}
			for b, base := range evals[sizes[0]] {
				for _, n := range sizes[1:] {
					// A linear count at most doubles with the input; allow a margin for the constant.
					if got := evals[n][b]; got > base*(n/sizes[0])*2 {
						t.Errorf("%s: n=%d: %d evaluations, %d at n=%d: not linear", b, n, got, base, sizes[0])
					}
				}
			}
			if t.Failed() || testing.Verbose() {
				t.Log(fmt.Sprint(evals))
			}
		})
	}
}

// TestLeftRecursionMemberMemoMatchesNoMemo checks that memoizing the members of a cycle changes
// only the cost: on inputs that match, fail inside the nesting and fail after a seed, every
// backend gives the result, the errors and the expectations of the same grammar compiled without
// a memo, also when parsing inserts the tokens that are missing.
func TestLeftRecursionMemberMemoMatchesNoMemo(t *testing.T) {
	t.Parallel()
	grammars := []string{
		`@start
rule main = a $$
rule a = c "+" / "n"
rule c = "(" c ")" "y" / "(" c ")" "z" / a`,
		`@start
rule main = a $$
rule a = c "+" / "n"
rule c = "(" c ")" "y" / "(" c ")" "z" / d
rule d = "[" d "]" "y" / "[" d "]" "z" / e
rule e = "{" e "}" "y" / "{" e "}" "z" / a`,
		`@start
rule main = e $$
rule e = e "+" m / m
rule m = t "!" / t "?" / t
rule t = t "*" f / f
rule f = [0-9] / "(" e ")"`,
	}
	inputs := [][]string{
		{"n", "n+", "(n)z+", "(n)y+", "((n)z)y+", "((n)y)z+n", "(n)x", "((n)z+", "(n+", "()z", "", "((n)z)", "n)z+"},
		{"n", "n+", "(n)z+", "[n]y+", "({n}z)y+", "([{n}y]z)z+", "([{n}y]z)z+n", "[(n)z]y+", "({n)z+", "([n]z", "", "{n}"},
		{"1+2*3", "1!+2?", "(1+2)!*3", "1*2!+3*4?", "1+", "(1+2!", "1!?", "((1!)?)+2", "", "+", "1+*"},
	}
	for gi, src := range grammars {
		memo, plain := mustCompile(t, src), mustCompile(t, src, compile.Options{DisableMemo: true})
		for _, in := range inputs[gi] {
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				for _, unit := range []PositionUnit{CodePoints, Bytes} {
					for _, missing := range []int{0, 12} {
						for _, recognize := range []bool{false, true} {
							o := ParseOptions{Backend: backend, Unit: unit, MissingTokens: missing, Recognize: recognize}
							got, err := memo.ParseWith("", in, o)
							want, wantErr := plain.ParseWith("", in, o)
							if gentest.ResultJSON(got, err) != gentest.ResultJSON(want, wantErr) {
								t.Errorf("grammar %d %q %s/%s missing=%d recognize=%v: %v, %v vs %v, %v", gi, in, backend, unit, missing, recognize, got, err, want, wantErr)
							}
						}
					}
				}
			}
		}
	}
}

// memberMemoDiffers compares, on every backend, the results of the grammar compiled with and
// without a memo on the inputs, with the options that each backend runs (missing tokens inserted
// or not, recognition or a tree), and returns the differences, ignoring an input that exhausts a
// limit in either. full adds positions in bytes.
func memberMemoDiffers(t *testing.T, c gentest.Case, full bool) []string {
	t.Helper()
	memo, plain := mustCompile(t, c.Src), mustCompile(t, c.Src, compile.Options{DisableMemo: true})
	units := []PositionUnit{CodePoints}
	if full {
		units = append(units, Bytes)
	}
	var diffs []string
	for _, in := range c.Inputs {
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range units {
				for _, missing := range []int{0, 6} {
					for _, recognize := range []bool{false, true} {
						o := ParseOptions{Backend: backend, Unit: unit, MissingTokens: missing, Recognize: recognize}
						got, err := memo.ParseWith("", in, o)
						want, wantErr := plain.ParseWith("", in, o)
						a, b := gentest.ResultJSON(got, err), gentest.ResultJSON(want, wantErr)
						if a != b && !strings.Contains(a+b, "budget") && !strings.Contains(a+b, "limit") {
							diffs = append(diffs, fmt.Sprintf("%s %q %s/%s missing=%d recognize=%v:\n got %s\nwant %s", c.Name, in, backend, unit, missing, recognize, a, b))
						}
					}
				}
			}
		}
	}
	return diffs
}

// TestLeftRecursionMemberMemoStale checks that a memoized member result is not reused where it
// may differ from evaluating the member again: while a leader grows at its position (the leader
// may have grown there before, inside a lookahead, and the member result read that earlier
// result), and when it read the result of a leader whose growth read the seed of another leader
// (that leader may grow again with another result). The grammars include random ones where these
// cases arise.
func TestLeftRecursionMemberMemoStale(t *testing.T) {
	t.Parallel()
	for _, c := range append(gentest.LeftRecursionMemberCases(), gentest.LeftRecursionMemberSeedCases()...) {
		for _, d := range memberMemoDiffers(t, c, true) {
			t.Error(d)
		}
	}
}

// TestLeftRecursionMemberMemoRandom compares the results with and without a memo of random
// grammars whose cycles have members read inside and outside lookaheads, with and without cuts.
func TestLeftRecursionMemberMemoRandom(t *testing.T) {
	t.Parallel()
	seeds := 400
	if testing.Short() {
		seeds = 50
	}
	compiled, n := 0, 0
	for s := int64(0); s < int64(seeds); s++ {
		for _, cut := range []bool{false, true} {
			c := gentest.RandomCycleGrammar(s, cut)
			g, err := parseTest(c.Src)
			if err != nil {
				continue
			}
			if _, err := Compile(g, compile.Options{}); err != nil {
				continue
			}
			compiled++
			for _, d := range memberMemoDiffers(t, c, false) {
				if n++; n <= 5 {
					t.Errorf("%s\n%s", c.Src, d)
				}
			}
		}
	}
	if n > 0 || compiled < seeds {
		t.Errorf("%d differences in %d grammars", n, compiled)
	}
}

// TestLeftRecursionMemberMemoRandomRecovering compares the results with and without a memo of
// random grammars whose items recover from errors and call an Operators Rule that is a member of a
// cycle, so that members read leader results reused in one kind of context only.
func TestLeftRecursionMemberMemoRandomRecovering(t *testing.T) {
	t.Parallel()
	seeds := 3000
	if testing.Short() {
		seeds = 300
	}
	compiled, n := 0, 0
	for s := int64(0); s < int64(seeds); s++ {
		c := gentest.RandomRecoveringCycleGrammar(s)
		g, err := parseTest(c.Src)
		if err != nil {
			continue
		}
		if _, err := Compile(g, compile.Options{}); err != nil {
			continue
		}
		compiled++
		for _, d := range memberMemoDiffers(t, c, false) {
			if n++; n <= 5 {
				t.Errorf("%s\n%s", c.Src, d)
			}
		}
	}
	if n > 0 || compiled < seeds*9/10 {
		t.Errorf("%d differences in %d grammars", n, compiled)
	}
}
