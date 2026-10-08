package main

import (
	"math/rand/v2"
	"strconv"
	"strings"
)

// A generator of CEL expressions for the differential test: random expressions of the grammar, with random
// whitespace and comments between the tokens, and mutations of them (a token dropped, repeated, replaced or
// inserted) that make most of the inputs near misses, which cel-go accepts or rejects for reasons of its own.

type gen struct {
	r *rand.Rand
}

func (g *gen) n(n int) int              { return g.r.IntN(n) }
func (g *gen) p(p float64) bool         { return g.r.Float64() < p }
func (g *gen) pick(xs ...string) string { return xs[g.n(len(xs))] }

var (
	plainNames = []string{"a", "b", "c", "x", "y", "foo", "bar", "_x", "A1", "size", "int", "type", "name", "Msg", "pkg", "has", "all", "exists", "map", "filter"}
	// Words that the lexer reads as keywords (never a name) and the reserved words (a name only after a dot or in a
	// message), which the parser must treat differently.
	keywords = []string{"true", "false", "null", "in"}
	reserved = []string{"as", "break", "const", "continue", "else", "for", "function", "if", "import", "let", "loop", "package", "namespace", "return", "var", "void", "while"}
	escNames = []string{"`a b`", "`a-b`", "`a.b`", "`a/b`", "`1`", "`_`", "`a  b`", "`x`", "``", "`a_b-c.d/e f`", "`é`", "`a:b`"}
)

// name returns a name: mostly an ordinary one, sometimes a keyword or a reserved word.
func (g *gen) name() string {
	switch {
	case g.p(0.04):
		return keywords[g.n(len(keywords))]
	case g.p(0.06):
		return reserved[g.n(len(reserved))]
	}
	return plainNames[g.n(len(plainNames))]
}

// field returns a name that follows a dot or starts a field initializer, which may be escaped.
func (g *gen) field() string {
	if g.p(0.08) {
		return escNames[g.n(len(escNames))]
	}
	return g.name()
}

func (g *gen) expr(d int) []string {
	if d <= 0 {
		return g.primary(0)
	}
	e := g.binary(d)
	if g.p(0.07) {
		e = append(e, "?")
		e = append(e, g.binary(d-1)...)
		e = append(e, ":")
		e = append(e, g.expr(d-1)...)
	}
	return e
}

var binops = []string{"||", "&&", "==", "!=", "<", "<=", ">", ">=", "in", "+", "-", "*", "/", "%"}

func (g *gen) binary(d int) []string {
	e := g.unary(d)
	for g.p(0.35) {
		e = append(e, binops[g.n(len(binops))])
		e = append(e, g.unary(d-1)...)
	}
	return e
}

func (g *gen) unary(d int) []string {
	var e []string
	switch {
	case g.p(0.08):
		for range 1 + g.n(3) {
			e = append(e, "!")
		}
	case g.p(0.08):
		for range 1 + g.n(3) {
			e = append(e, "-")
		}
	}
	return append(e, g.member(d)...)
}

func (g *gen) member(d int) []string {
	e := g.primary(d)
	for g.p(0.3) && d > 0 {
		switch g.n(6) {
		case 0:
			e = append(e, ".", g.field())
		case 1:
			e = append(e, ".", "?", g.field())
		case 2, 3:
			e = append(e, ".", g.name(), "(")
			e = append(e, g.args(d-1)...)
			e = append(e, ")")
		case 4:
			e = append(e, "[")
			e = append(e, g.expr(d-1)...)
			e = append(e, "]")
		case 5:
			e = append(e, "[", "?")
			e = append(e, g.expr(d-1)...)
			e = append(e, "]")
		}
	}
	return e
}

func (g *gen) args(d int) []string {
	var e []string
	for i, n := 0, g.n(4); i < n; i++ {
		if i > 0 {
			e = append(e, ",")
		}
		e = append(e, g.expr(d)...)
	}
	if len(e) > 0 && g.p(0.02) {
		e = append(e, ",") // not allowed after the arguments of a call
	}
	return e
}

func (g *gen) trailingComma(e []string) []string {
	if g.p(0.15) {
		e = append(e, ",")
	}
	return e
}

func (g *gen) primary(d int) []string {
	if d <= 0 {
		switch g.n(3) {
		case 0:
			return []string{g.literal()}
		case 1:
			return g.ident()
		default:
			return g.ident()
		}
	}
	switch g.n(14) {
	case 0, 1, 2, 3:
		return []string{g.literal()}
	case 4, 5, 6:
		return g.ident()
	case 7, 8:
		e := append(g.ident(), "(")
		e = append(e, g.args(d-1)...)
		return append(e, ")")
	case 9:
		e := []string{"("}
		e = append(e, g.expr(d-1)...)
		return append(e, ")")
	case 10:
		e := []string{"["}
		for i, n := 0, g.n(4); i < n; i++ {
			if i > 0 {
				e = append(e, ",")
			}
			if g.p(0.12) {
				e = append(e, "?")
			}
			e = append(e, g.expr(d-1)...)
		}
		e = g.trailingComma(e)
		return append(e, "]")
	case 11:
		e := []string{"{"}
		for i, n := 0, g.n(4); i < n; i++ {
			if i > 0 {
				e = append(e, ",")
			}
			if g.p(0.12) {
				e = append(e, "?")
			}
			e = append(e, g.expr(d-1)...)
			e = append(e, ":")
			e = append(e, g.expr(d-1)...)
		}
		e = g.trailingComma(e)
		return append(e, "}")
	default:
		var e []string
		if g.p(0.15) {
			e = append(e, ".")
		}
		e = append(e, g.name())
		for g.p(0.4) {
			e = append(e, ".", g.name())
		}
		e = append(e, "{")
		for i, n := 0, g.n(3); i < n; i++ {
			if i > 0 {
				e = append(e, ",")
			}
			if g.p(0.12) {
				e = append(e, "?")
			}
			e = append(e, g.field(), ":")
			e = append(e, g.expr(d-1)...)
		}
		e = g.trailingComma(e)
		return append(e, "}")
	}
}

func (g *gen) ident() []string {
	if g.p(0.1) {
		return []string{".", g.name()}
	}
	return []string{g.name()}
}

func (g *gen) literal() string {
	switch g.n(9) {
	case 0, 1, 2:
		return g.number()
	case 3, 4, 5:
		return g.str()
	case 6:
		return "b" + g.pick("", "", "", "r", "R") + g.strBody(true)
	case 7:
		return g.pick("true", "false", "null")
	default:
		return g.number()
	}
}

func (g *gen) digits(n int) string {
	var b strings.Builder
	for range n {
		b.WriteByte(byte('0' + g.n(10)))
	}
	return b.String()
}

func (g *gen) hexdigits(n int) string {
	var b strings.Builder
	for range n {
		b.WriteByte("0123456789abcdefABCDEF"[g.n(22)])
	}
	return b.String()
}

var numberPool = []string{
	"0", "1", "2", "10", "42", "100", "255", "9223372036854775807", "9223372036854775808", "9223372036854775809",
	"18446744073709551615u", "18446744073709551616u", "18446744073709551615", "0x7fffffffffffffff", "0x8000000000000000",
	"0xffffffffffffffffu", "0x10000000000000000", "0x10000000000000000u", "00000000000000000000000000001", "0", "0u", "0U", "0x0", "0X1",
	"1.5", "0.5", ".5", "5.", "1e3", "1E3", "1e+3", "1e-3", "1.5e10", ".5e-3", "1e", "1e+", "1.e3", "0.0", "1e308", "1e309", "1e-324", "1e-400",
	"1.7976931348623157e308", "1.7976931348623159e308", "4.9e-324", "2.2250738585072014e-308", "123456789012345678901234567890.0", "00.5", "0e0",
	"1_000", "0b11", "0o7", "0xg", "0x", "1u2", "1ul", "1.5u", "1d", "1f", "-0", "-0.0", "-1e3", "-0x1", "-0x8000000000000000", "-9223372036854775808",
	"-9223372036854775809", "-1u",
}

func (g *gen) number() string {
	var s string
	switch g.n(8) {
	case 0, 1, 2, 3:
		s = numberPool[g.n(len(numberPool))]
	case 4:
		s = g.digits(1 + g.n(21))
	case 5:
		s = g.digits(1+g.n(5)) + "." + g.digits(g.n(5))
		if g.p(0.4) {
			s += g.pick("e", "E") + g.pick("", "+", "-") + g.digits(1+g.n(3))
		}
	case 6:
		s = "0x" + g.hexdigits(1+g.n(17))
	default:
		s = g.digits(1 + g.n(3))
	}
	if g.p(0.15) && s[0] != '-' && !strings.HasSuffix(s, "u") && !strings.HasSuffix(s, "U") {
		switch g.n(3) {
		case 0:
			s += g.pick("u", "U")
		}
	}
	if g.p(0.08) && s[0] != '-' {
		s = "-" + s
	}
	return s
}

func (g *gen) str() string {
	return g.pick("", "", "", "", "r", "R") + g.strBody(false)
}

var pieces = []string{
	"a", "b", "abc", "hello", " ", "  ", "x y", "0", "é", "日本語", "😀", "ÿ", " ", "\u0000", "\u007f", "\u0080", "�", "\xff", "\xc3", "\xc3\x28",
	"\\n", "\\t", "\\r", "\\\\", "\\\"", "\\'", "\\?", "\\`", "\\a", "\\b", "\\f", "\\v",
	"\\x41", "\\xff", "\\xFF", "\\X7f", "\\x4", "\\xg1", "\\101", "\\377", "\\400", "\\777", "\\000", "\\18", "\\1", "\\08", "\\7",
	"\\u00e9", "\\u263A", "\\u263a", "\\ud800", "\\uDFFF", "\\ud83d\\ude03", "\\uD83D", "\\u12", "\\u12g4", "\\u0000", "\\uFFFF", "\\ufffe",
	"\\U0001F600", "\\U0010FFFF", "\\U00110000", "\\UD83DDE03", "\\U0000d800", "\\U000000e9", "\\U1234", "\\U0000dfff", "\\U0000e000", "\\Ufffffff",
	"\\s", "\\z", "\\0", "\\8", "\\9", "\\e", "\\ ", "\\/", "\\$", "\\{",
	"\n", "\r\n", "\r", "\t", "\f", "\v",
	"\"", "'", "\"\"", "''", "\"\"\"", "'''", "\\",
}

var quotes = []string{"\"", "'", "\"\"\"", "'''"}

// strBody returns a quoted string literal (without the r or b prefix) of random content.
func (g *gen) strBody(bytes bool) string {
	q := quotes[g.n(len(quotes))]
	var b strings.Builder
	b.WriteString(q)
	for i, n := 0, g.n(6); i < n; i++ {
		b.WriteString(pieces[g.n(len(pieces))])
	}
	if !g.p(0.03) { // sometimes unterminated
		b.WriteString(q)
	}
	return b.String()
}

// tokens that mutations insert or substitute.
var vocab = []string{
	"(", ")", "[", "]", "{", "}", ",", ":", "?", ".", "!", "-", "+", "*", "/", "%", "<", ">", "=", "==", "!=", "<=", ">=", "&&", "||", "&", "|",
	"in", "true", "false", "null", "a", "b", "1", "0", "1u", "1.5", "\"s\"", "'t'", "b'x'", "r'y'", "`q`", "//", "//c\n", "@", "#", "$", ";", "\\",
	"if", "var", "as", "x", "_", "?.", ".?", "[?", "{?", "?[",
	"é", " ", "\x00", "\xff", " ", "\\ufeff", "§", "😀", "\r", "\f", "\v", "'''", "\"\"\"", "\\u0041", "\\n",
}

// mutate applies one to three random edits to the tokens.
func (g *gen) mutate(toks []string) []string {
	toks = append([]string(nil), toks...)
	for range 1 + g.n(3) {
		if len(toks) == 0 {
			toks = append(toks, vocab[g.n(len(vocab))])
			continue
		}
		i := g.n(len(toks))
		switch g.n(7) {
		case 0: // drop
			toks = append(toks[:i], toks[i+1:]...)
		case 1: // repeat
			toks = append(toks[:i+1], toks[i:]...)
		case 2: // replace
			toks[i] = vocab[g.n(len(vocab))]
		case 3, 4: // insert
			toks = append(toks[:i+1], toks[i:]...)
			toks[i] = vocab[g.n(len(vocab))]
		case 5: // swap
			if i+1 < len(toks) {
				toks[i], toks[i+1] = toks[i+1], toks[i]
			}
		case 6: // truncate
			toks = toks[:i]
		}
	}
	return toks
}

// join writes the tokens with random whitespace and comments between them. Tokens that would run together into a
// different token are separated, except now and then.
func (g *gen) join(toks []string) string {
	var b strings.Builder
	for i, t := range toks {
		if i > 0 {
			sep := ""
			switch g.n(20) {
			case 0:
				sep = "  "
			case 1:
				sep = "\n"
			case 2:
				sep = " // c\n"
			case 3:
				sep = "\t"
			case 4, 5, 6, 7, 8, 9, 10, 11:
				sep = " "
			}
			if sep == "" || sep == "\t" && false {
				prev := b.String()
				if joinable(prev[len(prev)-1], t[0]) && !g.p(0.03) {
					sep = " "
				}
			}
			b.WriteString(sep)
		}
		b.WriteString(t)
	}
	if g.p(0.03) {
		b.WriteString(" // end")
	}
	return b.String()
}

func wordChar(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func joinable(a, b byte) bool {
	switch {
	case wordChar(a) && wordChar(b):
		return true
	case a == '.' && b >= '0' && b <= '9', b == '.' && a >= '0' && a <= '9':
		return true
	case (a == '<' || a == '>' || a == '=' || a == '!') && b == '=':
		return true
	case a == '/' && b == '/', a == '&' && b == '&', a == '|' && b == '|', a == '-' && b == '-':
		return true
	case (a == 'r' || a == 'R' || a == 'b' || a == 'B') && (b == '"' || b == '\''):
		return true
	}
	return false
}

// generate returns n distinct expressions.
func generate(seed uint64, n int) []string {
	g := &gen{r: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
	seen := map[string]bool{}
	var out []string
	for len(out) < n {
		depth := 1 + g.n(4)
		toks := g.expr(depth)
		if g.p(0.3) {
			toks = g.mutate(toks)
		}
		s := g.join(toks)
		if seen[s] || len(s) > 4000 {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// literals returns n distinct expressions that are one literal: numbers and strings of every form.
func generateLiterals(seed uint64, n int) []string {
	g := &gen{r: rand.New(rand.NewPCG(seed, seed^0xdeadbeefcafe))}
	seen := map[string]bool{}
	var out []string
	for len(out) < n {
		var s string
		switch g.n(5) {
		case 0, 1:
			s = g.number()
		case 2, 3:
			s = g.str()
		default:
			s = "b" + g.pick("", "", "r", "R", "") + g.strBody(true)
		}
		if g.p(0.2) {
			s = g.pick(" ", "\n", "// c\n", "") + s + g.pick(" ", "\n", "")
		}
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// wrappers put an expression s inside another construct; each is at most a level of nesting of one kind or another.
var wrappers = []func(s string) string{
	func(s string) string { return "(" + s + ")" },
	func(s string) string { return "(" + s + ")" },
	func(s string) string { return "[" + s + "]" },
	func(s string) string { return "[1, " + s + "]" },
	func(s string) string { return "{1: " + s + "}" },
	func(s string) string { return "{" + s + ": 1}" },
	func(s string) string { return "f(" + s + ")" },
	func(s string) string { return "f(1, " + s + ")" },
	func(s string) string { return "a[" + s + "]" },
	func(s string) string { return "(" + s + ")[0]" },
	func(s string) string { return "(" + s + ").b" },
	func(s string) string { return "(" + s + ").f()" },
	func(s string) string { return "(" + s + ").f(1)" },
	func(s string) string { return "1 + (" + s + ")" },
	func(s string) string { return "(" + s + ") + 1" },
	func(s string) string { return "1 * (" + s + ")" },
	func(s string) string { return "(" + s + ") * 1" },
	func(s string) string { return "1 < (" + s + ")" },
	func(s string) string { return "(" + s + ") == 1" },
	func(s string) string { return "(" + s + ") && b" },
	func(s string) string { return "a || (" + s + ")" },
	func(s string) string { return "!(" + s + ")" },
	func(s string) string { return "-(" + s + ")" },
	func(s string) string { return "(" + s + ") ? 1 : 2" },
	func(s string) string { return "1 ? (" + s + ") : 2" },
	func(s string) string { return "1 ? 2 : " + s },
	func(s string) string { return "M{f: " + s + "}" },
	func(s string) string { return "(" + s + ") in a" },
	func(s string) string { return "a in (" + s + ")" },
	func(s string) string { return "1 + 2 * (" + s + ")" },
	func(s string) string { return "1 < 2 + (" + s + ")" },
	func(s string) string { return "1 + " + s },
	func(s string) string { return s + " + 1" },
	func(s string) string { return "1 * " + s },
	func(s string) string { return s + ".b" },
	func(s string) string { return s + "[0]" },
	func(s string) string { return s + " < 1" },
	func(s string) string { return "1 < " + s },
}

// generateDeep returns n expressions that nest one construct in another many times, around the limit of 250.
func generateDeep(seed uint64, n int) []string {
	g := &gen{r: rand.New(rand.NewPCG(seed, seed^0x5555aaaa))}
	seen := map[string]bool{}
	var out []string
	for len(out) < n {
		s := "x"
		// A few wrappers of one kind make long runs, so that the limit of a rule is reached with few kinds.
		k := 60 + g.n(300)
		w := wrappers[g.n(len(wrappers))]
		for range k {
			if g.p(0.3) {
				w = wrappers[g.n(len(wrappers))]
			}
			s = w(s)
		}
		if seen[s] || len(s) > 6000 {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// generateMacros returns n expressions with calls that look like the standard macros, with arguments that the macros
// take and arguments that they do not.
func generateMacros(seed uint64, n int) []string {
	g := &gen{r: rand.New(rand.NewPCG(seed, seed^0x1234abcd))}
	names := []string{"has", "all", "exists", "exists_one", "existsOne", "map", "filter", "exist", "optMap", "optFlatMap", "cel.bind", "size"}
	args := []string{"x", "y", ".x", "__result__", "@result", "(x)", "((x))", "1", "'s'", "x.y", "x.?y", "x[0]", "x[?0]", "a.b.c", "(a.b)", "x == 1", "x.f()", "f(x)",
		"[x]", "{x: 1}", "x ? y : z", "-x", "true", "null", "x.y.z == 1", "in", "if", "has(x.y)", "x.all(y, y)", "`x`", "x.`y`", "_", "a", "all", "map"}
	recvs := []string{"a", "a.b", "[1, 2]", "{'k': 1}", "x.y()", "(a)", "a[0]", "f(a)", "a.?b", "'s'", ".a", "1", "null"}
	pick := func(xs []string) string { return xs[g.n(len(xs))] }
	seen := map[string]bool{}
	var out []string
	for len(out) < n {
		var calls []string
		for range 1 + g.n(2) {
			name := pick(names)
			var as []string
			for range g.n(5) {
				as = append(as, pick(args))
			}
			c := name + "(" + strings.Join(as, ", ") + ")"
			if g.p(0.8) {
				c = pick(recvs) + "." + c
			} else if g.p(0.2) {
				c = "." + c
			}
			if g.p(0.2) {
				c = pick(args) + " && " + c
			}
			calls = append(calls, c)
		}
		s := strings.Join(calls, " || ")
		if g.p(0.15) {
			s = "!(" + s + ")"
		}
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

var _ = strconv.Itoa
