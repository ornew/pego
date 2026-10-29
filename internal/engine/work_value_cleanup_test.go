//go:build !pego_work_controls_reference

package engine

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

func TestWorkValueCopyInterruptionPreservesChildren(t *testing.T) {
	retained := value.Ptr(value.Data{Text: "retained", Start: 7, End: 9, Fields: value.Fields{{Name: "value", Value: "unchanged"}}})
	p := &core.Parser{KidStack: make([]*value.Node, 257)}
	for i := range p.KidStack {
		p.KidStack[i] = retained
	}
	p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 128})
	if err := catchFatal(t, func() { p.Kids(0) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("copy interruption = %v", err)
	}
	// A failed copy does not publish a partial child list or clear its source.
	if len(p.KidStack) != 257 {
		t.Fatalf("interrupted child stack length = %d", len(p.KidStack))
	}
	for i, n := range p.KidStack {
		if n != retained {
			t.Fatalf("interrupted source child %d changed", i)
		}
	}
	p.Control = nil
	kids := p.Kids(0)
	if len(kids) != 257 || len(p.KidStack) != 0 {
		t.Fatalf("retry lengths = %d, %d", len(kids), len(p.KidStack))
	}
	for i, n := range kids {
		if n != retained {
			t.Fatalf("retry child %d lost its identity", i)
		}
	}
	if value.D(retained).Text != "retained" || value.D(retained).Start != 7 || value.D(retained).End != 9 || value.D(retained).Fields[0].Value != "unchanged" {
		t.Fatal("clearing tracking references modified the retained node")
	}
}

func TestWorkValueFieldAndEnvironmentWalks(t *testing.T) {
	fields := make(value.Fields, 300)
	st := &grammar.TypeDef{Type: &grammar.StructType{}}
	var bindings *core.Env
	for i := range fields {
		name := fmt.Sprintf("v%d", i)
		fields[i] = value.NodeField{Name: name, Value: i}
		st.Struct().Fields = append(st.Struct().Fields, &grammar.Field{Name: name})
		bindings = &core.Env{Name: name, Val: i, Next: bindings}
	}
	for _, run := range []struct {
		name string
		fn   func(*core.Parser)
	}{
		{"get", func(p *core.Parser) { p.ValueField(fields, "absent") }},
		{"set", func(p *core.Parser) { p.SetValueField(&fields, "absent", 42) }},
		{"type fields", func(p *core.Parser) { p.HasValueField(st, "absent") }},
		{"environment", func(p *core.Parser) { p.LookupValue("absent") }},
	} {
		t.Run(run.name, func(t *testing.T) {
			p := &core.Parser{Env: bindings}
			p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 128})
			if err := catchFatal(t, func() { run.fn(p) }); !errors.Is(err, limits.ErrWorkLimit) {
				t.Fatalf("walk interruption = %v", err)
			}
			if len(fields) != 300 || fields[299].Value != 299 {
				t.Fatal("interrupted field walk modified existing values")
			}
		})
	}
}

func TestWorkValueTrackingCleanupAfterInterruption(t *testing.T) {
	retained := value.Ptr(value.Data{Text: "retained"})
	p := &core.Parser{Created: make([]*value.Node, 258)}
	for i := range p.Created {
		p.Created[i] = retained
	}
	p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 128})
	if err := catchFatal(t, func() { p.DropCreated(1) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("tracking cleanup interruption = %v", err)
	}
	// The interruption boundary must disable controls before mandatory cleanup.
	p.ReleaseScratch()
	p.DropCreated(1)
	if p.Control != nil || len(p.Created) != 1 || p.Created[0] != retained || value.D(retained).Text != "retained" {
		t.Fatal("cleanup changed the tracking prefix or retained value")
	}
	for _, n := range p.Created[1:cap(p.Created)] {
		if n != nil {
			t.Fatal("mandatory cleanup left a tracking reference")
		}
	}
}

func TestWorkActionArenaResetInterruption(t *testing.T) {
	retained := value.Ptr(value.Data{Text: "retained"})
	p := &core.Parser{}
	cm := closureMachineOf(p)
	for range 300 {
		*cm.closures.Alloc() = closure{ctx: &core.EvalCtx{Items: []*value.Node{retained}}}
	}
	p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 64})
	if err := catchFatal(t, func() { evalTerm(p.UseCtx(core.EvalCtx{P: p}), &grammar.NilLit{}) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("arena reset interruption = %v", err)
	}
	p.ReleaseScratch()
	evalTerm(p.UseCtx(core.EvalCtx{P: p}), &grammar.NilLit{})
	if closureMachineOf(p) != cm || cm.closures.N != 0 || value.D(retained).Text != "retained" {
		t.Fatal("reset retry failed or changed an externally retained value")
	}
	for _, chunk := range cm.closures.Chunks {
		for _, f := range chunk {
			if f.ctx != nil || f.params != nil || f.body != nil {
				t.Fatal("reset retry left references in the arena")
			}
		}
	}
}
