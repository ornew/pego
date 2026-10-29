//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

func stateWorkParser(limits limits.WorkLimits) *parser {
	return &parser{prog: &sharedProgram{staticProgram: &compile.Program{}}, input: input{control: newWorkControl(nil, limits)}}
}

func undoFixture(p *parser) (*frame, []*value.Node) {
	old0 := value.Ptr(value.Data{Text: "old-zero", Fields: value.Fields{{Name: "keep", Value: "zero"}}})
	first0 := value.Ptr(value.Data{Text: "first-zero"})
	latest0 := value.Ptr(value.Data{Text: "latest-zero"})
	old1 := value.Ptr(value.Data{Text: "old-one"})
	last1 := value.Ptr(value.Data{Text: "last-one"})
	f := &frame{vals: []*value.Node{latest0, old1}}
	p.trail = []undo{{f: f, slot: 0, old: old0}, {f: f, slot: 0, old: first0}, {f: f, slot: 1, old: old1}, {f: f, slot: 1, old: last1}}
	return f, []*value.Node{old0, first0, old1, last1, latest0}
}

func assertUndoRestored(t *testing.T, f *frame, nodes []*value.Node) {
	t.Helper()
	if f.vals[0] != nodes[0] || f.vals[1] != nodes[2] {
		t.Fatalf("restored slots = (%p,%p), want (%p,%p)", f.vals[0], f.vals[1], nodes[0], nodes[2])
	}
	if value.D(nodes[0]).Text != "old-zero" || value.D(nodes[0]).Fields[0].Value != "zero" || value.D(nodes[1]).Text != "first-zero" || value.D(nodes[2]).Text != "old-one" || value.D(nodes[3]).Text != "last-one" {
		t.Fatal("undo changed a retained Node pointee")
	}
}

func TestWorkResetRestoresRepeatedCaptureWritesInReverse(t *testing.T) {
	p := stateWorkParser(limits.WorkLimits{MaxSteps: 4})
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
			p := stateWorkParser(limits.WorkLimits{MaxSteps: 2})
			f, nodes := undoFixture(p)
			p.frame = f
			p.pos = 99
			p.env = &env{name: "temporary", val: 1}
			p.recovered = []*SyntaxError{{Messages: []string{"temporary"}}}
			if name == "reset" {
				m := mark{pos: 4, trail: 0}
				if err := catchFatal(t, func() { p.reset(m) }); !errors.Is(err, limits.ErrWorkLimit) {
					t.Fatalf("reset interruption = %v, want ErrWorkLimit", err)
				}
				if p.pos != 4 || len(p.trail) != 4 {
					t.Fatalf("reset partial state: pos=%d trail=%d", p.pos, len(p.trail))
				}
			} else {
				vmMachineOf(p).vals = []any{"keep", "discard-one", "discard-two"}
				vmMachineOf(p).reps = []repState{{count: 1}, {count: 2}}
				save := vmSave{pos: 4, vals: 1, reps: 1, trail: 0, recovered: 0, env: nil, frame: f}
				if err := catchFatal(t, func() { vmMachineOf(p).restore(&save) }); !errors.Is(err, limits.ErrWorkLimit) {
					t.Fatalf("VM restore interruption = %v, want ErrWorkLimit", err)
				}
				if p.pos != 4 || len(vmMachineOf(p).vals) != 1 || len(vmMachineOf(p).reps) != 1 || len(p.trail) != 4 {
					t.Fatalf("restore partial state: pos=%d vals=%d reps=%d trail=%d", p.pos, len(vmMachineOf(p).vals), len(vmMachineOf(p).reps), len(p.trail))
				}
			}
			// The budget permits the two newest undo records; the oldest remains pending.
			if f.vals[0] != nodes[4] || f.vals[1] != nodes[2] {
				t.Fatalf("partial reverse restore slots=(%p,%p), want (%p,%p)", f.vals[0], f.vals[1], nodes[4], nodes[2])
			}
			if value.D(nodes[0]).Text != "old-zero" || value.D(nodes[0]).Fields[0].Value != "zero" {
				t.Fatal("partial rollback mutated retained node data")
			}
		})
	}
}

func TestWorkResetPollingCancellationDuringUndo(t *testing.T) {
	p := stateWorkParser(limits.WorkLimits{})
	f := &frame{vals: make([]*value.Node, 257)}
	for i := range f.vals {
		old := value.Ptr(value.Data{Text: "old"})
		f.vals[i] = value.Ptr(value.Data{Text: "new"})
		p.trail = append(p.trail, undo{f: f, slot: i, old: old})
	}
	p.control = newWorkControl(newPollingContext(1), limits.WorkLimits{})
	err := catchFatal(t, func() { p.reset(mark{trail: 0}) })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("undo cancellation = %v, want context.Canceled", err)
	}
	if len(p.trail) != 257 {
		t.Fatalf("canceled reset truncated trail to %d", len(p.trail))
	}
	for i := range f.vals {
		if f.vals[i] == nil || value.D(f.vals[i]).Text != "old" && value.D(f.vals[i]).Text != "new" {
			t.Fatalf("undo cancellation damaged slot %d: %#v", i, f.vals[i])
		}
	}
}

func TestWorkAttachCapturesNilFrameAndFieldOrder(t *testing.T) {
	retained := value.Ptr(value.Data{Text: "base", Fields: value.Fields{{Name: "existing", Value: "kept"}}})
	allNil := &frame{vals: make([]*value.Node, 320)}
	p := stateWorkParser(limits.WorkLimits{MaxSteps: 100_000})
	got := p.attachCaptures(retained, &compile.Scope{Names: make([]string, len(allNil.vals))}, allNil, 2, 9)
	if got != retained || len(value.D(retained).Fields) != 1 || value.D(retained).Fields[0].Value != "kept" {
		t.Fatal("all-nil capture frame changed the base node")
	}

	fields := []string{"first", "duplicate", "third", "duplicate", "late_" + strings.Repeat("p", 80)}
	values := []*value.Node{
		value.Ptr(value.Data{Text: "first-value"}), value.Ptr(value.Data{Text: "old-duplicate"}), value.Ptr(value.Data{Text: "third-value"}), value.Ptr(value.Data{Text: "new-duplicate"}), value.Ptr(value.Data{Text: "late-value"}),
	}
	base := value.Ptr(value.Data{Text: "fresh-body", Fresh: true})
	result := p.attachCaptures(base, &compile.Scope{Names: fields}, &frame{vals: values}, 0, 5)
	if result != base {
		t.Fatal("fresh body was unexpectedly wrapped")
	}
	wantNames := []string{"first", "duplicate", "third", fields[4]}
	if len(value.D(result).Fields) != len(wantNames) {
		t.Fatalf("capture field count = %d, want %d", len(value.D(result).Fields), len(wantNames))
	}
	for i, name := range wantNames {
		if value.D(result).Fields[i].Name != name {
			t.Fatalf("field %d name = %q, want %q", i, value.D(result).Fields[i].Name, name)
		}
	}
	if value.D(result).Fields[1].Value != values[3] || value.D(result).Fields[3].Value != values[4] {
		t.Fatal("capture overwrite/order semantics changed")
	}
}

func TestWorkAttachCapturesWrapsNonfreshAndSelfCapture(t *testing.T) {
	child := value.Ptr(value.Data{Text: "retained-child"})
	prior := value.Fields{{Name: "prior", Value: "unchanged"}}
	shared := value.Ptr(value.Data{Text: "shared", Fields: append(value.Fields(nil), prior...)})
	p := stateWorkParser(limits.WorkLimits{MaxSteps: 100_000})
	wrapped := p.attachCaptures(shared, &compile.Scope{Names: []string{"child"}}, &frame{vals: []*value.Node{child}}, 4, 7)
	if wrapped == shared || len(value.D(wrapped).Children) != 1 || value.D(wrapped).Children[0] != shared {
		t.Fatal("nonfresh node was not wrapped before capture attachment")
	}
	if got, ok := value.D(wrapped).Fields.Get("child"); !ok || got != child {
		t.Fatalf("wrapped child capture = %v, %t", got, ok)
	}
	if !reflect.DeepEqual(value.D(shared).Fields, prior) || value.D(shared).Text != "shared" || value.D(child).Text != "retained-child" {
		t.Fatal("capture attachment modified a retained node")
	}

	self := value.Ptr(value.Data{Text: "self", Fresh: true})
	got := p.attachCaptures(self, &compile.Scope{Names: []string{"self"}}, &frame{vals: []*value.Node{self}}, 1, 2)
	if got == self || len(value.D(got).Children) != 1 || value.D(got).Children[0] != self {
		t.Fatal("self-capture did not wrap the body node")
	}
	if val, ok := value.D(got).Fields.Get("self"); !ok || val != self || len(value.D(self).Fields) != 0 {
		t.Fatal("self-capture fields or source node changed")
	}
}

func TestWorkAttachCapturesInterruptsDiscoveryAndFieldWalk(t *testing.T) {
	base := value.Ptr(value.Data{Text: "retained", Fields: value.Fields{{Name: "prior", Value: "kept"}}})
	wide := &frame{vals: make([]*value.Node, 320)}
	p := stateWorkParser(limits.WorkLimits{MaxSteps: 128})
	if err := catchFatal(t, func() { _ = p.attachCaptures(base, &compile.Scope{Names: make([]string, 320)}, wide, 0, 1) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("all-nil discovery interruption = %v, want ErrWorkLimit", err)
	}
	if len(value.D(base).Fields) != 1 || value.D(base).Fields[0].Value != "kept" {
		t.Fatal("interrupted all-nil scan changed the retained node")
	}

	const n = 48
	names := make([]string, n)
	vals := make([]*value.Node, n)
	for i := range names {
		names[i] = fmt.Sprintf("capture_%03d_%s", i, strings.Repeat("common_prefix_", 12))
		vals[i] = value.Ptr(value.Data{Text: fmt.Sprintf("value-%d", i)})
	}
	retained := value.Ptr(value.Data{Text: "shared-retained", Fields: value.Fields{{Name: "existing", Value: "stable"}}})
	p = stateWorkParser(limits.WorkLimits{MaxSteps: 256})
	if err := catchFatal(t, func() { _ = p.attachCaptures(retained, &compile.Scope{Names: names}, &frame{vals: vals}, 3, 9) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("capture field walk interruption = %v, want ErrWorkLimit", err)
	}
	if len(value.D(retained).Fields) != 1 || value.D(retained).Fields[0].Name != "existing" || value.D(retained).Fields[0].Value != "stable" || value.D(retained).Text != "shared-retained" {
		t.Fatal("interrupted capture assembly modified the retained nonfresh node")
	}
}
