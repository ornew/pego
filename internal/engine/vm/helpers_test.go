package vm

import (
	"context"
	"testing"

	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/engine/core"
	"github.com/ornew/zeroparse/internal/limits"
	"github.com/ornew/zeroparse/internal/value"
)

// catchFatal runs f and returns the error of the Fatal panic it raises, or nil.
func catchFatal(t *testing.T, f func()) error {
	t.Helper()
	var got error
	func() {
		defer func() {
			if v := recover(); v != nil {
				fatal, ok := v.(core.Fatal)
				if !ok {
					t.Fatalf("panic = %T(%v), want fatal", v, v)
				}
				got = fatal.Err
			}
		}()
		f()
	}()
	return got
}

func catchStreamPanic(f func()) (val any) { defer func() { val = recover() }(); f(); return }

func stateBudgetParser(lim limits.Limits) *core.Parser {
	prog := &core.SharedProgram{}
	core.InitSharedProgram(prog, &compile.Program{})
	return &core.Parser{Prog: prog, Input: core.Input{Control: core.NewBudgetControl(nil, lim)}}
}

func undoFixture(p *core.Parser) (*core.Frame, []*value.Node) {
	old0 := value.Ptr(value.Data{Text: "old-zero", Fields: value.Fields{{Name: "keep", Value: "zero"}}})
	first0 := value.Ptr(value.Data{Text: "first-zero"})
	latest0 := value.Ptr(value.Data{Text: "latest-zero"})
	old1 := value.Ptr(value.Data{Text: "old-one"})
	last1 := value.Ptr(value.Data{Text: "last-one"})
	f := &core.Frame{Vals: []*value.Node{latest0, old1}}
	p.Trail = []core.Undo{{F: f, Slot: 0, Old: old0}, {F: f, Slot: 0, Old: first0}, {F: f, Slot: 1, Old: old1}, {F: f, Slot: 1, Old: last1}}
	return f, []*value.Node{old0, first0, old1, last1, latest0}
}

func diagnosticParser(lim limits.Limits) *core.Parser {
	return &core.Parser{Input: core.Input{Control: core.NewBudgetControl(nil, lim)}}
}

type pollingContext struct {
	context.Context
	done   chan struct{}
	checks int
	at     int
}

func newPollingContext(at int) *pollingContext {
	return &pollingContext{Context: context.Background(), done: make(chan struct{}), at: at}
}

func (c *pollingContext) Done() <-chan struct{} { return c.done }
func (c *pollingContext) Err() error {
	c.checks++
	if c.at > 0 && c.checks >= c.at {
		return context.Canceled
	}
	return nil
}
