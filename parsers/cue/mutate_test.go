package cue

import (
	"math/rand/v2"
	"strings"
)

// mutate returns a variation of src chosen by seed: most are near misses, valid or not, which tests the
// parser at the borders of the syntax. It is a copy of mutate in internal/refgen/mutate.go, which made
// the vendored results (testdata/mutants.txt.gz): TestMutants checks the hash of each input first.
func mutate(src []byte, seed uint64) []byte {
	r := rand.New(rand.NewPCG(seed, 0x6375652d70656776))
	const alphabet = "{}[](),:?!@\"'#\\ \n\t;.=&|*+-<>~_a1/`$%^"
	if r.IntN(2) == 0 {
		return mutateTokens(r, src)
	}
	n := len(src)
	if n == 0 {
		return []byte(string(alphabet[r.IntN(len(alphabet))]))
	}
	b := append([]byte(nil), src...)
	// An edit often touches a line that is well formed, so first pick a line.
	lines := strings.SplitAfter(string(b), "\n")
	switch op := r.IntN(10); op {
	case 0: // delete a byte
		i := r.IntN(n)
		return append(b[:i:i], b[i+1:]...)
	case 1: // insert a character
		i := r.IntN(n + 1)
		c := alphabet[r.IntN(len(alphabet))]
		return append(b[:i:i], append([]byte{c}, b[i:]...)...)
	case 2: // replace a character
		i := r.IntN(n)
		b[i] = alphabet[r.IntN(len(alphabet))]
		return b
	case 3: // delete a line
		i := r.IntN(len(lines))
		return []byte(strings.Join(append(lines[:i:i], lines[i+1:]...), ""))
	case 4: // duplicate a line
		i := r.IntN(len(lines))
		out := append(lines[:i+1:i+1], lines[i:]...)
		return []byte(strings.Join(out, ""))
	case 5: // swap two adjacent lines
		if len(lines) < 2 {
			return b
		}
		i := r.IntN(len(lines) - 1)
		lines[i], lines[i+1] = lines[i+1], lines[i]
		return []byte(strings.Join(lines, ""))
	case 6: // truncate
		return b[:r.IntN(n)]
	case 7: // delete a span of up to eight bytes
		i := r.IntN(n)
		j := min(n, i+1+r.IntN(8))
		return append(b[:i:i], b[j:]...)
	case 8: // insert a second character
		i := r.IntN(n + 1)
		c := alphabet[r.IntN(len(alphabet))]
		d := alphabet[r.IntN(len(alphabet))]
		return append(b[:i:i], append([]byte{c, d}, b[i:]...)...)
	default: // delete the line break at the end of a line
		i := r.IntN(len(lines))
		lines[i] = strings.TrimSuffix(lines[i], "\n")
		return []byte(strings.Join(lines, ""))
	}
}

// lexemes splits src into pieces that are good enough to edit at the borders of tokens: runs of blanks, line
// breaks, comments, simple strings, identifiers, numbers and single characters.
func lexemes(src string) []string {
	var out []string
	isID := func(c byte) bool {
		return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
	}
	for i := 0; i < len(src); {
		j := i + 1
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			for j < len(src) && (src[j] == ' ' || src[j] == '\t' || src[j] == '\r') {
				j++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for j < len(src) && src[j] != '\n' {
				j++
			}
		case c == '"' || c == '\'':
			for j < len(src) && src[j] != '\n' && src[j] != c {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			if j < len(src) && src[j] == c {
				j++
			}
			j = min(j, len(src))
		case isID(c) || c == '#':
			for j < len(src) && isID(src[j]) {
				j++
			}
		}
		out = append(out, src[i:j])
		i = j
	}
	return out
}

var vocabulary = []string{
	"for", "in", "if", "let", "try", "else", "otherwise", "fallback", "func", "true", "false", "null", "package",
	"import", "_", "_|_", "x", "#D", "_h", "a.b", "...", "..", ".", ",", ":", "::", "?", "!", "?:", "!:", "~", "~X", "X=",
	"(", ")", "[", "]", "{", "}", "@a(b)", "@b(", ")", "=", "==", "!=", "=~", "!~", "<", "<-", ">", "<=", ">=", "&", "&&",
	"|", "||", "+", "-", "*", "/", "%", "1", "0", "01", "1.5", "1.", ".5", "1e3", "1_0", "0x1F", "0b1", "0o7", "1Ki",
	"1K", `"s"`, `""`, `"\(x)"`, `'b'`, `#"r"#`, `##"q"##`, `#"\#(x)"#`, "\"\"\"\n s\n\"\"\"", "'''\n b\n'''",
	"#\"\"\"\n s\n\"\"\"#", "\n", "\n\n", "// c\n", "\t", " ", "\r\n", "é", "@experiment(try)\n",
	"@experiment(aliasv2)\n", "@experiment(explicitopen)\n",
}

// mutateTokens edits src at the borders of tokens: it deletes, duplicates, swaps, inserts or replaces one.
func mutateTokens(r *rand.Rand, src []byte) []byte {
	ls := lexemes(string(src))
	if len(ls) == 0 {
		return []byte(vocabulary[r.IntN(len(vocabulary))])
	}
	i := r.IntN(len(ls))
	v := vocabulary[r.IntN(len(vocabulary))]
	switch r.IntN(7) {
	case 0:
		ls = append(ls[:i:i], ls[i+1:]...)
	case 1:
		ls = append(ls[:i+1:i+1], ls[i:]...)
	case 2:
		if i+1 < len(ls) {
			ls[i], ls[i+1] = ls[i+1], ls[i]
		}
	case 3:
		ls = append(ls[:i:i], append([]string{v}, ls[i:]...)...)
	case 4:
		ls = append(ls[:i+1:i+1], append([]string{v}, ls[i+1:]...)...)
	case 5:
		ls[i] = v
	default:
		// A line break for blanks, or blanks for a line break.
		for k := 0; k < len(ls); k++ {
			j := (i + k) % len(ls)
			if strings.TrimLeft(ls[j], " \t") == "" && ls[j] != "" && ls[j] != "\n" {
				ls[j] = "\n"
				break
			}
			if ls[j] == "\n" {
				ls[j] = " "
				break
			}
		}
	}
	return []byte(strings.Join(ls, ""))
}
