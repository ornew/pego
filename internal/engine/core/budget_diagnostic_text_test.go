//go:build !zp_budget_controls_reference

package core

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/limits"
)

func TestBudgetDiagnosticRecoveryTextMatchesNative(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	cases := []*SyntaxError{
		{Line: 1, Col: 1},
		{Line: 0, Col: 0, Messages: []string{""}, Expected: []string{"ignored"}},
		{Line: -1, Col: -2, Messages: []string{"", "two", "", "é\xff"}, Expected: []string{"ignored"}},
		{Line: maxInt, Col: -maxInt - 1, Expected: []string{"", "x", "", "é", "\xff"}},
		{Line: 99, Col: 101, Messages: []string{strings.Repeat("λ\xff", 180)}},
		{Line: 2, Col: 3, Expected: make([]string, 320)},
		{Line: 4, Col: 5, Expected: []string{"end of input", `"a"`, `"b"`, `"c"`, `"d"`, `"e"`, `"f"`, `"g"`, "[0-9]", "[x-z]"}},
		{Line: 4, Col: 5, Expected: []string{"end of input", `"a"`, `"b"`, `"c"`, `"d"`, `"e"`, `"f"`, `"g"`, `"h"`, "[0-9]", "[x-z]"}},
	}
	for i, e := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			want := e.Error()
			parts := e.Messages
			if len(parts) == 0 {
				parts, _ = expectedShown(e.Expected)
			}
			steps := uint64(len(want) + 2*len(parts))
			p := diagnosticParser(limits.Limits{Budget: steps})
			if got := p.RecoveryErrorText(e); got != want || p.Control.Used != steps {
				t.Fatalf("controlled text=%q, steps=%d; native=%q, steps=%d", got, p.Control.Used, want, steps)
			}
			p = diagnosticParser(limits.Limits{Budget: steps - 1})
			if err := catchFatal(t, func() { _ = p.RecoveryErrorText(e) }); !errors.Is(err, limits.ErrLimit) {
				t.Fatalf("one-short recovery formatting: %v", err)
			}
		})
	}
}

func TestBudgetDiagnosticRecoveryTextCancellationAndOwnership(t *testing.T) {
	for _, messages := range [][]string{make([]string, 320), {strings.Repeat("invalid\xff", 120)}} {
		e := &SyntaxError{Line: 7, Col: 11, Messages: messages, Expected: []string{"ignored"}}
		before := append([]string(nil), e.Messages...)
		p := diagnosticParser(limits.Limits{Budget: 1 << 50})
		retained := p.RecoveryErrorText(e)
		p.Control = NewBudgetControl(newPollingContext(1), limits.Limits{})
		var got string
		if err := catchFatal(t, func() { got = p.RecoveryErrorText(e) }); !errors.Is(err, context.Canceled) || got != "" {
			t.Fatalf("format cancellation: err=%v partial=%q", err, got)
		}
		p.Control = NewBudgetControl(nil, limits.Limits{Budget: 1 << 50})
		if retry := p.RecoveryErrorText(&SyntaxError{Line: 1, Col: 2, Messages: []string{"other"}}); retry != "1:2: other" {
			t.Fatal("formatter retry differs")
		}
		if retained != e.Error() || !reflect.DeepEqual(e.Messages, before) || e.Expected[0] != "ignored" {
			t.Fatal("formatter retry mutated retained diagnostic or string")
		}
	}
}

func TestBudgetDiagnosticSizeOverflow(t *testing.T) {
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

func TestBudgetDiagnosticSortParityAndMoves(t *testing.T) {
	for _, n := range []int{0, 1, 2, 3, 7, 8, 9, 127, 128, 129, 511} {
		values := make([]string, n)
		for i := range values {
			values[i] = []string{"", "é", "\xff", "z", "\xc3\xff", "alpha", `"a"`, "[a]", `"\xff`, "[é"}[(n-i)%10] + strings.Repeat("p", (n-i)%3)
		}
		want := append([]string(nil), values...)
		sortExpectedEntries(want)
		p := diagnosticParser(limits.Limits{Budget: 1 << 50})
		p.sortExpected(values)
		if !slices.Equal(values, want) {
			t.Fatalf("sort length%d differs from native", n)
		}
	}
	// Empty-string comparisons consume no byte spend, so these budgets/cancellation
	// isolate movement rather than failing in the comparator.
	p := diagnosticParser(limits.Limits{Budget: 128})
	if err := catchFatal(t, func() { p.sortExpected(make([]string, 320)) }); !errors.Is(err, limits.ErrLimit) || p.Control.Used != 128 {
		t.Fatalf("sort moves: err=%v steps=%d", err, p.Control.Used)
	}
	p.Control = NewBudgetControl(newPollingContext(1), limits.Limits{})
	if err := catchFatal(t, func() { p.sortExpected(make([]string, 320)) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("sort move cancellation: %v", err)
	}
}
