package main

import (
	"math/rand/v2"
	"strings"
)

// families lists the generators of inputs, by name.
var families = []string{"strings", "numbers", "tokens"}

// generate returns an input of a family chosen by seed. The same function is in the module's tests
// (gen_test.go): the vendored results record the hash of each input, which the tests check before comparing.
func generate(family string, seed uint64) string {
	r := rand.New(rand.NewPCG(seed, 0x67656e2d63756521))
	switch family {
	case "strings":
		return genString(r)
	case "numbers":
		return genNumber(r)
	default:
		return genTokens(r)
	}
}

func pick(r *rand.Rand, xs []string) string { return xs[r.IntN(len(xs))] }

// genString makes a field with a string literal of a random form: with hashes, in quotes of either kind, on one
// line or several, with escapes and interpolations, and sometimes with a flaw.
func genString(r *rand.Rand) string {
	hashes := strings.Repeat("#", []int{0, 0, 0, 1, 1, 2, 3}[r.IntN(7)])
	q := pick(r, []string{`"`, `"`, `'`})
	multi := r.IntN(3) == 0
	pieces := []string{
		"a", "bc", " ", " ", "\t", `"`, `'`, `\`, `\n`, `\t`, `\"`, `\'`, `\\`, `\/`, `\a`, `\u00e9`, `\U0001F600`,
		`\U00110000`, `\x41`, `\101`, `\400`, `\(1)`, `\(x + "s")`, `\(y)`, `\( "a\(b)" )`, "#", `\#`, `\#n`, `\#(y)`,
		`\##(z)`, `\##n`, "\n", "\r\n", `"""`, `'''`, `""`, `''`, `\ `, "é", `\uD83D\uDE00`, `\uD83D`, `\u12`, "\\\n",
		`\(`, `)`, "\u0000", "\x80", `\(1\n)`, "\\(\n1\n)",
	}
	open := hashes + q
	closing := q + hashes
	if multi {
		open = hashes + strings.Repeat(q, 3) + pick(r, []string{"\n", "\n", "\r\n", " \n", ""})
		closing = pick(r, []string{"\n", "\n", "\n\t", "\n  "}) + strings.Repeat(q, 3) + hashes
	}
	var b strings.Builder
	b.WriteString(open)
	for range r.IntN(7) {
		p := pick(r, pieces)
		b.WriteString(p)
		if multi && r.IntN(3) == 0 {
			b.WriteString("\n" + pick(r, []string{"", "  ", "\t", "  ", "    "}))
		}
	}
	// A flaw: a closing delimiter with the wrong number of hashes, or none.
	switch r.IntN(8) {
	case 0:
		closing = strings.TrimSuffix(closing, "#")
	case 1:
		closing += "#"
	case 2:
		closing = ""
	}
	b.WriteString(closing)
	suffix := pick(r, []string{"", "", "", "\n", "\nz: 1", " @a(b)", " & 2", ", y: 1", "\n}", ")"})
	prefix := pick(r, []string{"x: ", "x: ", "x: ", "[", "x: f(", "x: {y: ", "", "x: a.", "\"a\\("})
	return prefix + b.String() + suffix
}

// genNumber makes a field with a number-like token.
func genNumber(r *rand.Rand) string {
	pieces := []string{
		"0", "1", "9", "00", "012", "1_0", "1__0", "_", "_1", "0x", "0X", "0xF", "0xf_f", "0b", "0b1", "0b2", "0o", "0o7",
		"0o8", "0B1", "0O7", ".", "..", "...", "1.", ".5", "e", "E", "e5", "e+", "e-5", "+", "-", "K", "M", "Ki", "Mi", "i",
		"Gi", "P", "T", "Ti", "k", "m", "x", "a", "z", "f", "1e3", "1.5K", "0K",
	}
	var b strings.Builder
	for range 1 + r.IntN(4) {
		b.WriteString(pick(r, pieces))
	}
	prefix := pick(r, []string{"x: ", "x: ", "x: [", "x: a + ", "x: f(", "x: a.", "x: 1 @a("})
	suffix := pick(r, []string{"", "", "", "\n", "]", ")", " y", ", z: 1", "\n}"})
	return prefix + b.String() + suffix
}

// genTokens makes a few lines of tokens in random order and with random white space between them.
func genTokens(r *rand.Rand) string {
	toks := []string{
		"a", "b", "#D", "_h", "_", "$x", "for", "in", "if", "let", "try", "else", "otherwise", "fallback", "func",
		"true", "false", "null", "package", "import", "_|_", ":", "::", "?", "!", "...", "..", ".", ",", ";", "~", "=", "==",
		"!=", "=~", "!~", "<", "<=", ">", ">=", "<-", "&", "&&", "|", "||", "+", "-", "*", "/", "%", "(", ")", "[", "]",
		"{", "}", "1", "2.5", `"s"`, `'b'`, `"\(a)"`, `#"r"#`, "@a(b)", "@a", "@x(", "x?", "a?:", "a!:", "x!", "?:",
		"{}", "[]", "()", "a: b", "[a]: b", "(a): b", "\"k\": v", "X=a", "a~X", "a~(K,V)", "for x in y {a: x}",
		"if c {a: 1}", "let x = 1", "try {a: 1}", "else {b: 2}", "otherwise {b: 2}", "@experiment(try)",
	}
	seps := []string{" ", " ", " ", " ", "\n", "\n", "\n\n", "", "\t", " // c\n", "\r\n", ",", ", ", ",\n"}
	var b strings.Builder
	if r.IntN(12) == 0 {
		b.WriteString(pick(r, []string{"@experiment(try)\n", "@experiment(aliasv2)\n", "@experiment(explicitopen)\n", "@experiment(try,aliasv2)\n"}))
	}
	for i := 0; i < 2+r.IntN(11); i++ {
		b.WriteString(pick(r, toks))
		b.WriteString(pick(r, seps))
	}
	return b.String()
}
