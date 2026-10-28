//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/limits"
)

func TestWorkDiagnosticRecoveryTextMatchesNative(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	cases := []*SyntaxError{
		{Line: 1, Col: 1},
		{Line: 0, Col: 0, Messages: []string{""}, Expected: []string{"ignored"}},
		{Line: -1, Col: -2, Messages: []string{"", "two", "", "é\xff"}, Expected: []string{"ignored"}},
		{Line: maxInt, Col: -maxInt - 1, Expected: []string{"", "x", "", "é", "\xff"}},
		{Line: 99, Col: 101, Messages: []string{strings.Repeat("λ\xff", 180)}},
		{Line: 2, Col: 3, Expected: make([]string, 320)},
	}
	for i, e := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			want := e.Error()
			parts := e.Messages
			if len(parts) == 0 {
				parts = e.Expected
			}
			steps := uint64(len(want) + 2*len(parts))
			p := diagnosticParser(limits.WorkLimits{MaxSteps: steps})
			if got := p.recoveryErrorText(e); got != want || p.control.stepsUsed != steps {
				t.Fatalf("controlled text=%q, steps=%d; native=%q, steps=%d", got, p.control.stepsUsed, want, steps)
			}
			p = diagnosticParser(limits.WorkLimits{MaxSteps: steps - 1})
			if err := catchFatal(t, func() { _ = p.recoveryErrorText(e) }); !errors.Is(err, limits.ErrWorkLimit) {
				t.Fatalf("one-short recovery formatting: %v", err)
			}
		})
	}
}

func TestWorkDiagnosticRecoveryTextCancellationAndOwnership(t *testing.T) {
	for _, messages := range [][]string{make([]string, 320), {strings.Repeat("invalid\xff", 120)}} {
		e := &SyntaxError{Line: 7, Col: 11, Messages: messages, Expected: []string{"ignored"}}
		before := append([]string(nil), e.Messages...)
		p := diagnosticParser(limits.WorkLimits{MaxSteps: 1 << 50})
		retained := p.recoveryErrorText(e)
		p.control = newWorkControl(newPollingContext(1), limits.WorkLimits{})
		var got string
		if err := catchFatal(t, func() { got = p.recoveryErrorText(e) }); !errors.Is(err, context.Canceled) || got != "" {
			t.Fatalf("format cancellation: err=%v partial=%q", err, got)
		}
		p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 1 << 50})
		if retry := p.recoveryErrorText(&SyntaxError{Line: 1, Col: 2, Messages: []string{"other"}}); retry != "1:2: other" {
			t.Fatal("formatter retry differs")
		}
		if retained != e.Error() || !reflect.DeepEqual(e.Messages, before) || e.Expected[0] != "ignored" {
			t.Fatal("formatter retry mutated retained diagnostic or string")
		}
	}
}

func TestWorkDiagnosticSizeOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, pair := range [][2]int{{0, 0}, {2, 3}, {maxInt - 1, 1}, {maxInt, 0}} {
		if got := checkedDiagnosticSize(pair[0], pair[1]); got != pair[0]+pair[1] {
			t.Fatalf("valid diagnostic size=%d", got)
		}
	}
	for _, pair := range [][2]int{{-1, 0}, {0, -1}, {maxInt, 1}, {maxInt - 1, 2}} {
		err := catchFatal(t, func() { _ = checkedDiagnosticSize(pair[0], pair[1]) })
		if err == nil || !strings.Contains(err.Error(), "diagnostic text exceeds") {
			t.Fatalf("invalid diagnostic size %v: %v", pair, err)
		}
	}
}

func TestWorkDiagnosticSortParityAndMoves(t *testing.T) {
	for _, n := range []int{0, 1, 2, 3, 7, 8, 9, 127, 128, 129, 511} {
		values := make([]string, n)
		for i := range values {
			values[i] = []string{"", "é", "\xff", "z", "\xc3\xff", "alpha"}[(n-i)%6] + strings.Repeat("p", (n-i)%3)
		}
		want := append([]string(nil), values...)
		sort.Strings(want)
		p := diagnosticParser(limits.WorkLimits{MaxSteps: 1 << 50})
		p.sortExpected(values)
		if !slices.Equal(values, want) {
			t.Fatalf("sort length%d differs from native", n)
		}
	}
	// Empty-string comparisons consume no byte work, so these budgets/cancellation
	// isolate movement rather than failing in the comparator.
	p := diagnosticParser(limits.WorkLimits{MaxSteps: 128})
	if err := catchFatal(t, func() { p.sortExpected(make([]string, 320)) }); !errors.Is(err, limits.ErrWorkLimit) || p.control.stepsUsed != 128 {
		t.Fatalf("sort moves: err=%v steps=%d", err, p.control.stepsUsed)
	}
	p.control = newWorkControl(newPollingContext(1), limits.WorkLimits{})
	if err := catchFatal(t, func() { p.sortExpected(make([]string, 320)) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("sort move cancellation: %v", err)
	}
}
