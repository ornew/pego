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
)

func TestWorkLiteralReplacementMismatchBudget(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, suffix := range []string{"", string(utf8.RuneError)} {
			literal := strings.Repeat("a", 4096) + suffix
			c := newWorkControl(nil, WorkLimits{MaxSteps: 32})
			p := &parser{input: input{unit: Bytes, bs: []byte("b"), eof: !streaming, control: c}}
			if streaming {
				p.reader = bufio.NewReader(strings.NewReader(""))
			}
			err := catchFatal(t, func() { p.matchLiteral([]rune(literal), []byte(literal)) })
			var limit *WorkLimitError
			if !errors.As(err, &limit) || limit.Kind != StepWorkLimit || limit.Used != 32 || limit.Requested != 1 {
				t.Fatalf("streaming=%v replacement=%v error=%v, want one-step reservation at limit", streaming, suffix != "", err)
			}
			if c.inputUsed != 1 || p.pos != 0 {
				t.Fatalf("streaming=%v input=%d pos=%d, want only initial byte mismatch", streaming, c.inputUsed, p.pos)
			}
		}
	}
}

func TestWorkLiteralReplacementPublicLookahead(t *testing.T) {
	literal := strings.Repeat("a", 4096) + string(utf8.RuneError)
	prog := compile(t, "rule main = !"+strconv.Quote(literal)+" \"b\" $$")
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, recognize := range []bool{false, true} {
			opts := ParseOptions{Backend: backend, Unit: Bytes, Recognize: recognize}
			if _, err := prog.ParseWith("main", "b", opts); err != nil {
				t.Fatalf("baseline %v/%v: %v", backend, recognize, err)
			}
			opts.WorkLimits = WorkLimits{MaxSteps: 256}
			n, err := prog.ParseWith("main", "b", opts)
			if n != nil || !errors.Is(err, ErrWorkLimit) {
				t.Fatalf("%v/recognize=%v node=%v error=%v, want fatal lookahead interruption", backend, recognize, n, err)
			}
			opts.WorkLimits = WorkLimits{MaxSteps: 1 << 20}
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
			c := newWorkControl(nil, WorkLimits{MaxSteps: tc.visits})
			p := &parser{input: input{control: c}}
			got := p.literalHasReplacement([]rune(tc.literal))
			if got != tc.found || c.stepsUsed != tc.visits || c.inputUsed != 0 {
				t.Fatalf("found=%v steps/input=%d/%d, want %v/%d/0", got, c.stepsUsed, c.inputUsed, tc.found, tc.visits)
			}
			if tc.visits > 1 {
				c = newWorkControl(nil, WorkLimits{MaxSteps: tc.visits - 1})
				p.control = c
				err := catchFatal(t, func() { p.literalHasReplacement([]rune(tc.literal)) })
				var limit *WorkLimitError
				if !errors.As(err, &limit) || limit.Kind != StepWorkLimit || limit.Used != tc.visits-1 || limit.Requested != 1 || c.inputUsed != 0 {
					t.Fatalf("one-short error=%v input=%d", err, c.inputUsed)
				}
			}
			p.control = nil
			if got := p.literalHasReplacement([]rune(tc.literal)); got != tc.found {
				t.Fatalf("inactive found=%v", got)
			}
		})
	}
}

func TestWorkLiteralReplacementSentinelCancellation(t *testing.T) {
	ctx := newPollingContext(1)
	c := newWorkControl(ctx, WorkLimits{MaxSteps: 1 << 20})
	p := &parser{input: input{control: c}}
	err := catchFatal(t, func() { p.literalHasReplacement([]rune(strings.Repeat("a", 4096) + string(utf8.RuneError))) })
	if !errors.Is(err, context.Canceled) || c.stepsUsed != workCheckStepInterval-1 || c.inputUsed != 0 {
		t.Fatalf("error=%v steps/input=%d/%d, want cancellation before first 256-step poll reservation", err, c.stepsUsed, c.inputUsed)
	}
}
