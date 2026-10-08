package main

import (
	"math/rand/v2"
	"strings"
)

// mutate returns a variation of src chosen by seed: most are near misses, valid or not, which tests the
// parser at the borders of the syntax. The same function is in the module's tests (mutate_test.go): the
// vendored results record the hash of each mutated input, which the tests check before comparing.
func mutate(src []byte, seed uint64) []byte {
	r := rand.New(rand.NewPCG(seed, 0x6375652d70656776))
	const alphabet = "{}[](),:?!@\"'#\\ \n\t;.=&|*+-<>~_a1/`$%^"
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
