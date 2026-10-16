package editfuzz

import (
	"bytes"
	"errors"

	"github.com/ornew/pego"
)

// DefaultMaxAttempts is the number of candidate replays that Minimize tries when maxAttempts is zero.
const DefaultMaxAttempts = 2000

// Minimize shortens seq, which must fail with a *Mismatch on the grammar source src, while it fails with
// the same Fingerprint. It removes operations, then characters of the initial text, then bytes of
// replacements, and makes edits insertions, also at the start of the text, or deletions. Candidates that do not apply are discarded. It
// replays at most maxAttempts candidates (DefaultMaxAttempts if zero) and returns the shortest failing
// sequence found with its mismatch. seq is not modified.
func Minimize(src []byte, seq *Sequence, maxAttempts int) (*Sequence, *Mismatch, error) {
	p, err := compileFor(src, seq)
	if err != nil {
		return nil, nil, err
	}
	if maxAttempts <= 0 {
		maxAttempts = DefaultMaxAttempts
	}
	shortest, m, _, err := minimize(p, seq, maxAttempts)
	return shortest, m, err
}

type minimizer struct {
	p           *pego.Parser
	fingerprint string
	attempts    int
	max         int
	best        *Sequence
	mismatch    *Mismatch
}

func minimize(p *pego.Parser, seq *Sequence, maxAttempts int) (*Sequence, *Mismatch, int, error) {
	var m *Mismatch
	if err := replay(p, seq); !errors.As(err, &m) {
		if err == nil {
			return nil, nil, 0, errors.New("the sequence does not fail")
		}
		return nil, nil, 0, err
	}
	z := &minimizer{p: p, fingerprint: m.Fingerprint(), max: maxAttempts, best: clone(seq), mismatch: m}
	z.truncate()
	for progress := true; progress && z.attempts < z.max; {
		progress = z.removeOperations()
		progress = z.shrinkInitial() || progress
		progress = z.shrinkReplacements() || progress
		progress = z.simplifyRanges() || progress
	}
	return z.best, z.mismatch, z.attempts, nil
}

// try replays the candidate and keeps it if it fails the same way.
func (z *minimizer) try(c *Sequence) bool {
	if z.attempts >= z.max {
		return false
	}
	z.attempts++
	var m *Mismatch
	if err := replay(z.p, c); !errors.As(err, &m) || m.Fingerprint() != z.fingerprint {
		return false
	}
	z.best, z.mismatch = c, m
	z.truncate()
	return true
}

// truncate drops the operations after the failing one.
func (z *minimizer) truncate() {
	if z.mismatch.Event < len(z.best.Operations)-1 {
		z.best.Operations = z.best.Operations[:z.mismatch.Event+1]
	}
}

// removeOperations removes runs of operations, from half of them down to single ones.
func (z *minimizer) removeOperations() bool {
	progress := false
	for size := max(len(z.best.Operations)/2, 1); size >= 1; size /= 2 {
		for i := 0; i+size <= len(z.best.Operations); {
			c := clone(z.best)
			c.Operations = append(c.Operations[:i:i], c.Operations[i+size:]...)
			if z.try(c) {
				progress = true
			} else {
				i++
			}
		}
	}
	return progress
}

// shrinkInitial removes runs of characters from the initial text.
func (z *minimizer) shrinkInitial() bool {
	progress := false
	for size := max((len(boundaries(string(z.best.Initial)))-1)/2, 1); size >= 1; size /= 2 {
		for i := 0; ; {
			b := boundaries(string(z.best.Initial))
			if i+size >= len(b) {
				break
			}
			c := clone(z.best)
			c.Initial = append(append([]byte{}, c.Initial[:b[i]]...), c.Initial[b[i+size]:]...)
			if z.try(c) {
				progress = true
			} else {
				i++
			}
		}
	}
	return progress
}

// shrinkReplacements removes runs of bytes from the replacements of edits.
func (z *minimizer) shrinkReplacements() bool {
	progress := false
	// A kept candidate can fail earlier, and truncate then drops operations.
	for k := 0; k < len(z.best.Operations); k++ {
		for size := max(len(z.best.Operations[k].Text)/2, 1); size >= 1 && k < len(z.best.Operations); size /= 2 {
			for i := 0; k < len(z.best.Operations) && i+size <= len(z.best.Operations[k].Text); {
				c := clone(z.best)
				t := c.Operations[k].Text
				c.Operations[k].Text = append(append([]byte{}, t[:i]...), t[i+size:]...)
				if z.try(c) {
					progress = true
				} else {
					i++
				}
			}
		}
	}
	return progress
}

// simplifyRanges turns edits into insertions at their start, their end or the start of the text, or into
// deletions.
func (z *minimizer) simplifyRanges() bool {
	progress := false
	for k := 0; k < len(z.best.Operations); k++ {
		for _, change := range []func(*Operation){
			func(o *Operation) { o.End = o.Start },
			func(o *Operation) { o.Start = o.End },
			func(o *Operation) { o.Text = nil },
			// An insertion at the start lets more of the initial text go.
			func(o *Operation) { o.Start, o.End = 0, 0 },
		} {
			if k >= len(z.best.Operations) || z.best.Operations[k].Kind != Edit {
				break
			}
			c := clone(z.best)
			before := c.Operations[k]
			change(&c.Operations[k])
			if o := c.Operations[k]; o.Start == before.Start && o.End == before.End && len(o.Text) == len(before.Text) {
				continue
			}
			progress = z.try(c) || progress
		}
	}
	return progress
}

func clone(s *Sequence) *Sequence {
	c := *s
	c.Initial = bytes.Clone(s.Initial)
	c.Operations = make([]Operation, len(s.Operations))
	for i, o := range s.Operations {
		o.Text = bytes.Clone(o.Text)
		c.Operations[i] = o
	}
	return &c
}
