//go:build !pego_budget_controls_reference

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

// A literal is matched against the input without charging the budget, however long it is: its length
// is a constant of the grammar, and the input it reads is not charged.
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
			if p.MatchLiteral([]rune(literal), []byte(literal)) || p.Pos != 0 || c.Used != 0 {
				t.Fatalf("streaming=%v replacement=%v matched=%d spent=%d pos=%d, want a mismatch that charges nothing", streaming, suffix != "", 0, c.Used, p.Pos)
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
			want, err := prog.ParseWith("main", "b", opts)
			if err != nil {
				t.Fatalf("baseline %v/%v: %v", backend, recognize, err)
			}
			// The lookahead reads a literal of 4097 characters and charges nothing for it.
			opts.Limits = limits.Limits{Budget: 8}
			got, err := prog.ParseWith("main", "b", opts)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("%v/recognize=%v node=%v error=%v, want the parse of the lookahead under a budget of 8", backend, recognize, got, err)
			}
		}
	}
}

func TestBudgetLiteralReplacementSentinelBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		literal string
		found   bool
	}{
		{"first", string(utf8.RuneError) + strings.Repeat("a", 4096), true},
		{"last", "aaaa" + string(utf8.RuneError), true},
		{"absent", "aaaaa", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := core.NewBudgetControl(nil, limits.Limits{Budget: 1})
			p := &core.Parser{Input: core.Input{Control: c}}
			if got := p.LiteralHasReplacement([]rune(tc.literal)); got != tc.found || c.Used != 0 {
				t.Fatalf("found=%v spent=%d, want %v/0", got, c.Used, tc.found)
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
	if !p.LiteralHasReplacement([]rune(strings.Repeat("a", 4096) + string(utf8.RuneError))) || c.Used != 0 || ctx.polls != 0 {
		t.Fatalf("spent=%d polls=%d, want a scan of the literal that neither charges nor polls", c.Used, ctx.polls)
	}
}
