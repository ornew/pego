package core

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

// ValidateStreamDiagnostics checks the diagnostic options of o for a stream parse (stream) or
// another parse, which takes none of them.
func ValidateStreamDiagnostics(o ParseOptions, stream bool) error {
	if o.DiagnosticLimit < 0 {
		return fmt.Errorf("diagnostic limit must be nonnegative")
	}
	if !stream && (o.DiagnosticCallback != nil || o.DiagnosticLimitSet || o.DiagnosticLimit != 0) {
		return fmt.Errorf("diagnostic options are only supported for stream parsing")
	}
	return nil
}

// StreamDiagnosticTail is populated only at irreversible stream boundaries.
// The ring avoids shifting a large tail for each committed recovery.
type StreamDiagnosticTail struct {
	Limit           int
	Errors          SyntaxErrors
	head            int
	Total           uint64
	Dropped         uint64
	truncated       bool
	countsSaturated bool
}

func (t *StreamDiagnosticTail) Add(e *SyntaxError) {
	if t.Total == math.MaxUint64 {
		t.countsSaturated = true
	} else {
		t.Total++
	}
	if len(t.Errors) < t.Limit {
		t.Errors = append(t.Errors, e)
		return
	}
	t.truncated = true
	if t.Dropped == math.MaxUint64 {
		t.countsSaturated = true
	} else {
		t.Dropped++
	}
	t.Errors[t.head] = e
	t.head++
	if t.head == t.Limit {
		t.head = 0
	}
}

func (t *StreamDiagnosticTail) Result(p *Parser) error {
	p.Control.Check()
	if len(t.Errors) == 0 {
		return nil
	}
	if t.head == 0 {
		if !t.truncated {
			return t.Errors
		}
		return &TruncatedSyntaxErrors{Errors: t.Errors, Total: t.Total, Dropped: t.Dropped, CountsSaturated: t.countsSaturated}
	}
	ordered := make(SyntaxErrors, 0, len(t.Errors))
	ordered = t.appendLast(p, ordered, len(t.Errors))
	p.Control.Check()
	return &TruncatedSyntaxErrors{Errors: ordered, Total: t.Total, Dropped: t.Dropped, CountsSaturated: t.countsSaturated}
}

// Preview includes recoveries from the successful suffix, which TraceExit
// observes before ParseStreamWith collects them for the terminal result.
func (t *StreamDiagnosticTail) Preview(p *Parser, suffix []*SyntaxError) []*SyntaxError {
	n := min(t.Limit, len(t.Errors)+len(suffix))
	if n == 0 {
		return nil
	}
	p.Control.Check()
	out := make([]*SyntaxError, 0, n)
	if len(suffix) >= n {
		return AppendSpend(p, out, suffix[len(suffix)-n:])
	}
	out = t.appendLast(p, out, n-len(suffix))
	return AppendSpend(p, out, suffix)
}

// appendLast copies the last n retained errors in occurrence order. Each
// contiguous segment is charged and copied in bounded chunks by AppendSpend.
func (t *StreamDiagnosticTail) appendLast(p *Parser, dst SyntaxErrors, n int) SyntaxErrors {
	if n == 0 {
		return dst
	}
	length := len(t.Errors)
	start := length - n
	index := t.head
	if start >= length-index {
		index = start - (length - index)
	} else {
		index += start
	}
	first := min(n, length-index)
	dst = AppendSpend(p, dst, t.Errors[index:index+first])
	if first < n {
		dst = AppendSpend(p, dst, t.Errors[:n-first])
	}
	return dst
}
