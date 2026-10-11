//go:build !pego_work_controls_reference

package engine

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestWorkValueCopyInterruptionPreservesChildren(t *testing.T) {
	retained := &Node{Text: "retained", Start: 7, End: 9, Fields: Fields{{Name: "value", Value: "unchanged"}}}
	p := &parser{kidStack: make([]*Node, 257)}
	for i := range p.kidStack {
		p.kidStack[i] = retained
	}
	p.control = newWorkControl(nil, WorkLimits{MaxSteps: 128})
	if err := catchFatal(t, func() { p.kids(0) }); !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("copy interruption = %v", err)
	}
	// A failed copy does not publish a partial child list or clear its source.
	if len(p.kidStack) != 257 {
		t.Fatalf("interrupted child stack length = %d", len(p.kidStack))
	}
	for i, n := range p.kidStack {
		if n != retained {
			t.Fatalf("interrupted source child %d changed", i)
		}
	}
	p.control = nil
	kids := p.kids(0)
	if len(kids) != 257 || len(p.kidStack) != 0 {
		t.Fatalf("retry lengths = %d, %d", len(kids), len(p.kidStack))
	}
	for i, n := range kids {
		if n != retained {
			t.Fatalf("retry child %d lost its identity", i)
		}
	}
	if retained.Text != "retained" || retained.Start != 7 || retained.End != 9 || retained.Fields[0].Value != "unchanged" {
		t.Fatal("clearing tracking references modified the retained node")
	}
}

func TestWorkValueFieldAndEnvironmentWalks(t *testing.T) {
	fields := make(Fields, 300)
	st := &grammar.StructSpec{}
	var bindings *env
	for i := range fields {
		name := fmt.Sprintf("v%d", i)
		fields[i] = NodeField{Name: name, Value: i}
		st.Fields = append(st.Fields, &grammar.Field{Name: name})
		bindings = &env{name: name, val: i, next: bindings}
	}
	for _, run := range []struct {
		name string
		fn   func(*parser)
	}{
		{"get", func(p *parser) { p.valueField(fields, "absent") }},
		{"set", func(p *parser) { p.setValueField(&fields, "absent", 42) }},
		{"type fields", func(p *parser) { p.hasValueField(st, "absent") }},
		{"environment", func(p *parser) { p.lookupValue("absent") }},
	} {
		t.Run(run.name, func(t *testing.T) {
			p := &parser{env: bindings}
			p.control = newWorkControl(nil, WorkLimits{MaxSteps: 128})
			if err := catchFatal(t, func() { run.fn(p) }); !errors.Is(err, ErrWorkLimit) {
				t.Fatalf("walk interruption = %v", err)
			}
			if len(fields) != 300 || fields[299].Value != 299 {
				t.Fatal("interrupted field walk modified existing values")
			}
		})
	}
}

func TestWorkValueTrackingCleanupAfterInterruption(t *testing.T) {
	retained := &Node{Text: "retained"}
	p := &parser{created: make([]*Node, 258)}
	for i := range p.created {
		p.created[i] = retained
	}
	p.control = newWorkControl(nil, WorkLimits{MaxSteps: 128})
	if err := catchFatal(t, func() { p.dropCreated(1) }); !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("tracking cleanup interruption = %v", err)
	}
	// The interruption boundary must disable controls before mandatory cleanup.
	p.releaseScratch()
	p.dropCreated(1)
	if p.control != nil || len(p.created) != 1 || p.created[0] != retained || retained.Text != "retained" {
		t.Fatal("cleanup changed the tracking prefix or retained value")
	}
	for _, n := range p.created[1:cap(p.created)] {
		if n != nil {
			t.Fatal("mandatory cleanup left a tracking reference")
		}
	}
}

func TestWorkActionArenaResetInterruption(t *testing.T) {
	retained := &Node{Text: "retained"}
	p := &parser{}
	for range 300 {
		*p.closures.alloc() = closure{ctx: &evalCtx{items: []*Node{retained}}}
	}
	p.control = newWorkControl(nil, WorkLimits{MaxSteps: 64})
	if err := catchFatal(t, func() { p.useCtx(evalCtx{p: p}) }); !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("arena reset interruption = %v", err)
	}
	p.releaseScratch()
	p.useCtx(evalCtx{p: p})
	if p.closures.n != 0 || retained.Text != "retained" {
		t.Fatal("reset retry failed or changed an externally retained value")
	}
	for _, chunk := range p.closures.chunks {
		for _, f := range chunk {
			if f.ctx != nil || f.params != nil || f.body != nil {
				t.Fatal("reset retry left references in the arena")
			}
		}
	}
}
