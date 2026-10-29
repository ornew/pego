package engine

import (
	"github.com/ornew/pego/grammar"

	"github.com/ornew/pego/internal/compile"
)

// classAccept returns the membership test of a character class. Classes of one or two ranges,
// which are most of them, compare against constants; others test ASCII characters with a bitmap.
func classAccept(e *grammar.CharClass) func(*parser, rune) bool {
	neg := e.Negated
	switch len(e.Ranges) {
	case 1:
		lo, hi := e.Ranges[0].Lo, e.Ranges[0].Hi
		return func(_ *parser, r rune) bool { return (lo <= r && r <= hi) != neg }
	case 2:
		lo1, hi1, lo2, hi2 := e.Ranges[0].Lo, e.Ranges[0].Hi, e.Ranges[1].Lo, e.Ranges[1].Hi
		return func(_ *parser, r rune) bool { return (lo1 <= r && r <= hi1 || lo2 <= r && r <= hi2) != neg }
	}
	ranges := append([]grammar.CharRange(nil), e.Ranges...)
	slow := func(r rune) bool {
		for _, rg := range ranges {
			if rg.Lo <= r && r <= rg.Hi {
				return !neg
			}
		}
		return neg
	}
	var ascii [2]uint64
	for r := rune(0); r < 128; r++ {
		if slow(r) {
			ascii[r>>6] |= 1 << (r & 63)
		}
	}
	return func(p *parser, r rune) bool {
		if uint32(r) < 128 {
			return ascii[r>>6]&(1<<(r&63)) != 0
		}
		if !workControlsEnabled || p.control == nil {
			return slow(r)
		}
		for _, rg := range ranges {
			p.stepWork()
			if rg.Lo <= r && r <= rg.Hi {
				return !neg
			}
		}
		return neg
	}
}

func classAcceptControlled(e *grammar.CharClass, control *compile.Control) func(*parser, rune) bool {
	if control == nil || len(e.Ranges) <= 2 {
		return classAccept(e)
	}
	ranges := compile.CopyClassRanges(e.Ranges, control)
	neg := e.Negated
	slow := func(r rune) bool {
		for _, rg := range ranges {
			if rg.Lo <= r && r <= rg.Hi {
				return !neg
			}
		}
		return neg
	}
	ascii := compile.ClassASCII(ranges, neg, control)
	return func(p *parser, r rune) bool {
		if uint32(r) < 128 {
			return ascii[r>>6]&(1<<(r&63)) != 0
		}
		if !workControlsEnabled || p.control == nil {
			return slow(r)
		}
		for _, rg := range ranges {
			p.stepWork()
			if rg.Lo <= r && r <= rg.Hi {
				return !neg
			}
		}
		return neg
	}
}
