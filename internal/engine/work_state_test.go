//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func stateWorkParser(limits WorkLimits) *parser {
	return &parser{input: input{control: newWorkControl(nil, limits)}}
}

func undoFixture(p *parser) (*frame, []*Node) {
	old0 := &Node{Text: "old-zero", Fields: Fields{{Name: "keep", Value: "zero"}}}
	first0 := &Node{Text: "first-zero"}
	latest0 := &Node{Text: "latest-zero"}
	old1 := &Node{Text: "old-one"}
	last1 := &Node{Text: "last-one"}
	f := &frame{vals: []*Node{latest0, old1}}
	p.trail = []undo{{f: f, slot: 0, old: old0}, {f: f, slot: 0, old: first0}, {f: f, slot: 1, old: old1}, {f: f, slot: 1, old: last1}}
	return f, []*Node{old0, first0, old1, last1, latest0}
}

func assertUndoRestored(t *testing.T, f *frame, nodes []*Node) {
	t.Helper()
	if f.vals[0] != nodes[0] || f.vals[1] != nodes[2] {
		t.Fatalf("restored slots = (%p,%p), want (%p,%p)", f.vals[0], f.vals[1], nodes[0], nodes[2])
	}
	if nodes[0].Text != "old-zero" || nodes[0].Fields[0].Value != "zero" || nodes[1].Text != "first-zero" || nodes[2].Text != "old-one" || nodes[3].Text != "last-one" {
		t.Fatal("undo changed a retained Node pointee")
	}
}

func TestWorkResetRestoresRepeatedCaptureWritesInReverse(t *testing.T) {
	p := stateWorkParser(WorkLimits{MaxSteps: 4})
	f, nodes := undoFixture(p)
	p.pos = 71
	p.frame = f
	p.env = &env{name: "temporary", val: 1}
	p.recovered = []*SyntaxError{{Messages: []string{"temporary"}}}
	m := mark{pos: 4, env: nil, trail: 0, recovered: 0}

	p.reset(m)
	assertUndoRestored(t, f, nodes)
	if p.pos != 4 || p.env != nil || len(p.recovered) != 0 || len(p.trail) != 0 {
		t.Fatalf("reset state: pos=%d env=%p recovered=%d trail=%d", p.pos, p.env, len(p.recovered), len(p.trail))
	}
}

func TestWorkResetAndVMRestoreInterruptionIsFatal(t *testing.T) {
	for _, name := range []string{"reset", "restore"} {
		t.Run(name, func(t *testing.T) {
			p := stateWorkParser(WorkLimits{MaxSteps: 2})
			f, nodes := undoFixture(p)
			p.frame = f
			p.pos = 99
			p.env = &env{name: "temporary", val: 1}
			p.recovered = []*SyntaxError{{Messages: []string{"temporary"}}}
			if name == "reset" {
				m := mark{pos: 4, trail: 0}
				if err := catchFatal(t, func() { p.reset(m) }); !errors.Is(err, ErrWorkLimit) {
					t.Fatalf("reset interruption = %v, want ErrWorkLimit", err)
				}
				if p.pos != 4 || len(p.trail) != 4 {
					t.Fatalf("reset partial state: pos=%d trail=%d", p.pos, len(p.trail))
				}
			} else {
				p.vals = []any{"keep", "discard-one", "discard-two"}
				p.reps = []repState{{count: 1}, {count: 2}}
				save := vmSave{pos: 4, vals: 1, reps: 1, trail: 0, recovered: 0, env: nil, frame: f}
				if err := catchFatal(t, func() { p.restore(&save) }); !errors.Is(err, ErrWorkLimit) {
					t.Fatalf("VM restore interruption = %v, want ErrWorkLimit", err)
				}
				if p.pos != 4 || len(p.vals) != 1 || len(p.reps) != 1 || len(p.trail) != 4 {
					t.Fatalf("restore partial state: pos=%d vals=%d reps=%d trail=%d", p.pos, len(p.vals), len(p.reps), len(p.trail))
				}
			}
			// The budget permits the two newest undo records; the oldest remains pending.
			if f.vals[0] != nodes[4] || f.vals[1] != nodes[2] {
				t.Fatalf("partial reverse restore slots=(%p,%p), want (%p,%p)", f.vals[0], f.vals[1], nodes[4], nodes[2])
			}
			if nodes[0].Text != "old-zero" || nodes[0].Fields[0].Value != "zero" {
				t.Fatal("partial rollback mutated retained node data")
			}
		})
	}
}

func TestWorkResetPollingCancellationDuringUndo(t *testing.T) {
	p := stateWorkParser(WorkLimits{})
	f := &frame{vals: make([]*Node, 257)}
	for i := range f.vals {
		old := &Node{Text: "old"}
		f.vals[i] = &Node{Text: "new"}
		p.trail = append(p.trail, undo{f: f, slot: i, old: old})
	}
	p.control = newWorkControl(newPollingContext(1), WorkLimits{})
	err := catchFatal(t, func() { p.reset(mark{trail: 0}) })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("undo cancellation = %v, want context.Canceled", err)
	}
	if len(p.trail) != 257 {
		t.Fatalf("canceled reset truncated trail to %d", len(p.trail))
	}
	for i := range f.vals {
		if f.vals[i] == nil || f.vals[i].Text != "old" && f.vals[i].Text != "new" {
			t.Fatalf("undo cancellation damaged slot %d: %#v", i, f.vals[i])
		}
	}
}

func TestWorkAttachCapturesNilFrameAndFieldOrder(t *testing.T) {
	retained := &Node{Text: "base", Fields: Fields{{Name: "existing", Value: "kept"}}}
	allNil := &frame{vals: make([]*Node, 320)}
	p := stateWorkParser(WorkLimits{MaxSteps: 100_000})
	got := p.attachCaptures(retained, &scope{names: make([]string, len(allNil.vals))}, allNil, 2, 9)
	if got != retained || len(retained.Fields) != 1 || retained.Fields[0].Value != "kept" {
		t.Fatal("all-nil capture frame changed the base node")
	}

	fields := []string{"first", "duplicate", "third", "duplicate", "late_" + strings.Repeat("p", 80)}
	values := []*Node{
		{Text: "first-value"}, {Text: "old-duplicate"}, {Text: "third-value"}, {Text: "new-duplicate"}, {Text: "late-value"},
	}
	base := &Node{Text: "fresh-body", fresh: true}
	result := p.attachCaptures(base, &scope{names: fields}, &frame{vals: values}, 0, 5)
	if result != base {
		t.Fatal("fresh body was unexpectedly wrapped")
	}
	wantNames := []string{"first", "duplicate", "third", fields[4]}
	if len(result.Fields) != len(wantNames) {
		t.Fatalf("capture field count = %d, want %d", len(result.Fields), len(wantNames))
	}
	for i, name := range wantNames {
		if result.Fields[i].Name != name {
			t.Fatalf("field %d name = %q, want %q", i, result.Fields[i].Name, name)
		}
	}
	if result.Fields[1].Value != values[3] || result.Fields[3].Value != values[4] {
		t.Fatal("capture overwrite/order semantics changed")
	}
}

func TestWorkAttachCapturesWrapsNonfreshAndSelfCapture(t *testing.T) {
	child := &Node{Text: "retained-child"}
	prior := Fields{{Name: "prior", Value: "unchanged"}}
	shared := &Node{Text: "shared", Fields: append(Fields(nil), prior...)}
	p := stateWorkParser(WorkLimits{MaxSteps: 100_000})
	wrapped := p.attachCaptures(shared, &scope{names: []string{"child"}}, &frame{vals: []*Node{child}}, 4, 7)
	if wrapped == shared || len(wrapped.Children) != 1 || wrapped.Children[0] != shared {
		t.Fatal("nonfresh node was not wrapped before capture attachment")
	}
	if got, ok := wrapped.Fields.Get("child"); !ok || got != child {
		t.Fatalf("wrapped child capture = %v, %t", got, ok)
	}
	if !reflect.DeepEqual(shared.Fields, prior) || shared.Text != "shared" || child.Text != "retained-child" {
		t.Fatal("capture attachment modified a retained node")
	}

	self := &Node{Text: "self", fresh: true}
	got := p.attachCaptures(self, &scope{names: []string{"self"}}, &frame{vals: []*Node{self}}, 1, 2)
	if got == self || len(got.Children) != 1 || got.Children[0] != self {
		t.Fatal("self-capture did not wrap the body node")
	}
	if value, ok := got.Fields.Get("self"); !ok || value != self || len(self.Fields) != 0 {
		t.Fatal("self-capture fields or source node changed")
	}
}

func TestWorkAttachCapturesInterruptsDiscoveryAndFieldWalk(t *testing.T) {
	base := &Node{Text: "retained", Fields: Fields{{Name: "prior", Value: "kept"}}}
	wide := &frame{vals: make([]*Node, 320)}
	p := stateWorkParser(WorkLimits{MaxSteps: 128})
	if err := catchFatal(t, func() { _ = p.attachCaptures(base, &scope{names: make([]string, 320)}, wide, 0, 1) }); !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("all-nil discovery interruption = %v, want ErrWorkLimit", err)
	}
	if len(base.Fields) != 1 || base.Fields[0].Value != "kept" {
		t.Fatal("interrupted all-nil scan changed the retained node")
	}

	const n = 48
	names := make([]string, n)
	vals := make([]*Node, n)
	for i := range names {
		names[i] = fmt.Sprintf("capture_%03d_%s", i, strings.Repeat("common_prefix_", 12))
		vals[i] = &Node{Text: fmt.Sprintf("value-%d", i)}
	}
	retained := &Node{Text: "shared-retained", Fields: Fields{{Name: "existing", Value: "stable"}}}
	p = stateWorkParser(WorkLimits{MaxSteps: 256})
	if err := catchFatal(t, func() { _ = p.attachCaptures(retained, &scope{names: names}, &frame{vals: vals}, 3, 9) }); !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("capture field walk interruption = %v, want ErrWorkLimit", err)
	}
	if len(retained.Fields) != 1 || retained.Fields[0].Name != "existing" || retained.Fields[0].Value != "stable" || retained.Text != "shared-retained" {
		t.Fatal("interrupted capture assembly modified the retained nonfresh node")
	}
}
