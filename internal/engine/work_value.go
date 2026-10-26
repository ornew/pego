package engine

import (
	"math/bits"
	"strings"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
)

// Value work consumes steps, not input-work units: strings and collections can
// come from actions without referring to the input. Keep explicit copies and
// comparisons bounded; allocation, buffer growth, and GC remain nonpreemptible.
const valueWorkChunk = 128

func (p *parser) valueWork(n int) {
	if workControlsEnabled && p.control != nil {
		p.control.charge(0, uint64(n))
	}
}

// growthWork charges the copy that growing a table from oldLen to newLen entries stands for. The
// charge is a function of the two lengths alone: it falls where the length crosses a power of two,
// as doubling would copy, and never depends on the spare capacity that an earlier parse left in a
// pooled parser, so a parse is charged the same wherever its storage came from. The physical copy
// follows the charge and is not charged again.
func (p *parser) growthWork(oldLen, newLen int) {
	if oldLen > 0 && bits.Len(uint(newLen)) > bits.Len(uint(oldLen)) {
		p.chargeValues(oldLen)
	}
}

// chargeValues charges n values in chunks, so that a limit or cancellation is seen between them.
func (p *parser) chargeValues(n int) {
	for n > 0 {
		c := min(valueWorkChunk, n)
		p.valueWork(c)
		n -= c
	}
}

func copyWork[T any](p *parser, dst, src []T) {
	if !workControlsEnabled || p.control == nil {
		copy(dst, src)
		return
	}
	// Engine callers use disjoint storage; overlapping moves need their own
	// direction-aware implementation rather than a chunked forward copy.
	for off, end := 0, min(len(dst), len(src)); off < end; {
		n := min(valueWorkChunk, end-off)
		p.valueWork(n)
		copy(dst[off:off+n], src[off:off+n])
		off += n
	}
}

func appendWork[T any](p *parser, dst, src []T) []T {
	if !workControlsEnabled || p.control == nil {
		return append(dst, src...)
	}
	for off := 0; off < len(src); {
		n := min(valueWorkChunk, len(src)-off)
		p.valueWork(n)
		dst = append(dst, src[off:off+n]...)
		off += n
	}
	return dst
}

func clearWork[T any](p *parser, values []T) {
	if !workControlsEnabled || p.control == nil {
		clear(values)
		return
	}
	for off := 0; off < len(values); {
		n := min(valueWorkChunk, len(values)-off)
		p.valueWork(n)
		clear(values[off : off+n])
		off += n
	}
}

// Reset during ordinary action evaluation is counted. Mandatory interruption
// cleanup continues to call the original reset or runs after controls are nil.
func resetWork[T any](p *parser, a *arena[T], n int) {
	if !workControlsEnabled || p.control == nil {
		a.reset(n)
		return
	}
	for i := n; i < a.n; i++ {
		p.stepWork()
		var zero T
		a.chunks[i/64][i%64] = zero
	}
	a.n = n
}

// compareText preserves bytewise Go string ordering, including malformed UTF-8.
func (p *parser) compareText(l, r string) int {
	if !workControlsEnabled || p.control == nil {
		return strings.Compare(l, r)
	}
	for off, end := 0, min(len(l), len(r)); off < end; {
		n := min(valueWorkChunk, end-off)
		p.valueWork(2 * n)
		if order := strings.Compare(l[off:off+n], r[off:off+n]); order != 0 {
			return order
		}
		off += n
	}
	if len(l) < len(r) {
		return -1
	}
	if len(l) > len(r) {
		return 1
	}
	return 0
}

func (p *parser) valueEqual(l, r any) bool {
	if workControlsEnabled && p.control != nil {
		if ls, ok := l.(string); ok {
			if rs, ok := r.(string); ok {
				return len(ls) == len(rs) && p.compareText(ls, rs) == 0
			}
		}
	}
	return equal(l, r)
}

// textEqual reports whether two strings are equal as valueEqual does.
func (p *parser) textEqual(l, r string) bool {
	if workControlsEnabled && p.control != nil {
		return len(l) == len(r) && p.compareText(l, r) == 0
	}
	return l == r
}

func (p *parser) valueBinary(op string, l, r any) (any, error) {
	if !workControlsEnabled || p.control == nil {
		return binaryOp(op, l, r)
	}
	if op == "==" || op == "!=" {
		return p.valueEqual(l, r) == (op == "=="), nil
	}
	ls, lok := l.(string)
	rs, rok := r.(string)
	if lok && rok {
		switch op {
		case "+":
			var out strings.Builder
			out.Grow(len(ls) + len(rs))
			for _, s := range [...]string{ls, rs} {
				for off := 0; off < len(s); {
					n := min(valueWorkChunk, len(s)-off)
					p.valueWork(n)
					out.WriteString(s[off : off+n])
					off += n
				}
			}
			return out.String(), nil
		case "<":
			return p.compareText(ls, rs) < 0, nil
		case "<=":
			return p.compareText(ls, rs) <= 0, nil
		case ">":
			return p.compareText(ls, rs) > 0, nil
		case ">=":
			return p.compareText(ls, rs) >= 0, nil
		}
	}
	return p.binaryOperation(op, l, r)
}

func (p *parser) valueTextLen(s string) int {
	if !workControlsEnabled || p.control == nil || p.unit == Bytes {
		return p.unit.textLen(s)
	}
	n := 0
	for off := 0; off < len(s); {
		p.valueWork(min(utf8.UTFMax, len(s)-off))
		_, size := utf8.DecodeRuneInString(s[off:])
		off += size
		n++
	}
	return n
}

func (p *parser) valueField(fs Fields, name string) (any, bool) {
	if !workControlsEnabled || p.control == nil {
		return fs.Get(name)
	}
	for _, f := range fs {
		p.stepWork()
		if p.valueEqual(f.Name, name) {
			return f.Value, true
		}
	}
	return nil, false
}

func (p *parser) setValueField(fs *Fields, name string, v any) {
	if !workControlsEnabled || p.control == nil {
		fs.set(name, v)
		return
	}
	for i := range *fs {
		p.stepWork()
		if p.valueEqual((*fs)[i].Name, name) {
			(*fs)[i].Value = v
			return
		}
	}
	*fs = append(*fs, NodeField{name, v})
}

func (p *parser) hasValueField(st *grammar.TypeDef, name string) bool {
	if !workControlsEnabled || p.control == nil {
		return fieldOf(st, name) != nil
	}
	for _, f := range defFields(st) {
		p.stepWork()
		if p.valueEqual(f.Name, name) {
			return true
		}
	}
	return false
}

func (p *parser) lookupValue(name string) (any, bool) {
	if !workControlsEnabled || p.control == nil {
		return p.env.lookup(name)
	}
	for e := p.env; e != nil; e = e.next {
		p.stepWork()
		if p.valueEqual(e.name, name) {
			return e.val, true
		}
	}
	return nil, false
}

// fusable is also called at runtime by closure actions, unlike the bytecode
// compiler's setup-time analysis. Charge that runtime grammar walk as steps.
func (p *parser) fusable(t *grammar.Call) bool {
	if !workControlsEnabled || p.control == nil {
		return fusable(t)
	}
	for _, a := range t.Args {
		p.stepWork()
		call, ok := a.(*grammar.Call)
		switch {
		case ok && call.Func == "list", ok && call.Func == "map" && len(call.Args) == 2:
		case ok && call.Func == "concat" && p.fusable(call):
		default:
			return false
		}
	}
	return true
}
