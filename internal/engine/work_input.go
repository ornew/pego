package engine

import "unicode/utf8"

// inputWork and stepWork have a statically removable instrumentation boundary.
func (in *input) inputWork(n uint64) {
	if workControlsEnabled && in.control != nil {
		in.control.charge(n, 1)
	}
}

func (p *parser) stepWork() {
	if workControlsEnabled && p.control != nil {
		p.control.charge(0, 1)
	}
}

// newControlledTextInput preserves the ordinary preparation path when inactive.
// Controlled decoding reserves up to four bytes before each UTF-8 decoder call;
// preparation counts both the sizing pass and the storage pass. Allocations
// themselves remain nonpreemptible, as do runtime buffer growth and GC.
func newControlledTextInput(s string, unit Unit, offsets bool, sc *scratch, c *workControl) input {
	if !workControlsEnabled || c == nil {
		return newTextInputIn(s, unit, offsets, sc)
	}
	if sc == nil {
		sc = &scratch{}
	}
	in := input{unit: unit, eof: true, baseLine: 1, baseCol: 1, control: c}
	if unit == Bytes {
		in.bs = grow(sc.bytes, len(s))
		for off := 0; off < len(s); {
			n := min(4096, len(s)-off)
			c.charge(uint64(n), 1)
			copy(in.bs[off:off+n], s[off:off+n])
			off += n
		}
		in.src, in.srcOK = s, true
		return in
	}
	n, valid := 0, true
	for off := 0; off < len(s); {
		c.charge(uint64(min(utf8.UTFMax, len(s)-off)), 1)
		r, size := utf8.DecodeRuneInString(s[off:])
		if r == utf8.RuneError && size == 1 {
			valid = false
		}
		n++
		off += size
	}
	in.in = grow(sc.runes, n)
	if offsets {
		in.offs = grow(sc.offs, n+1)
		in.offs[n] = int32(len(s))
	}
	for off, i := 0, 0; off < len(s); i++ {
		c.charge(uint64(min(utf8.UTFMax, len(s)-off)), 1)
		r, size := utf8.DecodeRuneInString(s[off:])
		in.in[i] = r
		if offsets {
			in.offs[i] = int32(off)
		}
		off += size
	}
	in.src, in.srcOK = s, valid && len(s) <= 1<<31-1
	if !in.srcOK {
		in.offs = nil
	}
	return in
}
