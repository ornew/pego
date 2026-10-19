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
)

func TestWorkActionErrorTextReservationsAndOwnership(t *testing.T) {
	for _, parts := range [][]string{{""}, {"", "", ""}, {"a", "", "λ\xff"}, {strings.Repeat("n", 8192)}} {
		want := strings.Join(parts, "")
		steps := uint64(len(want) + 2*len(parts))
		p := diagnosticParser(WorkLimits{MaxSteps: steps})
		retained := p.actionError(parts...)
		if retained.Error() != want || p.control.stepsUsed != steps || p.control.inputUsed != 0 {
			t.Fatalf("builder parity/counters: %q %d/%d", retained, p.control.stepsUsed, p.control.inputUsed)
		}
		p.control = newWorkControl(nil, WorkLimits{MaxSteps: steps - 1})
		var partial error
		if err := catchFatal(t, func() { partial = p.actionError(parts...) }); !errors.Is(err, ErrWorkLimit) || partial != nil {
			t.Fatalf("one-short formatting: fatal=%v partial=%v", err, partial)
		}
		p.control = newWorkControl(nil, WorkLimits{MaxSteps: 100})
		if got := p.actionError("retry").Error(); got != "retry" || retained.Error() != want {
			t.Fatal("retry changed owned text")
		}
	}
	// The second 128-byte write is rejected atomically, with no input work.
	p := diagnosticParser(WorkLimits{MaxSteps: 130})
	err := catchFatal(t, func() { _ = p.actionError(strings.Repeat("x", 512)) })
	var limit *WorkLimitError
	if !errors.As(err, &limit) || limit.Kind != StepWorkLimit || limit.Used != 130 || limit.Requested != 128 || p.control.inputUsed != 0 {
		t.Fatalf("chunk reservation: %v", err)
	}
	for _, parts := range [][]string{make([]string, 320), {strings.Repeat("x", 1024)}} {
		p.control = newWorkControl(newPollingContext(1), WorkLimits{})
		if err := catchFatal(t, func() { _ = p.actionError(parts...) }); !errors.Is(err, context.Canceled) {
			t.Fatalf("fragment/copy cancellation: %v", err)
		}
	}
}

func TestWorkActionErrorFinalPrefixIsFlatAndBounded(t *testing.T) {
	cause := actionTextError("division by zero")
	where := strings.Repeat("rule", 512)
	want := "action in " + where + ": " + cause.Error()
	p := diagnosticParser(WorkLimits{MaxSteps: uint64(len(want) + 8)})
	err := catchFatal(t, func() { p.failAction(where, cause) })
	if err.Error() != want || errors.Unwrap(err) != nil || errors.Is(err, cause) || p.control.stepsUsed != uint64(len(want)+8) {
		t.Fatalf("final error contract: %v", err)
	}
	p.control = newWorkControl(nil, WorkLimits{MaxSteps: uint64(len(want) + 7)})
	if err := catchFatal(t, func() { p.failAction(where, cause) }); !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("one-short wrapper: %v", err)
	}
	p.control = newWorkControl(newPollingContext(1), WorkLimits{})
	if err := catchFatal(t, func() { p.failAction(where, cause) }); !errors.Is(err, context.Canceled) {
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
		p := diagnosticParser(WorkLimits{MaxSteps: 1 << 50})
		if got := catchFatal(t, func() { p.failAction("r", cause) }); got.Error() != want || errors.Unwrap(got) != nil {
			t.Fatalf("opaque native formatting: %q, want %q", got, want)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := actionWorkParser(ctx, WorkLimits{}, Bytes)
	if err := catchFatal(t, func() { p.failAction("r", actionFormattingError{cancel: cancel}) }); err != context.Canceled {
		t.Fatalf("opaque boundary cancellation: %v", err)
	}
}

func TestWorkActionErrorProducersAreEager(t *testing.T) {
	long := strings.Repeat("T", 1024)
	prog := compile(t, "type A = {\n    N int\n}\n\nrule main = \"a\"\n")
	node := &Node{kind: &nodeKind{typ: long}}
	producers := []struct {
		name string
		make func(*parser) error
	}{
		{"undefined variable", func(p *parser) error { _, err := (&evalCtx{p: p}).eval(&grammar.VarRef{Name: long}); return err }},
		{"undefined capture", func(p *parser) error {
			_, err := (&evalCtx{p: p, scope: &scope{}}).eval(&grammar.CaptureRef{Name: long})
			return err
		}},
		{"invalid member", func(p *parser) error { _, err := (&evalCtx{p: p}).member(1, long); return err }},
		{"missing field", func(p *parser) error {
			_, err := (&evalCtx{p: p}).member(&Node{kind: prog.typeKind("A")}, long)
			return err
		}},
		{"function field", func(p *parser) error {
			_, err := (&evalCtx{p: p}).newStruct("A", []string{long}, []any{workActionFunc{params: 1}})
			return err
		}},
		{"unary type", func(p *parser) error { _, err := p.valueUnary("-", node); return err }},
		{"binary type", func(p *parser) error { _, err := p.valueBinary("+", node, 1); return err }},
		{"list name", func(p *parser) error { _, err := p.valueListItems(long, 1); return err }},
		{"element name", func(p *parser) error { _, err := (&evalCtx{p: p}).element(long, workActionFunc{}); return err }},
		{"text type", func(p *parser) error { _, err := (&evalCtx{p: p}).textString(workActionFunc{}); return err }},
	}
	for _, tc := range producers {
		t.Run(tc.name, func(t *testing.T) {
			p := diagnosticParser(WorkLimits{MaxSteps: 1 << 50})
			p.prog = prog
			owned := tc.make(p)
			if owned == nil {
				t.Fatal("expected expression error")
			}
			if _, ok := owned.(actionTextError); !ok {
				t.Fatalf("not owned text: %T", owned)
			}
			p.control = nil
			native := tc.make(p)
			if native.Error() != owned.Error() {
				t.Fatalf("native %q differs from controlled %q", native, owned)
			}
			p.control = newWorkControl(nil, WorkLimits{MaxSteps: 1})
			var partial error
			if err := catchFatal(t, func() { partial = tc.make(p) }); !errors.Is(err, ErrWorkLimit) || partial != nil {
				t.Fatalf("leaf was not eager: fatal=%v partial=%v", err, partial)
			}
		})
	}
}
