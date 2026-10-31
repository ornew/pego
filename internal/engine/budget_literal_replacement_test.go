//go:build !zp_budget_controls_reference

package engine

import (
	"bufio"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/zeroparse/internal/engine/core"
	"github.com/ornew/zeroparse/internal/limits"
)

func TestBudgetLiteralReplacementMismatchBudget(t *testing.T) {
	t.Parallel()
	for _, streaming := range []bool{false, true} {
		for _, suffix := range []string{"", string(utf8.RuneError)} {
			literal := strings.Repeat("a", 4096) + suffix
			c := core.NewBudgetControl(nil, limits.Limits{Budget: 32})
			p := &core.Parser{Input: core.Input{Unit: Bytes, Bs: []byte("b"), Eof: !streaming, Control: c}}
			if streaming {
				p.Reader = bufio.NewReader(strings.NewReader(""))
			}
			err := catchFatal(t, func() { p.MatchLiteral([]rune(literal), []byte(literal)) })
			var limit *limits.LimitError
			if !errors.As(err, &limit) || limit.Kind != limits.LimitBudget || limit.Used != 32 || limit.Requested != 1 {
				t.Fatalf("streaming=%v replacement=%v error=%v, want one-step reservation at limit", streaming, suffix != "", err)
			}
			if c.Used != 32 || p.Pos != 0 {
				t.Fatalf("streaming=%v spent=%d pos=%d, want spend only up to the limit and the initial byte mismatch", streaming, c.Used, p.Pos)
			}
			// With a limit that is not reached, the total is the scan of the literal for the replacement
			// character (one unit per rune of the literal) and the inspection of the first byte: peek and
			// mismatch, a few units, never a unit per byte compared.
			c = core.NewBudgetControl(nil, limits.Limits{Budget: 1 << 20})
			p = &core.Parser{Input: core.Input{Unit: Bytes, Bs: []byte("b"), Eof: !streaming, Control: c}}
			if streaming {
				p.Reader = bufio.NewReader(strings.NewReader(""))
			}
			scan := uint64(len([]rune(literal)))
			if p.MatchLiteral([]rune(literal), []byte(literal)) || p.Pos != 0 || c.Used < scan+1 || c.Used > scan+3 {
				t.Fatalf("streaming=%v replacement=%v spent=%d pos=%d, want a scan of %d and the first byte only", streaming, suffix != "", c.Used, p.Pos, scan)
			}
		}
	}
}

func TestBudgetLiteralReplacementPublicLookahead(t *testing.T) {
	t.Parallel()
	literal := strings.Repeat("a", 4096) + string(utf8.RuneError)
	prog := mustCompile(t, "rule main = !"+strconv.Quote(literal)+" \"b\" $$")
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, recognize := range []bool{false, true} {
			opts := ParseOptions{Backend: backend, Unit: Bytes, Recognize: recognize}
			if _, err := prog.ParseWith("main", "b", opts); err != nil {
				t.Fatalf("baseline %v/%v: %v", backend, recognize, err)
			}
			opts.Limits = limits.Limits{Budget: 256}
			n, err := prog.ParseWith("main", "b", opts)
			if n != nil || !errors.Is(err, limits.ErrLimit) {
				t.Fatalf("%v/recognize=%v node=%v error=%v, want fatal lookahead interruption", backend, recognize, n, err)
			}
			opts.Limits = limits.Limits{Budget: 1 << 20}
			if _, err := prog.ParseWith("main", "b", opts); err != nil {
				t.Fatalf("retry %v/%v: %v", backend, recognize, err)
			}
		}
	}
}

func TestBudgetLiteralReplacementSentinelBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		literal string
		visits  uint64
		found   bool
	}{
		{"first", string(utf8.RuneError) + strings.Repeat("a", 4096), 1, true},
		{"last", "aaaa" + string(utf8.RuneError), 5, true},
		{"absent", "aaaaa", 5, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := core.NewBudgetControl(nil, limits.Limits{Budget: tc.visits})
			p := &core.Parser{Input: core.Input{Control: c}}
			got := p.LiteralHasReplacement([]rune(tc.literal))
			if got != tc.found || c.Used != tc.visits {
				t.Fatalf("found=%v spent=%d, want %v/%d", got, c.Used, tc.found, tc.visits)
			}
			if tc.visits > 1 {
				c = core.NewBudgetControl(nil, limits.Limits{Budget: tc.visits - 1})
				p.Control = c
				err := catchFatal(t, func() { p.LiteralHasReplacement([]rune(tc.literal)) })
				var limit *limits.LimitError
				if !errors.As(err, &limit) || limit.Kind != limits.LimitBudget || limit.Used != tc.visits-1 || limit.Requested != 1 || c.Used != tc.visits-1 {
					t.Fatalf("one-short error=%v spent=%d", err, c.Used)
				}
			}
			p.Control = nil
			if got := p.LiteralHasReplacement([]rune(tc.literal)); got != tc.found {
				t.Fatalf("inactive found=%v", got)
			}
		})
	}
}

func TestBudgetLiteralReplacementSentinelCancellation(t *testing.T) {
	t.Parallel()
	ctx := newPollingContext(1)
	c := core.NewBudgetControl(ctx, limits.Limits{Budget: 1 << 20})
	p := &core.Parser{Input: core.Input{Control: c}}
	err := catchFatal(t, func() { p.LiteralHasReplacement([]rune(strings.Repeat("a", 4096) + string(utf8.RuneError))) })
	if !errors.Is(err, context.Canceled) || c.Used != core.BudgetCheckInterval-1 {
		t.Fatalf("error=%v spent=%d, want cancellation before first 256-unit poll reservation", err, c.Used)
	}
}
