package core

import "unicode/utf8"

// spendRead and Spend have a statically removable instrumentation boundary.
func (in *Input) spendRead(n uint64) {
	if BudgetControlsEnabled && in.Control != nil {
		in.Control.Charge(n)
	}
}

func (p *Parser) Spend() {
	if BudgetControlsEnabled && p.Control != nil {
		p.Control.Charge(1)
	}
}

// NewControlledTextInput preserves the ordinary preparation path when inactive.
// Controlled decoding reserves up to four bytes before each UTF-8 decoder call;
// preparation counts both the sizing pass and the storage pass. Allocations
// themselves remain nonpreemptible, as do runtime buffer growth and GC.
func NewControlledTextInput(s string, unit PositionUnit, offsets bool, sc *scratch, c *BudgetControl) Input {
	if !BudgetControlsEnabled || c == nil {
		return newTextInputIn(s, unit, offsets, sc)
	}
	if sc == nil {
		sc = &scratch{}
	}
	in := Input{Unit: unit, Eof: true, BaseLine: 1, BaseCol: 1, Control: c}
	if unit == Bytes {
		in.Bs = grow(sc.bytes, len(s))
		for off := 0; off < len(s); {
			n := min(4096, len(s)-off)
			c.Charge(uint64(n))
			copy(in.Bs[off:off+n], s[off:off+n])
			off += n
		}
		in.Src, in.SrcOK = s, true
		return in
	}
	// Each of the two passes charges one unit per byte of the source, in chunks of one poll
	// interval, before the bytes of a chunk are decoded: cancellation is observed as often as
	// when each byte is charged alone.
	n, valid := 0, true
	for off, charged := 0, 0; off < len(s); {
		if off >= charged {
			k := min(int(BudgetCheckInterval), len(s)-charged)
			c.Charge(uint64(k))
			charged += k
		}
		r, size := utf8.DecodeRuneInString(s[off:])
		if r == utf8.RuneError && size == 1 {
			valid = false
		}
		n++
		off += size
	}
	in.In = grow(sc.runes, n)
	if offsets {
		in.Offs = grow(sc.offs, n+1)
		in.Offs[n] = int32(len(s))
	}
	for off, i, charged := 0, 0, 0; off < len(s); i++ {
		if off >= charged {
			k := min(int(BudgetCheckInterval), len(s)-charged)
			c.Charge(uint64(k))
			charged += k
		}
		r, size := utf8.DecodeRuneInString(s[off:])
		in.In[i] = r
		if offsets {
			in.Offs[i] = int32(off)
		}
		off += size
	}
	in.Src, in.SrcOK = s, valid && len(s) <= 1<<31-1
	if !in.SrcOK {
		in.Offs = nil
	}
	return in
}
