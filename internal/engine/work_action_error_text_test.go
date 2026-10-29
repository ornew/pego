//go:build !pego_work_controls_reference

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
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

func TestWorkActionErrorTextReservationsAndOwnership(t *testing.T) {
	for _, parts := range [][]string{{""}, {"", "", ""}, {"a", "", "λ\xff"}, {strings.Repeat("n", 8192)}} {
		want := strings.Join(parts, "")
		steps := uint64(len(want) + 2*len(parts))
		p := diagnosticParser(limits.WorkLimits{MaxSteps: steps})
		retained := p.ActionError(parts...)
		if retained.Error() != want || p.Control.StepsUsed != steps || p.Control.InputUsed != 0 {
			t.Fatalf("builder parity/counters: %q %d/%d", retained, p.Control.StepsUsed, p.Control.InputUsed)
		}
		p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: steps - 1})
		var partial error
		if err := catchFatal(t, func() { partial = p.ActionError(parts...) }); !errors.Is(err, limits.ErrWorkLimit) || partial != nil {
			t.Fatalf("one-short formatting: fatal=%v partial=%v", err, partial)
		}
		p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 100})
		if got := p.ActionError("retry").Error(); got != "retry" || retained.Error() != want {
			t.Fatal("retry changed owned text")
		}
	}
	// The second 128-byte write is rejected atomically, with no input work.
	p := diagnosticParser(limits.WorkLimits{MaxSteps: 130})
	err := catchFatal(t, func() { _ = p.ActionError(strings.Repeat("x", 512)) })
	var limit *limits.WorkLimitError
	if !errors.As(err, &limit) || limit.Kind != limits.StepWorkLimit || limit.Used != 130 || limit.Requested != 128 || p.Control.InputUsed != 0 {
		t.Fatalf("chunk reservation: %v", err)
	}
	for _, parts := range [][]string{make([]string, 320), {strings.Repeat("x", 1024)}} {
		p.Control = core.NewWorkControl(newPollingContext(1), limits.WorkLimits{})
		if err := catchFatal(t, func() { _ = p.ActionError(parts...) }); !errors.Is(err, context.Canceled) {
			t.Fatalf("fragment/copy cancellation: %v", err)
		}
	}
}

func TestWorkActionErrorFinalPrefixIsFlatAndBounded(t *testing.T) {
	cause := core.ActionTextError("division by zero")
	where := strings.Repeat("rule", 512)
	want := "action in " + where + ": " + cause.Error()
	p := diagnosticParser(limits.WorkLimits{MaxSteps: uint64(len(want) + 8)})
	err := catchFatal(t, func() { p.FailAction(where, cause) })
	if err.Error() != want || errors.Unwrap(err) != nil || errors.Is(err, cause) || p.Control.StepsUsed != uint64(len(want)+8) {
		t.Fatalf("final error contract: %v", err)
	}
	p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: uint64(len(want) + 7)})
	if err := catchFatal(t, func() { p.FailAction(where, cause) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("one-short wrapper: %v", err)
	}
	p.Control = core.NewWorkControl(newPollingContext(1), limits.WorkLimits{})
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

func TestWorkActionErrorOpaqueFormattingPreservesNative(t *testing.T) {
	for _, cause := range []error{errors.New("ordinary"), actionFormattingError{}, actionFormattingError{panics: true}} {
		want := fmt.Sprintf("action in r: %v", cause)
		p := diagnosticParser(limits.WorkLimits{MaxSteps: 1 << 50})
		if got := catchFatal(t, func() { p.FailAction("r", cause) }); got.Error() != want || errors.Unwrap(got) != nil {
			t.Fatalf("opaque native formatting: %q, want %q", got, want)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := actionWorkParser(ctx, limits.WorkLimits{}, Bytes)
	if err := catchFatal(t, func() { p.FailAction("r", actionFormattingError{cancel: cancel}) }); err != context.Canceled {
		t.Fatalf("opaque boundary cancellation: %v", err)
	}
}

func TestWorkActionErrorProducersAreEager(t *testing.T) {
	long := strings.Repeat("T", 1024)
	prog := mustCompile(t, "type A = {\n    N int\n}\n\nrule main = \"a\"\n")
	node := value.Ptr(value.Data{Kind: &value.NodeKind{Type: long}})
	producers := []struct {
		name string
		make func(*core.Parser) error
	}{
		{"undefined variable", func(p *core.Parser) error {
			_, err := closureMachineOf(p).eval(&core.EvalCtx{P: p}, &grammar.VarRef{Name: long})
			return err
		}},
		{"undefined capture", func(p *core.Parser) error {
			_, err := closureMachineOf(p).eval(&core.EvalCtx{P: p, Scope: &compile.Scope{}}, &grammar.CaptureRef{Name: long})
			return err
		}},
		{"invalid member", func(p *core.Parser) error { _, err := (&core.EvalCtx{P: p}).Member(1, long); return err }},
		{"missing field", func(p *core.Parser) error {
			_, err := (&core.EvalCtx{P: p}).Member(value.Ptr(value.Data{Kind: prog.TypeKind("A")}), long)
			return err
		}},
		{"function field", func(p *core.Parser) error {
			_, err := (&core.EvalCtx{P: p}).NewStruct("A", []string{long}, []any{workActionFunc{params: 1}})
			return err
		}},
		{"unary type", func(p *core.Parser) error { _, err := p.ValueUnary("-", node); return err }},
		{"binary type", func(p *core.Parser) error { _, err := p.ValueBinary("+", node, 1); return err }},
		{"list name", func(p *core.Parser) error { _, err := p.ValueListItems(long, 1); return err }},
		{"element name", func(p *core.Parser) error {
			_, err := (&core.EvalCtx{P: p}).Element(long, workActionFunc{})
			return err
		}},
		{"text type", func(p *core.Parser) error { _, err := (&core.EvalCtx{P: p}).TextString(workActionFunc{}); return err }},
	}
	for _, tc := range producers {
		t.Run(tc.name, func(t *testing.T) {
			p := diagnosticParser(limits.WorkLimits{MaxSteps: 1 << 50})
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
			p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 1})
			var partial error
			if err := catchFatal(t, func() { partial = tc.make(p) }); !errors.Is(err, limits.ErrWorkLimit) || partial != nil {
				t.Fatalf("leaf was not eager: fatal=%v partial=%v", err, partial)
			}
		})
	}
}
