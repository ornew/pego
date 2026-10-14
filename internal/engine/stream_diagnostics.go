package engine

import (
	"fmt"
	"math"
	"slices"
)

// TruncatedSyntaxErrors retains only the final Errors of a recovered-error
// stream. Saturated Total and Dropped are lower bounds, never wrapped counts.
type TruncatedSyntaxErrors struct {
	Errors          SyntaxErrors
	Total, Dropped  uint64
	CountsSaturated bool
}

func (e *TruncatedSyntaxErrors) Error() string {
	return fmt.Sprintf("stream diagnostics truncated: %d total, %d dropped\n%s", e.Total, e.Dropped, e.Errors.Error())
}

// Unwrap exposes retained errors to errors.As without implying completeness.
func (e *TruncatedSyntaxErrors) Unwrap() error { return e.Errors }

type streamDiagnostics struct {
	limit          int
	callback       func(*SyntaxError) error
	errors         SyntaxErrors
	oldest         int
	total, dropped uint64
	saturated      bool
}

func copySyntaxError(e *SyntaxError) *SyntaxError {
	copy := *e
	copy.Expected = slices.Clone(e.Expected)
	copy.Messages = slices.Clone(e.Messages)
	return &copy
}

func (d *streamDiagnostics) increment(n *uint64) {
	if *n == math.MaxUint64 {
		d.saturated = true
		return
	}
	*n++
}

func (d *streamDiagnostics) add(e *SyntaxError) error {
	if d.callback != nil {
		if err := d.callback(copySyntaxError(e)); err != nil {
			return err
		}
	}
	d.increment(&d.total)
	if d.limit == 0 || len(d.errors) < d.limit {
		d.errors = append(d.errors, e)
	} else {
		d.errors[d.oldest] = e
		d.oldest = (d.oldest + 1) % d.limit
		d.increment(&d.dropped)
	}
	return nil
}

func (d *streamDiagnostics) result() error {
	if len(d.errors) == 0 {
		return nil
	}
	tail := d.errors
	if d.oldest != 0 {
		tail = make(SyntaxErrors, len(d.errors))
		n := copy(tail, d.errors[d.oldest:])
		copy(tail[n:], d.errors[:d.oldest])
	}
	if d.dropped != 0 {
		return &TruncatedSyntaxErrors{Errors: tail, Total: d.total, Dropped: d.dropped, CountsSaturated: d.saturated}
	}
	return tail
}
