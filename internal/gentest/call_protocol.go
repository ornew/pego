package gentest

import (
	"math/rand"
	"strings"
)

// CallProtocolCases are grammars whose rules are called three times or more at the same position,
// so that a call finds the entry of an earlier one (each such rule calls another rule, so that it is
// memoized): in a lookahead and outside it, inside an
// attribute that replaces the expectations of its expression and outside it, with recovered errors,
// and in left-recursive growth. The inputs include random strings of the grammars' letters.
func CallProtocolCases() []Case {
	cases := []Case{
		// The third call of r reuses the entry that the second stored inside the attribute, which
		// renamed r's expectations; the reuse records them again.
		{Name: "reuse after a diagnostic attribute", Src: `rule main = e "x" / e "y" / r "z" $$
rule e = r @diagnostic(name="thing")
rule r = l "b" / "c"
rule l = "a"
`, Inputs: []string{"abz", "abx", "aby", "a", "az", "cz", "c", "", "abq"}},
		// The entries stored inside the lookaheads are silent: the call outside them evaluates r
		// again and records its expectations.
		{Name: "silent entries outside a lookahead", Src: `rule main = &(r "q") "a" / !(r "w") "c" / r "z" $$
rule r = l "b" / "c" "d"
rule l = "a"
`, Inputs: []string{"abz", "cdz", "abq", "a", "ab", "c", "cd", "cdw", "", "abw"}},
		// An entry with recovered errors is reused only in the kind of context it was computed in.
		{Name: "recovered errors in and outside a lookahead", Src: `rule main = &(r "q") s / &(r "w") s / r "z" $$
rule r = (l "b") #recover(skip=[^qwz]+) / "c"
rule l = "a"
rule s = "a" / "c" / "x"
`, Inputs: []string{"abz", "axz", "axq", "axw", "ax", "cz", "x", "", "az", "aqz"}},
		// The entry of r stored outside a lookahead holds a recovered error; the call in the
		// lookahead evaluates r again.
		{Name: "recovered errors reused in a lookahead", Src: `rule main = r "q" / r "w" / &(r "z") s $$
rule r = (l "b") #recover(skip=[^qwz]+) / "c"
rule l = "a"
rule s = "a" [a-z]* / "c" [a-z]*
`, Inputs: []string{"axz", "axq", "abz", "ax", "abq", "cz", "c", "", "aw"}},
		// A left-recursive leader grows its seed; later calls at the position reuse the grown
		// result, also from a lookahead.
		{Name: "reuse of a grown leader", Src: `rule main = &(e ";") e ";" / e "." / e $$
rule e = e "+" n / e "-" n / n
rule n = [0-9]+ / "(" e ")"
`, Inputs: []string{"1+2;", "1+2.", "1+2", "1+", "(1-2)+3;", "(1", "", "1+(2", "+"}},
		// Recovered errors inside a growing leader.
		{Name: "recovery inside growth", Src: `rule main = e ";" / e "." / e $$
rule e = (e "+" n) #recover(skip=[^+;.]+) / n
rule n = [0-9]+
`, Inputs: []string{"1+2;", "1+x;", "1+x+3.", "1+", "x", "", "1+2+x"}},
	}
	alphabets := []string{"abcxyzq", "abcdqwz", "abcxqwz", "abcxqwz", "0123+-();.", "0123+x;."}
	rng := rand.New(rand.NewSource(65))
	for i := range cases {
		letters := strings.Split(alphabets[i], "")
		for range 120 {
			var b strings.Builder
			for range rng.Intn(9) {
				b.WriteString(letters[rng.Intn(len(letters))])
			}
			cases[i].Inputs = append(cases[i].Inputs, b.String())
		}
	}
	return cases
}
