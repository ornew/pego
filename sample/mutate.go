package sample

import (
	"fmt"
	"strconv"
)

// Invalid is a near-miss invalid input: an input that the parser accepts, changed by one mutation so
// that the parser rejects it. It exercises the error paths of the parser and of the code around it.
type Invalid struct {
	Input    string // the mutated input
	Base     string // the valid input it was made from
	Mutation string // the mutation, such as `delete "]" at 12` (positions are byte offsets in Base)
	Err      error  // the error the parser returned for Input
}

// Invalid returns a near-miss invalid input. It returns ErrNoInput if it finds no valid input to mutate,
// and ErrNoInvalid if no mutation of the valid inputs it found is rejected (a grammar such as .* accepts
// every input).
func (g *Generator) Invalid() (Invalid, error) {
	for range g.cfg.attempts {
		base, err := g.Next()
		if err != nil {
			return Invalid{}, err
		}
		for range 8 {
			in, desc := g.mutate(base)
			if in == base {
				continue
			}
			if _, err := g.p.Parse(in); err != nil {
				return Invalid{Input: in, Base: base, Mutation: desc, Err: err}, nil
			}
		}
	}
	return Invalid{}, ErrNoInvalid
}

// GenerateInvalid returns up to n near-miss invalid inputs with distinct Input. It returns fewer if it
// cannot find more, and the error of Invalid (ErrNoInput or ErrNoInvalid) if it finds none.
func (g *Generator) GenerateInvalid(n int) ([]Invalid, error) {
	var out []Invalid
	seen := map[string]bool{}
	var err error
	for dups := 0; len(out) < n && dups < g.cfg.attempts; {
		var inv Invalid
		if inv, err = g.Invalid(); err != nil {
			break
		}
		if seen[inv.Input] {
			dups++
			continue
		}
		seen[inv.Input] = true
		dups = 0
		out = append(out, inv)
	}
	if len(out) == 0 && n > 0 {
		return nil, err
	}
	return out, nil
}

// Characters inserted by mutations besides the grammar's own literals and characters.
var mutationChars = []string{" ", "\n", "\t", "(", ")", "[", "]", "{", "}", "\"", "'", ",", ";", ":", ".", "0", "a", "\\", "\x00", "é"}

// mutate applies one random mutation to s and describes it. Mutations cut s only at rune boundaries,
// so the result is valid UTF-8 if s is.
func (g *Generator) mutate(s string) (string, string) {
	rng := g.g.rng
	// Rune boundaries of s, including len(s).
	var bounds []int
	for i := range s {
		bounds = append(bounds, i)
	}
	bounds = append(bounds, len(s))
	n := len(bounds) - 1 // number of runes
	token := func() string {
		if len(g.in.alphabet) > 0 && rng.IntN(2) == 0 {
			return g.in.alphabet[rng.IntN(len(g.in.alphabet))]
		}
		return mutationChars[rng.IntN(len(mutationChars))]
	}
	// span returns a random range of 1 to limit runes.
	span := func(limit int) (int, int) {
		i := rng.IntN(n)
		j := min(n, i+1+rng.IntN(limit))
		return bounds[i], bounds[j]
	}
	if n == 0 {
		t := token()
		return t, fmt.Sprintf("insert %s at 0", strconv.Quote(t))
	}
	switch rng.IntN(7) {
	case 0:
		i, j := span(1)
		return s[:i] + s[j:], fmt.Sprintf("delete %s at %d", strconv.Quote(s[i:j]), i)
	case 1:
		i, j := span(8)
		return s[:i] + s[j:], fmt.Sprintf("delete %s at %d", strconv.Quote(s[i:j]), i)
	case 2:
		i := bounds[rng.IntN(len(bounds))]
		t := token()
		return s[:i] + t + s[i:], fmt.Sprintf("insert %s at %d", strconv.Quote(t), i)
	case 3:
		i, j := span(1)
		t := token()
		return s[:i] + t + s[j:], fmt.Sprintf("replace %s at %d with %s", strconv.Quote(s[i:j]), i, strconv.Quote(t))
	case 4:
		i, j := span(8)
		return s[:j] + s[i:j] + s[j:], fmt.Sprintf("duplicate %s at %d", strconv.Quote(s[i:j]), i)
	case 5:
		if n < 2 {
			break
		}
		k := rng.IntN(n - 1)
		i, m, j := bounds[k], bounds[k+1], bounds[k+2]
		if s[i:m] == s[m:j] {
			break
		}
		return s[:i] + s[m:j] + s[i:m] + s[j:], fmt.Sprintf("swap %s and %s at %d", strconv.Quote(s[i:m]), strconv.Quote(s[m:j]), i)
	}
	i := bounds[rng.IntN(n)]
	return s[:i], fmt.Sprintf("truncate at %d", i)
}
