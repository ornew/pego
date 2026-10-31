package gentest

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/limits"
)

// Mutants returns n random edits of each input (a character inserted, deleted or replaced, or two
// characters swapped, up to three times), with characters from the inputs, reproducibly for a seed.
func Mutants(inputs []string, n int, seed uint64) []string {
	rng := rand.New(rand.NewPCG(seed, 1))
	var pool []rune
	for _, in := range inputs {
		pool = append(pool, []rune(in)...)
	}
	if len(pool) == 0 {
		return nil
	}
	var out []string
	for _, in := range inputs {
		for range n {
			rs := []rune(in)
			for range 1 + rng.IntN(3) {
				i := rng.IntN(len(rs) + 1)
				switch c := pool[rng.IntN(len(pool))]; rng.IntN(4) {
				case 0:
					rs = append(rs[:i], append([]rune{c}, rs[i:]...)...)
				case 1:
					if i < len(rs) {
						rs = append(rs[:i], rs[i+1:]...)
					}
				case 2:
					if i < len(rs) {
						rs[i] = c
					}
				default:
					if i+1 < len(rs) {
						rs[i], rs[i+1] = rs[i+1], rs[i]
					}
				}
			}
			out = append(out, string(rs))
		}
	}
	return out
}

// GeneratedTypesCases returns the grammars of TestGeneratedTypes: the corpus, a nesting limit and
// the typed grammar, with mutated inputs for the grammars written for direct rules.
func GeneratedTypesCases(t *testing.T) []Case {
	// Exactly at and just beyond the nesting limit, through direct rules (as in
	// TestGeneratedParsersMatchEngine).
	cases := append(GenCorpus(t), Case{Name: "nesting limit", Src: `type R = { T node[string] }
rule main R = t:n #atomic => R{T: $t}
rule n = "(" n ")" / "x"`, Inputs: []string{strings.Repeat("(", limits.DefaultMaxDepth-2) + "x" + strings.Repeat(")", limits.DefaultMaxDepth-2),
		strings.Repeat("(", limits.DefaultMaxDepth-1) + "x" + strings.Repeat(")", limits.DefaultMaxDepth-1)}})
	cases = append(cases, TypedLeaderValueCases...)
	cases = append(cases, Case{Name: "typed", Src: TypedGrammar, Inputs: []string{"f(1,x)!?;[3];zz", "f(1,(;g();", "f(a);"}})
	// The grammars written for direct rules also get mutated inputs, which reach other partial
	// matches and failures than the written ones.
	for i := range cases {
		if strings.Contains(cases[i].Name, "typed/direct_") {
			cases[i].Inputs = append(cases[i].Inputs, Mutants(cases[i].Inputs, 8, uint64(i))...)
		}
	}
	return cases
}

// TypedLeaderValueCases are left-recursion leaders without an action of their own, whose values
// are not nodes: the results of their alternatives' actions or of the rules they call. The typed
// runtime finishes a leader's value itself, so the value must survive that, wherever the leader
// is captured, and a node built over it must keep the positions the engine gives it. The typed
// parsers' values are compared with the engine's like those of every other corpus case.
var TypedLeaderValueCases = []Case{
	{Name: "typed leader reads its own value", Src: `rule main string = e:e $$ => $e
rule e string = (l:e "," o:o => $l + $o) / (o:o => $o)
rule o string = x:[a-z] => text($x)`, Inputs: []string{"a", "a,b", "a,b,c", "a,", ""}},
	{Name: "typed leader over a rule call", Src: `rule main string = "<" e:e ">" $$ => "<" + $e + ">"
rule e string = (l:e "+" o:o => $l + $o) / o
rule o string = x:[a-z] => text($x)`, Inputs: []string{"<a>", "<a+b>", "<a+b+c>", "<+>", "<a"}},
	{Name: "typed leader in a plain rule", Src: `rule main string = "[" s:p "]" $$ => $s
rule p string = (l:p "." x:[a-z] => $l + "." + text($x)) / (x:[a-z] => text($x))`, Inputs: []string{"[a]", "[a.b]", "[a.b.c]", "[a.]", "[]"}},
	{Name: "typed leader in an Operators Rule operand", Src: `rule main string = o:ops $$ => $o
rule ops string = #operators {
    operand "[" s:p "]" => "[" + $s + "]"
    operand x:[a-z] => text($x)
    level { infix left "+" => $lhs + "+" + $rhs }
}
rule p string = (l:p "." x:[a-z] => $l + "." + text($x)) / (x:[a-z] => text($x))`, Inputs: []string{"[a]", "[a.b]", "c+[a.b]", "[a.b]+c", "[a", "+"}},
	{Name: "typed leader in a negative lookahead predicate", Src: `rule main string = e:e $$ => $e
rule e string = (l:e !("q" {$l != ""}) c:[a-z] => $l + text($c)) / (d:[0-9] => text($d))`, Inputs: []string{"1a", "1ab", "1q", "1aq", "q", ""}},
	{Name: "typed leader spanned by an operator node", Src: `rule main = v:e $$ => ($v.(node[_]).Start, $v.(node[_]).End, text($v.(node[_])))
rule e = #operators {
    operand p
    level { infix left "+" }
}
rule p string = (l:p "." x:[a-z] => $l + text($x)) / (x:[a-z] => text($x))`, Inputs: []string{"a+b", "a.b+c", "c+a.b", "a.b.c+d.e+f"}},
	{Name: "typed leader in a tuple node", Src: `rule main = v:q $$ => ($v.Start, $v.End, $v.Value[0], text($v.Value[1]))
rule q = p "!"
rule p string = (l:p "." x:[a-z] => $l + text($x)) / (x:[a-z] => text($x))`, Inputs: []string{"a!", "a.b!", "a.b.c!", "!", "a."}},
}
