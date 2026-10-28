//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

type workCancelledRefillReader struct {
	cancel context.CancelFunc
	first  []byte
	calls  int
}

func (r *workCancelledRefillReader) Read(dst []byte) (int, error) {
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

func TestWorkStreamRawReaderCancellation(t *testing.T) {
	prog := compile(t, `rule main = row* #stream $$

rule row = .+ #atomic`)
	workStreamVariants(t, func(t *testing.T, o ParseOptions) {
		for _, shape := range []string{"empty", "partial-rune"} {
			t.Run(shape, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				reader := &workCancelledRefillReader{cancel: cancel}
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

func TestWorkStreamRawReaderAdmission(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	caller := &workCancelledRefillReader{cancel: func() {}}
	var returned error
	observer := &streamReaderObserver{reader: caller, result: &returned, control: newWorkControl(ctx, limits.WorkLimits{})}
	err := catchFatal(t, func() { _, _ = observer.Read(make([]byte, 4)) })
	if !errors.Is(err, context.Canceled) || caller.calls != 0 || returned != nil {
		t.Fatalf("error=%v calls=%d returned=%v", err, caller.calls, returned)
	}
}

type workRawReaderPanicContext struct {
	context.Context
	reader *workCancelledRefillReader
	token  *workContextPanicToken
}

func (c *workRawReaderPanicContext) Err() error {
	if c.reader.calls != 0 {
		panic(c.token)
	}
	return c.Context.Err()
}

func TestWorkStreamRawReaderContextPanicIdentity(t *testing.T) {
	prog := compile(t, `rule main = row* #stream $$

rule row = .+ #atomic`)
	workStreamVariants(t, func(t *testing.T, o ParseOptions) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		reader := &workCancelledRefillReader{cancel: cancel, first: []byte{0xc3}}
		token := &workContextPanicToken{phase: "raw reader postcheck"}
		o.Context = &workRawReaderPanicContext{Context: ctx, reader: reader, token: token}
		var saved TraceEvent
		o.Trace = func(e TraceEvent) {
			if saved.p == nil {
				saved = e
			}
		}
		var err error
		got := workContextRecover(func() {
			err = prog.ParseStreamWith("main", reader, func(*value.Node) error { t.Fatal("unexpected emit"); return nil }, o)
		})
		if got != token || err != nil || reader.calls != 1 {
			t.Fatalf("panic=%#v error=%v calls=%d", got, err, reader.calls)
		}
		assertTraceParserDetached(t, saved.p, false)
	})
}
