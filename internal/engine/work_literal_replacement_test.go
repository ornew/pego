//go:build !pego_work_controls_reference

package engine

import (
	"bufio"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
)

func TestWorkLiteralReplacementMismatchBudget(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, suffix := range []string{"", string(utf8.RuneError)} {
			literal := strings.Repeat("a", 4096) + suffix
			c := core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 32})
			p := &core.Parser{Input: core.Input{Unit: Bytes, Bs: []byte("b"), Eof: !streaming, Control: c}}
			if streaming {
				p.Reader = bufio.NewReader(strings.NewReader(""))
			}
			err := catchFatal(t, func() { p.MatchLiteral([]rune(literal), []byte(literal)) })
			var limit *limits.WorkLimitError
			if !errors.As(err, &limit) || limit.Kind != limits.StepWorkLimit || limit.Used != 32 || limit.Requested != 1 {
				t.Fatalf("streaming=%v replacement=%v error=%v, want one-step reservation at limit", streaming, suffix != "", err)
			}
			if c.InputUsed != 1 || p.Pos != 0 {
				t.Fatalf("streaming=%v input=%d pos=%d, want only initial byte mismatch", streaming, c.InputUsed, p.Pos)
			}
		}
	}
}

func TestWorkLiteralReplacementPublicLookahead(t *testing.T) {
	literal := strings.Repeat("a", 4096) + string(utf8.RuneError)
	prog := mustCompile(t, "rule main = !"+strconv.Quote(literal)+" \"b\" $$")
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, recognize := range []bool{false, true} {
			opts := ParseOptions{Backend: backend, Unit: Bytes, Recognize: recognize}
			if _, err := prog.ParseWith("main", "b", opts); err != nil {
				t.Fatalf("baseline %v/%v: %v", backend, recognize, err)
			}
			opts.WorkLimits = limits.WorkLimits{MaxSteps: 256}
			n, err := prog.ParseWith("main", "b", opts)
			if n != nil || !errors.Is(err, limits.ErrWorkLimit) {
				t.Fatalf("%v/recognize=%v node=%v error=%v, want fatal lookahead interruption", backend, recognize, n, err)
			}
			opts.WorkLimits = limits.WorkLimits{MaxSteps: 1 << 20}
			if _, err := prog.ParseWith("main", "b", opts); err != nil {
				t.Fatalf("retry %v/%v: %v", backend, recognize, err)
			}
		}
	}
}

func TestWorkLiteralReplacementSentinelBoundaries(t *testing.T) {
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
			c := core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: tc.visits})
			p := &core.Parser{Input: core.Input{Control: c}}
			got := p.LiteralHasReplacement([]rune(tc.literal))
			if got != tc.found || c.StepsUsed != tc.visits || c.InputUsed != 0 {
				t.Fatalf("found=%v steps/input=%d/%d, want %v/%d/0", got, c.StepsUsed, c.InputUsed, tc.found, tc.visits)
			}
			if tc.visits > 1 {
				c = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: tc.visits - 1})
				p.Control = c
				err := catchFatal(t, func() { p.LiteralHasReplacement([]rune(tc.literal)) })
				var limit *limits.WorkLimitError
				if !errors.As(err, &limit) || limit.Kind != limits.StepWorkLimit || limit.Used != tc.visits-1 || limit.Requested != 1 || c.InputUsed != 0 {
					t.Fatalf("one-short error=%v input=%d", err, c.InputUsed)
				}
			}
			p.Control = nil
			if got := p.LiteralHasReplacement([]rune(tc.literal)); got != tc.found {
				t.Fatalf("inactive found=%v", got)
			}
		})
	}
}

func TestWorkLiteralReplacementSentinelCancellation(t *testing.T) {
	ctx := newPollingContext(1)
	c := core.NewWorkControl(ctx, limits.WorkLimits{MaxSteps: 1 << 20})
	p := &core.Parser{Input: core.Input{Control: c}}
	err := catchFatal(t, func() { p.LiteralHasReplacement([]rune(strings.Repeat("a", 4096) + string(utf8.RuneError))) })
	if !errors.Is(err, context.Canceled) || c.StepsUsed != core.WorkCheckStepInterval-1 || c.InputUsed != 0 {
		t.Fatalf("error=%v steps/input=%d/%d, want cancellation before first 256-step poll reservation", err, c.StepsUsed, c.InputUsed)
	}
}
