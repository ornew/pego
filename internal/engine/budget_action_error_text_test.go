//go:build !pego_budget_controls_reference

package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine/closure"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

func TestBudgetActionErrorTextReservationsAndOwnership(t *testing.T) {
	t.Parallel()
	for _, parts := range [][]string{{""}, {"", "", ""}, {"a", "", "λ\xff"}, {strings.Repeat("n", 8192)}} {
		want := strings.Join(parts, "")
		steps := uint64(len(want) + 2*len(parts))
		p := diagnosticParser(limits.Limits{Budget: steps})
		retained := p.ActionError(parts...)
		if retained.Error() != want || p.Control.Used != steps {
			t.Fatalf("builder parity/spend: %q %d", retained, p.Control.Used)
		}
		p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: steps - 1})
		var partial error
		if err := catchFatal(t, func() { partial = p.ActionError(parts...) }); !errors.Is(err, limits.ErrLimit) || partial != nil {
			t.Fatalf("one-short formatting: fatal=%v partial=%v", err, partial)
		}
		p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 100})
		if got := p.ActionError("retry").Error(); got != "retry" || retained.Error() != want {
			t.Fatal("retry changed owned text")
		}
	}
	// The second 128-byte write is rejected atomically.
	p := diagnosticParser(limits.Limits{Budget: 130})
	err := catchFatal(t, func() { _ = p.ActionError(strings.Repeat("x", 512)) })
	var limit *limits.LimitError
	if !errors.As(err, &limit) || limit.Kind != limits.LimitBudget || limit.Used != 130 || limit.Requested != 128 || p.Control.Used != 130 {
		t.Fatalf("chunk reservation: %v", err)
	}
	for _, parts := range [][]string{make([]string, 320), {strings.Repeat("x", 1024)}} {
		p.Control = core.NewBudgetControl(newPollingContext(1), limits.Limits{})
		if err := catchFatal(t, func() { _ = p.ActionError(parts...) }); !errors.Is(err, context.Canceled) {
			t.Fatalf("fragment/copy cancellation: %v", err)
		}
	}
}

func TestBudgetActionErrorFinalPrefixIsFlatAndBounded(t *testing.T) {
	t.Parallel()
	cause := core.ActionTextError("division by zero")
	where := strings.Repeat("rule", 512)
	want := "action in " + where + ": " + cause.Error()
	p := diagnosticParser(limits.Limits{Budget: uint64(len(want) + 8)})
	err := catchFatal(t, func() { p.FailAction(where, cause) })
	if err.Error() != want || errors.Unwrap(err) != nil || errors.Is(err, cause) || p.Control.Used != uint64(len(want)+8) {
		t.Fatalf("final error contract: %v", err)
	}
	p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: uint64(len(want) + 7)})
	if err := catchFatal(t, func() { p.FailAction(where, cause) }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("one-short wrapper: %v", err)
	}
	p.Control = core.NewBudgetControl(newPollingContext(1), limits.Limits{})
	if err := catchFatal(t, func() { p.FailAction(where, cause) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("wrapper cancellation: %v", err)
	}
}

type actionFormattingError struct {
	cancel context.CancelFunc
	panics bool
}

func (e actionFormattingError) Error() string { return "Error method" }
func (e actionFormattingError) Format(s fmt.State, verb rune) {
	if e.cancel != nil {
		e.cancel()
	}
	if e.panics {
		panic("formatter panic")
	}
	_, _ = io.WriteString(s, "Formatter method")
}

func TestBudgetActionErrorOpaqueFormattingPreservesNative(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{errors.New("ordinary"), actionFormattingError{}, actionFormattingError{panics: true}} {
		want := fmt.Sprintf("action in r: %v", cause)
		p := diagnosticParser(limits.Limits{Budget: 1 << 50})
		if got := catchFatal(t, func() { p.FailAction("r", cause) }); got.Error() != want || errors.Unwrap(got) != nil {
			t.Fatalf("opaque native formatting: %q, want %q", got, want)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := actionBudgetParser(ctx, limits.Limits{}, Bytes)
	if err := catchFatal(t, func() { p.FailAction("r", actionFormattingError{cancel: cancel}) }); err != context.Canceled {
		t.Fatalf("opaque boundary cancellation: %v", err)
	}
}

func TestBudgetActionErrorProducersAreEager(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("T", 1024)
	prog := mustCompile(t, "type A = {\n    N int\n}\n\nrule main = \"a\"\n")
	node := value.Ptr(value.Data{Kind: &value.NodeKind{Type: long}})
	producers := []struct {
		name string
		make func(*core.Parser) error
	}{
		{"undefined variable", func(p *core.Parser) error {
			_, err := closure.MachineOf(p).Eval(&core.EvalCtx{P: p}, &grammar.VarRef{Name: long})
			return err
		}},
		{"undefined capture", func(p *core.Parser) error {
			_, err := closure.MachineOf(p).Eval(&core.EvalCtx{P: p, Scope: &compile.Scope{}}, &grammar.CaptureRef{Name: long})
			return err
		}},
		{"invalid member", func(p *core.Parser) error { _, err := (&core.EvalCtx{P: p}).Member(1, long); return err }},
		{"missing field", func(p *core.Parser) error {
			_, err := (&core.EvalCtx{P: p}).Member(value.Ptr(value.Data{Kind: prog.TypeKind("A")}), long)
			return err
		}},
		{"function field", func(p *core.Parser) error {
			_, err := (&core.EvalCtx{P: p}).NewStruct("A", []string{long}, []any{budgetActionFunc{params: 1}})
			return err
		}},
		{"unary type", func(p *core.Parser) error { _, err := p.ValueUnary("-", node); return err }},
		{"binary type", func(p *core.Parser) error { _, err := p.ValueBinary("+", node, 1); return err }},
		{"list name", func(p *core.Parser) error { _, err := p.ValueListItems(long, 1); return err }},
		{"element name", func(p *core.Parser) error {
			_, err := (&core.EvalCtx{P: p}).Element(long, budgetActionFunc{})
			return err
		}},
		{"text type", func(p *core.Parser) error { _, err := (&core.EvalCtx{P: p}).TextString(budgetActionFunc{}); return err }},
	}
	for _, tc := range producers {
		t.Run(tc.name, func(t *testing.T) {
			p := diagnosticParser(limits.Limits{Budget: 1 << 50})
			p.Prog = &prog.sharedProgram
			owned := tc.make(p)
			if owned == nil {
				t.Fatal("expected expression error")
			}
			if _, ok := owned.(core.ActionTextError); !ok {
				t.Fatalf("not owned text: %T", owned)
			}
			p.Control = nil
			native := tc.make(p)
			if native.Error() != owned.Error() {
				t.Fatalf("native %q differs from controlled %q", native, owned)
			}
			p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 1})
			var partial error
			if err := catchFatal(t, func() { partial = tc.make(p) }); !errors.Is(err, limits.ErrLimit) || partial != nil {
				t.Fatalf("leaf was not eager: fatal=%v partial=%v", err, partial)
			}
		})
	}
}
