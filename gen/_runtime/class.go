package runtime

// Class is a character class for generated parsers; not a stable API. ASCII is the bitset of the
// characters below 128 that the class matches, Wide the inclusive [lo, hi] pairs of the members at
// or above 128, and Neg inverts the membership of the characters at or above 128 (the bitset
// already accounts for it below).
type Class struct {
	ASCII [2]uint64
	Wide  []rune
	Neg   bool
}

// Has reports whether r belongs to the class.
func (c *Class) Has(r rune) bool {
	if uint32(r) < 128 {
		return c.ASCII[r>>6]>>(uint(r)&63)&1 != 0
	}
	return c.hasWide(r)
}

func (c *Class) hasWide(r rune) bool {
	for i := 0; i+1 < len(c.Wide); i += 2 {
		if r >= c.Wide[i] && r <= c.Wide[i+1] {
			return !c.Neg
		}
	}
	return c.Neg
}

// ClassFast consumes one ASCII character of c and reports whether it did. It changes nothing
// otherwise, and the caller then runs ClassSkip for the other characters and the expectation. It
// is small enough for the compiler to inline into the generated parser.
func (p *TParser) ClassFast(c *Class) bool {
	i := p.Pos
	r := uint32(128)
	if i < len(p.In) {
		r = uint32(p.In[i])
	} else if i < len(p.bs) {
		r = uint32(p.bs[i])
	}
	if r < 128 && c.ASCII[r>>6]>>(r&63)&1 != 0 {
		p.Pos = i + 1
		return true
	}
	return false
}

// ClassSkip consumes one character of c in an ordinary parse and reports whether there was one;
// otherwise it records the expectation id at the position.
func (p *TParser) ClassSkip(c *Class, id ExpID) bool {
	if p.ClassFast(c) {
		return true
	}
	return p.classSkip(c, id)
}

func (p *TParser) classSkip(c *Class, id ExpID) bool {
	if p.Stream == nil && p.unit == Bytes && p.Pos < p.n {
		if b := uint32(p.bs[p.Pos]); b < 128 {
			if c.ASCII[b>>6]>>(b&63)&1 != 0 {
				p.Pos++
				return true
			}
			p.ExpectOrdinary(p.Pos, id)
			return false
		}
	}
	ch, size, ok := p.NextOrdinary()
	if !ok || !c.Has(ch) {
		p.ExpectOrdinary(p.Pos, id)
		return false
	}
	p.Pos += size
	return true
}

// ClassStep is one character class of a sequence, with the expectation to record when it fails.
type ClassStep struct {
	C  *Class
	ID ExpID
}

// ClassesSkip consumes one character of each step's class in turn in an ordinary parse and reports
// whether all matched; at the first that does not it records that step's expectation.
func (p *TParser) ClassesSkip(steps []ClassStep) bool {
	for i := range steps {
		if !p.ClassFast(steps[i].C) && !p.classSkip(steps[i].C, steps[i].ID) {
			return false
		}
	}
	return true
}
