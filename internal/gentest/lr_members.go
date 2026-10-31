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
	c := randomCycleGrammar(seed, cut, false)
	c.Name = fmt.Sprintf("random cycle grammar %d", seed)
	if cut {
		c.Name += " with cuts"
	}
	return c
}

// RandomRecoveringCycleGrammar returns a grammar of RandomCycleGrammar's kind whose items also
// recover from errors (#recover) and call an Operators Rule, r9, whose operands call r1, at the
// lowest binding level or at the level of its postfix operator. r9 is then often a member of a
// cycle whose leaders have results with recovered errors, which are reused in one kind of context
// only. The grammar is a function of seed.
func RandomRecoveringCycleGrammar(seed int64) Case {
	c := randomCycleGrammar(seed, false, true)
	c.Name = fmt.Sprintf("random recovering cycle grammar %d", seed)
	return c
}

// randomCycleGrammar returns the grammar of RandomCycleGrammar, or of RandomRecoveringCycleGrammar
// with recovering. Without recovering it draws the same random numbers as before recovering was
// added, so the seeds of LeftRecursionMemberSeeds keep their grammars.
func randomCycleGrammar(seed int64, cut, recovering bool) Case {
	rng := rand.New(rand.NewSource(seed))
	names := []string{"r0", "r1", "r2", "r3", "r4"}
	nr := 2 + rng.Intn(4)
	lit := func() string { return strconv.Quote(string("abc"[rng.Intn(3)])) }
	call := func() string {
		if recovering && rng.Intn(5) == 0 {
			if rng.Intn(2) == 0 {
				return "r9"
			}
			return "level(r9, l2)"
		}
		return names[rng.Intn(nr)]
	}
	var item func(first bool, depth int) string
	var alt func(depth int) string
	item = func(first bool, depth int) string {
		if recovering && depth < 2 && rng.Intn(9) == 0 {
			return "(" + alt(depth+1) + ") #recover(skip=[abc])"
		}
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
	if recovering {
		b.WriteString("rule r9 = #operators {\n    operand r1\n    operand \"b\"\n    level l1 { infix left \"a\" }\n    level l2 { postfix \"c\" }\n}\n")
	}
	inputs := []string{""}
	for i := 0; i < 14; i++ {
		s := make([]byte, rng.Intn(9))
		for j := range s {
			s[j] = "abc"[rng.Intn(3)]
		}
		inputs = append(inputs, string(s))
	}
	return Case{Src: b.String(), Inputs: inputs}
}

// LeftRecursionMemberCases are grammars whose cycles have members besides their leaders, where a
// memoized member result could go stale while a leader grows: a leader that grows again at a
// position where member results read its earlier result, a leader whose result read the seed of
// another leader, an Operators Rule leader that grows at one position at two binding levels, and a
// member that reads such a leader result by finishing the leader's growth.
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
		// On "bcab", the member r9 at 2 calls the leader r1 there, whose growth reads the unstable
		// result of r0 there and recovers an error, so r9 reads an unstable result by finishing the
		// call. r0 grows at 2 inside a lookahead, and then outside it, and r1 grows again inside
		// each growth: the last result of r1 at 2 differs from the one r9 read, and replaces it.
		{"member read of a leader result with recovered errors", `@start
rule main = r0 $$
rule r0 = r0 r0 / r1 (r0 / "b" #recover(skip=[abc])) / "a"
rule r1 = (("b" / level(r9, l2))* !r1 / level(r9, l2) level(r9, l2) r0) / !r0 "b" / r0 (r0 / "a") / "c"
rule r9 = #operators {
    operand r1
    level l2 { postfix "c" }
}`, []string{"bcaa", "bcab", "bcac", "abcaa", "bbcab", "bcaab", "bcaca", "bca", "b", "a", ""}},
	}
}

// LeftRecursionMemberSeeds are the seeds of RandomCycleGrammar (without cuts) whose grammars gave
// a memoized member result that differs from the result without a memo when member results were
// reused while a leader grew at their position, or when a member result that read the result of
// a leader whose growth read another seed was memoized.
var LeftRecursionMemberSeeds = []int64{906, 1661, 2206, 2331, 2938, 7673, 10627, 13632, 21954}

// LeftRecursionMemberCutSeeds are such seeds of RandomCycleGrammar with cuts.
var LeftRecursionMemberCutSeeds = []int64{276}

// LeftRecursionMemberRecoveringSeeds are the seeds of RandomRecoveringCycleGrammar whose grammars
// gave a memoized member result that differs from the result without a memo when a member result
// that read a leader result with recovered errors was memoized.
var LeftRecursionMemberRecoveringSeeds = []int64{16472, 65734}

// LeftRecursionMemberSeedCases returns the grammars of LeftRecursionMemberSeeds,
// LeftRecursionMemberCutSeeds and LeftRecursionMemberRecoveringSeeds.
func LeftRecursionMemberSeedCases() []Case {
	var cases []Case
	for _, s := range LeftRecursionMemberSeeds {
		cases = append(cases, RandomCycleGrammar(s, false))
	}
	for _, s := range LeftRecursionMemberCutSeeds {
		cases = append(cases, RandomCycleGrammar(s, true))
	}
	for _, s := range LeftRecursionMemberRecoveringSeeds {
		cases = append(cases, RandomRecoveringCycleGrammar(s))
	}
	return cases
}

// LeftRecursionMemberDocumentSeeds are the seeds of RandomCycleGrammar (without cuts) whose
// parses store results that depend on the order in which the parse evaluated the calls: leaders
// grow inside the growth of another leader and read its seed. A later Document parse that reused
// such a result, without the growth it was computed in, gave another outcome than a parse from
// scratch.
var LeftRecursionMemberDocumentSeeds = []int64{117}

// LeftRecursionMemberDocumentRecoveringSeeds are such seeds of RandomRecoveringCycleGrammar, whose
// members also read leader results with recovered errors.
var LeftRecursionMemberDocumentRecoveringSeeds = []int64{5077}

// LeftRecursionMemberDocumentCases returns the grammars of LeftRecursionMemberDocumentSeeds and
// LeftRecursionMemberDocumentRecoveringSeeds.
func LeftRecursionMemberDocumentCases() []Case {
	var cases []Case
	for _, s := range LeftRecursionMemberDocumentSeeds {
		cases = append(cases, RandomCycleGrammar(s, false))
	}
	for _, s := range LeftRecursionMemberDocumentRecoveringSeeds {
		cases = append(cases, RandomRecoveringCycleGrammar(s))
	}
	return cases
}
