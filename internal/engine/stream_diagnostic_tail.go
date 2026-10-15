package engine

import (
	"fmt"
	"math"
)

// Keep only small recovery scratch slices across stream elements. The
// diagnostic collector, not this scratch, owns committed errors.
const streamRecoveryScratchMax = 16

// TruncatedSyntaxErrors is the incomplete result of a stream parsed with a
// positive diagnostic limit. Errors is the retained tail in occurrence order.
// Saturated counts are lower bounds; truncation and the tail remain exact.
type TruncatedSyntaxErrors struct {
	Errors          SyntaxErrors
	Total           uint64
	Dropped         uint64
	CountsSaturated bool
}

func (e *TruncatedSyntaxErrors) Error() string {
	return fmt.Sprintf("recovered syntax errors truncated (total %d, dropped %d):\n%s", e.Total, e.Dropped, e.Errors)
}

// Unwrap exposes the retained errors to errors.As as SyntaxErrors.
func (e *TruncatedSyntaxErrors) Unwrap() error { return e.Errors }

func (o ParseOptions) validateStreamDiagnostics(stream bool) error {
	if o.DiagnosticLimit < 0 {
		return fmt.Errorf("diagnostic limit must be nonnegative")
	}
	if !stream && (o.DiagnosticCallback != nil || o.DiagnosticLimitSet || o.DiagnosticLimit != 0) {
		return fmt.Errorf("diagnostic options are only supported for stream parsing")
	}
	return nil
}

// streamDiagnosticTail is populated only at irreversible stream boundaries.
// The ring avoids shifting a large tail for each committed recovery.
type streamDiagnosticTail struct {
	limit           int
	errors          SyntaxErrors
	head            int
	total           uint64
	dropped         uint64
	truncated       bool
	countsSaturated bool
}

func (t *streamDiagnosticTail) add(e *SyntaxError) {
	if t.total == math.MaxUint64 {
		t.countsSaturated = true
	} else {
		t.total++
	}
	if len(t.errors) < t.limit {
		t.errors = append(t.errors, e)
		return
	}
	t.truncated = true
	if t.dropped == math.MaxUint64 {
		t.countsSaturated = true
	} else {
		t.dropped++
	}
	t.errors[t.head] = e
	t.head++
	if t.head == t.limit {
		t.head = 0
	}
}

func (t *streamDiagnosticTail) result(p *parser) error {
	p.control.check()
	if len(t.errors) == 0 {
		return nil
	}
	if t.head == 0 {
		if !t.truncated {
			return t.errors
		}
		return &TruncatedSyntaxErrors{Errors: t.errors, Total: t.total, Dropped: t.dropped, CountsSaturated: t.countsSaturated}
	}
	ordered := make(SyntaxErrors, 0, len(t.errors))
	ordered = t.appendLast(p, ordered, len(t.errors))
	p.control.check()
	return &TruncatedSyntaxErrors{Errors: ordered, Total: t.total, Dropped: t.dropped, CountsSaturated: t.countsSaturated}
}

// preview includes recoveries from the successful suffix, which traceExit
// observes before ParseStreamWith collects them for the terminal result.
func (t *streamDiagnosticTail) preview(p *parser, suffix []*SyntaxError) []*SyntaxError {
	n := min(t.limit, len(t.errors)+len(suffix))
	if n == 0 {
		return nil
	}
	p.control.check()
	out := make([]*SyntaxError, 0, n)
	if len(suffix) >= n {
		return appendWork(p, out, suffix[len(suffix)-n:])
	}
	out = t.appendLast(p, out, n-len(suffix))
	return appendWork(p, out, suffix)
}

// appendLast copies the last n retained errors in occurrence order. Each
// contiguous segment is charged and copied in bounded chunks by appendWork.
func (t *streamDiagnosticTail) appendLast(p *parser, dst SyntaxErrors, n int) SyntaxErrors {
	if n == 0 {
		return dst
	}
	length := len(t.errors)
	start := length - n
	index := t.head
	if start >= length-index {
		index = start - (length - index)
	} else {
		index += start
	}
	first := min(n, length-index)
	dst = appendWork(p, dst, t.errors[index:index+first])
	if first < n {
		dst = appendWork(p, dst, t.errors[:n-first])
	}
	return dst
}
