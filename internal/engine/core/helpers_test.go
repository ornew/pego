package core

import (
	"context"
	"testing"

	"github.com/ornew/pego/internal/limits"
)

// catchFatal runs f and returns the error of the Fatal panic it raises, or nil.
func catchFatal(t *testing.T, f func()) error {
	t.Helper()
	var got error
	func() {
		defer func() {
			if v := recover(); v != nil {
				fatal, ok := v.(Fatal)
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

// pollingContext reports cancellation from the at-th Err call on (never when at is 0); Done never
// fires, so only polling sees it.
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

// diagnosticParser returns a parser that charges diagnostic work to a control with the limits.
func diagnosticParser(limits limits.Limits) *Parser {
	return &Parser{Input: Input{Control: NewBudgetControl(nil, limits)}}
}
