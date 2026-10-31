//go:build !zp_budget_controls_reference

package engine

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/ornew/zeroparse/internal/engine/core"
	"github.com/ornew/zeroparse/internal/limits"
	"github.com/ornew/zeroparse/internal/value"
)

type budgetCancelledRefillReader struct {
	cancel context.CancelFunc
	first  []byte
	calls  int
}

func (r *budgetCancelledRefillReader) Read(dst []byte) (int, error) {
	r.calls++
	if r.calls == 1 {
		r.cancel()
		return copy(dst, r.first), nil
	}
	if len(r.first) == 0 {
		return 0, nil
	}
	return 0, io.EOF
}

func TestBudgetStreamRawReaderCancellation(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = row* #stream $$

rule row = .+ #atomic`)
	budgetStreamVariants(t, func(t *testing.T, o ParseOptions) {
		for _, shape := range []string{"empty", "partial-rune"} {
			t.Run(shape, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				reader := &budgetCancelledRefillReader{cancel: cancel}
				if shape == "partial-rune" {
					reader.first = []byte{0xc3}
				}
				o.Context = ctx
				emits := 0
				err := prog.ParseStreamWith("main", reader, func(*value.Node) error { emits++; return nil }, o)
				if !errors.Is(err, context.Canceled) || reader.calls != 1 || emits != 0 {
					t.Fatalf("error=%v readercalls=%d emits=%d", err, reader.calls, emits)
				}
			})
		}
	})
}

func TestBudgetStreamRawReaderAdmission(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	caller := &budgetCancelledRefillReader{cancel: func() {}}
	var returned error
	observer := &core.StreamReaderObserver{Reader: caller, Result: &returned, Control: core.NewBudgetControl(ctx, limits.Limits{})}
	err := catchFatal(t, func() { _, _ = observer.Read(make([]byte, 4)) })
	if !errors.Is(err, context.Canceled) || caller.calls != 0 || returned != nil {
		t.Fatalf("error=%v calls=%d returned=%v", err, caller.calls, returned)
	}
}

type budgetRawReaderPanicContext struct {
	context.Context
	reader *budgetCancelledRefillReader
	token  *budgetContextPanicToken
}

func (c *budgetRawReaderPanicContext) Err() error {
	if c.reader.calls != 0 {
		panic(c.token)
	}
	return c.Context.Err()
}

func TestBudgetStreamRawReaderContextPanicIdentity(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = row* #stream $$

rule row = .+ #atomic`)
	budgetStreamVariants(t, func(t *testing.T, o ParseOptions) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		reader := &budgetCancelledRefillReader{cancel: cancel, first: []byte{0xc3}}
		token := &budgetContextPanicToken{phase: "raw reader postcheck"}
		o.Context = &budgetRawReaderPanicContext{Context: ctx, reader: reader, token: token}
		var saved TraceEvent
		o.Trace = func(e TraceEvent) {
			if core.TraceParser(saved) == nil {
				saved = e
			}
		}
		var err error
		got := budgetContextRecover(func() {
			err = prog.ParseStreamWith("main", reader, func(*value.Node) error { t.Fatal("unexpected emit"); return nil }, o)
		})
		if got != token || err != nil || reader.calls != 1 {
			t.Fatalf("panic=%#v error=%v calls=%d", got, err, reader.calls)
		}
		assertTraceParserDetached(t, core.TraceParser(saved), false)
	})
}
