package core

import (
	"math/bits"
	"strings"
	"unicode/utf8"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/value"
)

// Value spend is charged like any other spend, though strings and collections can
// come from actions without referring to the input. Keep explicit copies and
// comparisons bounded; allocation, buffer growth, and GC remain nonpreemptible.
const spendValueChunk = 128

func (p *Parser) SpendValue(n int) {
	if BudgetControlsEnabled && p.Control != nil {
		p.Control.Charge(uint64(n))
	}
}

// growthSpend charges the copy that growing a table from oldLen to newLen entries stands for. The
// charge is a function of the two lengths alone: it falls where the length crosses a power of two,
// as doubling would copy, and never depends on the spare capacity that an earlier parse left in a
// pooled parser, so a parse is charged the same wherever its storage came from. The physical copy
// follows the charge and is not charged again.
func (p *Parser) growthSpend(oldLen, newLen int) {
	if oldLen > 0 && bits.Len(uint(newLen)) > bits.Len(uint(oldLen)) {
		p.chargeValues(oldLen)
	}
}

// chargeValues charges n values in chunks, so that a limit or cancellation is seen between them.
func (p *Parser) chargeValues(n int) {
	for n > 0 {
		c := min(spendValueChunk, n)
		p.SpendValue(c)
		n -= c
	}
}

func CopySpend[T any](p *Parser, dst, src []T) {
	if !BudgetControlsEnabled || p.Control == nil {
		copy(dst, src)
		return
	}
	// Engine callers use disjoint storage; overlapping moves need their own
	// direction-aware implementation rather than a chunked forward copy.
	for off, end := 0, min(len(dst), len(src)); off < end; {
		n := min(spendValueChunk, end-off)
		p.SpendValue(n)
		copy(dst[off:off+n], src[off:off+n])
		off += n
	}
}

func AppendSpend[T any](p *Parser, dst, src []T) []T {
	if !BudgetControlsEnabled || p.Control == nil {
		return append(dst, src...)
	}
	for off := 0; off < len(src); {
		n := min(spendValueChunk, len(src)-off)
		p.SpendValue(n)
		dst = append(dst, src[off:off+n]...)
		off += n
	}
	return dst
}

func ClearSpend[T any](p *Parser, values []T) {
	if !BudgetControlsEnabled || p.Control == nil {
		clear(values)
		return
	}
	for off := 0; off < len(values); {
		n := min(spendValueChunk, len(values)-off)
		p.SpendValue(n)
		clear(values[off : off+n])
		off += n
	}
}

// Reset during ordinary action evaluation is counted. Mandatory interruption
// cleanup continues to call the original reset or runs after controls are nil.
func ResetSpend[T any](p *Parser, a *Arena[T], n int) {
	if !BudgetControlsEnabled || p.Control == nil {
		a.reset(n)
		return
	}
	for i := n; i < a.N; i++ {
		p.Spend()
		var zero T
		a.Chunks[i/64][i%64] = zero
	}
	a.N = n
}

// CompareText preserves bytewise Go string ordering, including malformed UTF-8.
func (p *Parser) CompareText(l, r string) int {
	if !BudgetControlsEnabled || p.Control == nil {
		return strings.Compare(l, r)
	}
	for off, end := 0, min(len(l), len(r)); off < end; {
		n := min(spendValueChunk, end-off)
		p.SpendValue(2 * n)
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

func (p *Parser) ValueEqual(l, r any) bool {
	if BudgetControlsEnabled && p.Control != nil {
		if ls, ok := l.(string); ok {
			if rs, ok := r.(string); ok {
				return len(ls) == len(rs) && p.CompareText(ls, rs) == 0
			}
		}
	}
	return equal(l, r)
}

func (p *Parser) ValueBinary(op string, l, r any) (any, error) {
	if !BudgetControlsEnabled || p.Control == nil {
		return binaryOp(op, l, r)
	}
	if op == "==" || op == "!=" {
		return p.ValueEqual(l, r) == (op == "=="), nil
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
					n := min(spendValueChunk, len(s)-off)
					p.SpendValue(n)
					out.WriteString(s[off : off+n])
					off += n
				}
			}
			return out.String(), nil
		case "<":
			return p.CompareText(ls, rs) < 0, nil
		case "<=":
			return p.CompareText(ls, rs) <= 0, nil
		case ">":
			return p.CompareText(ls, rs) > 0, nil
		case ">=":
			return p.CompareText(ls, rs) >= 0, nil
		}
	}
	return p.binaryOperation(op, l, r)
}

func (p *Parser) ValueTextLen(s string) int {
	if !BudgetControlsEnabled || p.Control == nil || p.Unit == Bytes {
		return TextLen(p.Unit, s)
	}
	n := 0
	for off := 0; off < len(s); {
		p.SpendValue(min(utf8.UTFMax, len(s)-off))
		_, size := utf8.DecodeRuneInString(s[off:])
		off += size
		n++
	}
	return n
}

func (p *Parser) ValueField(fs value.Fields, name string) (any, bool) {
	if !BudgetControlsEnabled || p.Control == nil {
		return fs.Get(name)
	}
	for _, f := range fs {
		p.Spend()
		if p.ValueEqual(f.Name, name) {
			return f.Value, true
		}
	}
	return nil, false
}

func (p *Parser) SetValueField(fs *value.Fields, name string, v any) {
	if !BudgetControlsEnabled || p.Control == nil {
		value.SetField(fs, name, v)
		return
	}
	for i := range *fs {
		p.Spend()
		if p.ValueEqual((*fs)[i].Name, name) {
			(*fs)[i].Value = v
			return
		}
	}
	*fs = append(*fs, value.NodeField{Name: name, Value: v})
}

func (p *Parser) HasValueField(st *grammar.TypeDef, name string) bool {
	if !BudgetControlsEnabled || p.Control == nil {
		return compile.FieldOf(st, name) != nil
	}
	for _, f := range compile.DefFields(st) {
		p.Spend()
		if p.ValueEqual(f.Name, name) {
			return true
		}
	}
	return false
}

// VarValue returns the value of the variable in slot i. Under budget controls it charges one
// unit for each node on the path to the slot (the levels of the environment, and the leaf).
func (p *Parser) VarValue(slot int) any {
	if BudgetControlsEnabled && p.Control != nil {
		for l := 0; l <= p.envLevels; l++ {
			p.Spend()
		}
	}
	return p.Env.Get(slot, p.envLevels)
}

// Fusable is also called at runtime by closure actions, unlike the bytecode
// compiler's setup-time analysis. Charge that runtime grammar walk as steps.
func (p *Parser) Fusable(t *grammar.Call) bool {
	if !BudgetControlsEnabled || p.Control == nil {
		return compile.Fusable(t)
	}
	for _, a := range t.Args {
		p.Spend()
		call, ok := a.(*grammar.Call)
		switch {
		case ok && call.Func == "list", ok && call.Func == "map" && len(call.Args) == 2:
		case ok && call.Func == "concat" && p.Fusable(call):
		default:
			return false
		}
	}
	return true
}
