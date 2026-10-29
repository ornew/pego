package core

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// equalExpected preserves the native snapshot-identity fast path while charging
// each comparison under active work controls. Snapshots are never modified.
func (p *Parser) equalExpected(a, b []ExpID) bool {
	if !WorkControlsEnabled || p.Control == nil {
		return slices.Equal(a, b)
	}
	if len(a) != len(b) {
		return false
	}
	for i, x := range a {
		p.StepWork()
		if x != b[i] {
			return false
		}
	}
	return true
}

// sortExpected sorts a newly constructed, parser-owned diagnostic. Comparisons
// and explicit moves are bounded; allocation and GC remain nonpreemptible.
// Published or memoized errors must never be passed to this helper.
func (p *Parser) sortExpected(values []string) {
	if !WorkControlsEnabled || p.Control == nil {
		sort.Strings(values)
		return
	}
	if len(values) < 2 {
		return
	}
	src, dst := values, make([]string, len(values))
	for width := 1; width < len(values); {
		for lo := 0; lo < len(values); {
			mid := lo + min(width, len(values)-lo)
			hi := mid + min(width, len(values)-mid)
			i, j, k := lo, mid, lo
			for i < mid && j < hi {
				order := p.CompareText(src[i], src[j])
				p.StepWork()
				if order <= 0 {
					dst[k] = src[i]
					i++
				} else {
					dst[k] = src[j]
					j++
				}
				k++
			}
			CopyWork(p, dst[k:k+mid-i], src[i:mid])
			k += mid - i
			CopyWork(p, dst[k:hi], src[j:hi])
			lo = hi
		}
		src, dst = dst, src
		// Stop before doubling can overflow; this pass merged the last run.
		if width >= len(values)-width {
			break
		}
		width *= 2
	}
	if &src[0] != &values[0] {
		CopyWork(p, values, src)
	}
}

// RecoveryErrorText is only for engine-owned Error-node materialization.
// Public post-return Error/Message calls remain caller work. The local builder
// owns its immutable output and is never pooled or written after return.
func (p *Parser) RecoveryErrorText(e *SyntaxError) string {
	if !WorkControlsEnabled || p.Control == nil {
		return e.Error()
	}
	prefix := fmt.Sprintf("%d:%d: ", e.Line, e.Col) // bounded integer formatting
	intro, sep := "", "; "
	parts := e.Messages
	if len(parts) == 0 {
		parts, sep = e.Expected, ", "
		intro = "syntax error"
		if len(parts) > 0 {
			intro += ": expected "
		}
	}
	size := checkedDiagnosticSize(len(prefix), len(intro))
	for i, part := range parts {
		p.StepWork()
		if i > 0 {
			size = checkedDiagnosticSize(size, len(sep))
		}
		size = checkedDiagnosticSize(size, len(part))
	}
	var out strings.Builder
	out.Grow(size)
	p.writeDiagnosticText(&out, prefix)
	p.writeDiagnosticText(&out, intro)
	for i, part := range parts {
		p.StepWork()
		if i > 0 {
			p.writeDiagnosticText(&out, sep)
		}
		p.writeDiagnosticText(&out, part)
	}
	return out.String()
}

func (p *Parser) writeDiagnosticText(out *strings.Builder, text string) {
	for off := 0; off < len(text); {
		n := min(valueWorkChunk, len(text)-off)
		p.ValueWork(n)
		out.WriteString(text[off : off+n])
		off += n
	}
}

func checkedDiagnosticSize(size, width int) int {
	if size < 0 || width < 0 || size > int(^uint(0)>>1)-width {
		panic(Fatal{Err: fmt.Errorf("diagnostic text exceeds maximum string size")})
	}
	return size + width
}
