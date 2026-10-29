//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"fmt"
	"testing"
)

func workDocumentVariants(t *testing.T, run func(*testing.T, ParseOptions)) {
	t.Helper()
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, u := range []PositionUnit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", b, u), func(t *testing.T) { run(t, ParseOptions{Backend: b, Unit: u}) })
		}
	}
}

func TestWorkDocumentConstructorCallerPanicIdentity(t *testing.T) {
	prog := mustCompile(t, `rule main = .* $$`)
	workDocumentVariants(t, func(t *testing.T, base ParseOptions) {
		for _, at := range []int{0, 1, 2} {
			token := &workContextPanicToken{phase: fmt.Sprint(at)}
			o := base
			ctx := newWorkContextErrPanic(token, at)
			ctx.armed = true
			o.Context = ctx
			if at == 0 {
				o.Context = &workContextDonePanic{Context: context.Background(), token: token}
			}
			var d *Document
			var err error
			got := workContextRecover(func() { d, err = prog.NewDocumentWith("main", "é💡abc", o) })
			if got != token || d != nil || err != nil {
				t.Fatalf("checkpoint %d: panic=%v document=%v error=%v", at, got, d, err)
			}
			if at > 0 && ctx.calls != at {
				t.Fatalf("calls=%d want %d", ctx.calls, at)
			}
		}
	})
}

type workDocumentInactiveContext struct{ context.Context }

func (workDocumentInactiveContext) Err() error { panic("inactive context must not be polled") }

func TestWorkDocumentInactiveContext(t *testing.T) {
	prog := mustCompile(t, `rule main = "a" $$`)
	workDocumentVariants(t, func(t *testing.T, o ParseOptions) {
		o.Context = workDocumentInactiveContext{Context: context.Background()}
		d, err := prog.NewDocumentWith("main", "a", o)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if n, err := d.Parse(); n == nil || err != nil {
				t.Fatalf("inactive parse: node=%v error=%v", n, err)
			}
		}
	})
}
