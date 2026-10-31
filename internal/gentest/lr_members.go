package gentest

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
)

// RandomCycleGrammar returns a random grammar of two to five rules that call one another, often
// at the start of an alternative, with lookaheads, optional and repeated items and nested choices,
// and random inputs over its letters. Most such grammars have left-recursion cycles with members
// besides their leaders, read inside and outside lookaheads. The grammar is a function of seed and
// cut, which adds cuts.
func RandomCycleGrammar(seed int64, cut bool) Case {
	rng := rand.New(rand.NewSource(seed))
	names := []string{"r0", "r1", "r2", "r3", "r4"}
	nr := 2 + rng.Intn(4)
	lit := func() string { return strconv.Quote(string("abc"[rng.Intn(3)])) }
	call := func() string { return names[rng.Intn(nr)] }
	var item func(first bool, depth int) string
	var alt func(depth int) string
	item = func(first bool, depth int) string {
		k := rng.Intn(100)
		switch {
		case first && k < 45:
			return call()
		case k < 35:
			return lit()
		case k < 60:
			return call()
		case k < 66 && depth < 2:
			return "&" + item(false, depth+1)
		case k < 74 && depth < 2:
			return "!" + item(false, depth+1)
		case k < 78 && depth < 2:
			return item(false, depth+1) + "?"
		case k < 81 && depth < 2:
			return "(" + lit() + " / " + call() + ")*"
		case k < 84 && cut:
			return "#cut"
		case k < 92 && depth < 2:
			return "(" + alt(depth+1) + " / " + alt(depth+1) + ")"
		default:
			return lit()
		}
	}
	alt = func(depth int) string {
		parts := make([]string, 1+rng.Intn(3))
		for i := range parts {
			parts[i] = item(i == 0, depth)
		}
		return strings.Join(parts, " ")
	}
	var b strings.Builder
	end := ""
	if rng.Intn(2) == 0 {
		end = " $$"
	}
	fmt.Fprintf(&b, "@start\nrule main = r0%s\n", end)
	for i := 0; i < nr; i++ {
		alts := make([]string, 1+rng.Intn(3))
		for j := range alts {
			alts[j] = alt(0)
		}
		// Some alternative matches without calling a rule.
		alts = append(alts, lit())
		fmt.Fprintf(&b, "rule %s = %s\n", names[i], strings.Join(alts, " / "))
	}
	inputs := []string{""}
	for i := 0; i < 14; i++ {
		s := make([]byte, rng.Intn(9))
		for j := range s {
			s[j] = "abc"[rng.Intn(3)]
		}
		inputs = append(inputs, string(s))
	}
	name := fmt.Sprintf("random cycle grammar %d", seed)
	if cut {
		name += " with cuts"
	}
	return Case{Name: name, Src: b.String(), Inputs: inputs}
}

// LeftRecursionMemberCases are grammars whose cycles have members besides their leaders, where a
// memoized member result could go stale while a leader grows: a leader that grows again at a
// position where member results read its earlier result, a leader whose result read the seed of
// another leader, and an Operators Rule leader that grows at one position at two binding levels.
func LeftRecursionMemberCases() []Case {
	return []Case{
		// r1 grows at 1 inside the lookahead of its own body, where r0 at 1 reads its result; outside
		// the lookahead r1 grows there again, and r0 at 1 must read the new seed.
		{"member read by a leader that grows again", `@start
rule main = r0
rule r0 = r1 "a" r0 / "a"
rule r1 = r1 "c" / &(r0 "a") / "a"`, []string{"aaa", "aaab", "a", "aa", "aca", "acaa", "ab", ""}},
		// r1 grows at 2 inside the growth of r3 there, reading the failing seed of r3, so it fails;
		// r0 at 2 reads that result. Called outside the lookahead, r1 grows again and matches.
		{"member read of a leader that read another seed", `@start
rule main = r0
rule r0 = r1 / "b"
rule r1 = r4 / r2
rule r2 = "b" (r3 (r2 "a" / "b" "a" "c") / &r0) r2 / r1
rule r3 = r3 / !r0
rule r4 = r3`, []string{"bb", "b", "bab", "bbb", "ba", "bbac", ""}},
		// expr grows at one position at binding level 0 and, through level(expr, mul), at the level
		// of mul; x reads both seeds.
		{"member of an Operators Rule cycle with binding levels", `@start
rule main = expr $$
rule expr = #operators {
    operand x
    operand "n"
    level add { infix left "+" }
    level mul { infix left "*" }
}
rule x = level(expr, mul) "!" / expr "?"`, []string{"n", "n?", "n!", "n*n!", "n+n?", "n?!", "n*n?!", "n!?", "n+n*n!?", "n+n!*n?", "", "n*"}},
	}
}

// LeftRecursionMemberSeeds are the seeds of RandomCycleGrammar (without cuts) whose grammars gave
// a memoized member result that differs from the result without a memo when member results were
// reused while a leader grew at their position, or when a member result that read the result of
// a leader whose growth read another seed was memoized.
var LeftRecursionMemberSeeds = []int64{906, 1661, 2206, 2331, 2938, 7673, 10627, 13632, 21954}

// LeftRecursionMemberCutSeeds are such seeds of RandomCycleGrammar with cuts.
var LeftRecursionMemberCutSeeds = []int64{276}

// LeftRecursionMemberSeedCases returns the grammars of LeftRecursionMemberSeeds and
// LeftRecursionMemberCutSeeds.
func LeftRecursionMemberSeedCases() []Case {
	var cases []Case
	for _, s := range LeftRecursionMemberSeeds {
		cases = append(cases, RandomCycleGrammar(s, false))
	}
	for _, s := range LeftRecursionMemberCutSeeds {
		cases = append(cases, RandomCycleGrammar(s, true))
	}
	return cases
}
