package engine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

func snapshotVariants(t *testing.T, run func(*testing.T, ParseOptions)) {
	t.Helper()
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) { run(t, ParseOptions{Backend: backend, Unit: unit}) })
		}
	}
}

func TestSnapshotGraphIdentityAndOwnership(t *testing.T) {
	leaf := value.Ptr(value.Data{Kind: value.KindOf("Leaf", "leaf"), Start: 2, End: 3, Text: "x", Terminal: true, Children: []*value.Node{}, Fields: value.Fields{}})
	root := value.Ptr(value.Data{Kind: value.SeqKind, Start: 1, End: 4, Children: []*value.Node{leaf, nil, leaf}})
	value.D(root).Fields = value.Fields{{Name: "self", Value: root}, {Name: "leaf", Value: leaf}, {Name: "typedNil", Value: (*value.Node)(nil)}, {Name: "nil", Value: nil}, {Name: "int", Value: 42}, {Name: "text", Value: "hi"}, {Name: "bool", Value: true}}
	g, err := freezeSnapshotGraph(&parser{}, root)
	if err != nil {
		t.Fatal(err)
	}
	s := &Snapshot{graph: g}
	n := s.Root()
	if !n.Valid() || n.Type() != value.TypeTuple || n.Start() != 1 || n.End() != 4 || n.ChildCount() != 3 {
		t.Fatal("metadata")
	}
	if n.Child(0) != n.Child(2) || n.Child(1).Valid() || n.Child(0).Rule() != "leaf" || !n.Child(0).IsTerminal() {
		t.Fatal("identity")
	}
	if self, _ := n.Field("self"); self != n {
		t.Fatal("cycle")
	}
	if v, ok := n.Field("typedNil"); !ok || v != (SnapshotNode{}) {
		t.Fatal("typed nil")
	}
	if v, ok := n.Field("nil"); !ok || v != nil {
		t.Fatal("nil")
	}
	if _, ok := n.Field("absent"); ok {
		t.Fatal("absent")
	}
	fs := n.Fields()
	fs[0].Name = "changed"
	fs[0].Value = nil
	if v, ok := n.Field("self"); !ok || v != n {
		t.Fatal("field slice alias")
	}
	value.D(root).Start = 99
	value.D(root).Children[0] = nil
	value.D(root).Fields[0].Value = nil
	value.D(leaf).Text = "changed"
	if n.Start() != 1 || n.Child(0).Text() != "x" {
		t.Fatal("legacy alias")
	}
	a, b := s.Materialize(), s.Materialize()
	if a == b || value.D(a).Children[0] == value.D(b).Children[0] || value.D(a).Children[0] != value.D(a).Children[2] || value.D(a).Children[1] != nil || value.D(a).Fields[0].Value != a {
		t.Fatal("materialized graph")
	}
	if value.D(value.D(a).Children[0]).Children == nil || value.D(value.D(a).Children[0]).Fields == nil {
		t.Fatal("allocated empty lost")
	}
	if v, ok := value.D(a).Fields[2].Value.(*value.Node); !ok || v != nil {
		t.Fatal("materialized typed nil")
	}
	value.D(value.D(a).Children[0]).Text = "mutated"
	if n.Child(0).Text() != "x" || value.D(value.D(b).Children[0]).Text != "x" {
		t.Fatal("materialized alias")
	}
	var z SnapshotNode
	if z.Valid() || z.Type() != "" || z.Rule() != "" || z.Start() != 0 || z.End() != 0 || z.Text() != "" || z.IsTerminal() || z.ChildCount() != 0 || z.Fields() != nil {
		t.Fatal("zero metadata")
	}
	for _, handle := range []SnapshotNode{z, n} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("bad child index did not panic")
				}
			}()
			handle.Child(handle.ChildCount())
		}()
	}
}

func TestSnapshotGraphDeepAndUnsupported(t *testing.T) {
	root := &value.Node{}
	at := root
	for i := 0; i < 20000; i++ {
		next := &value.Node{}
		value.D(at).Children = []*value.Node{next}
		at = next
	}
	value.D(at).Fields = value.Fields{{Name: "back", Value: root}}
	g, err := freezeSnapshotGraph(&parser{}, root)
	if err != nil || len(g.nodes) != 20001 {
		t.Fatal("deep capture", err)
	}
	s := &Snapshot{graph: g}
	at = s.Materialize()
	for i := 0; i < 20000; i++ {
		at = value.D(at).Children[0]
	}
	if value.D(value.D(at).Fields[0].Value.(*value.Node)).Children[0] == nil {
		t.Fatal("deep cycle")
	}
	type namedInt int
	for _, val := range []any{namedInt(1), []int(nil), map[string]int(nil), (*int)(nil), (chan int)(nil), (func())(nil)} {
		child := value.Ptr(value.Data{Fields: value.Fields{{Name: "bad\"name", Value: val}}})
		root := value.Ptr(value.Data{Children: []*value.Node{child}, Fields: value.Fields{{Name: "other", Value: val}}})
		_, err := freezeSnapshotGraph(&parser{}, root)
		if err == nil || err.ValueType != reflect.TypeOf(val) || err.Path != `root.Children[0].Fields[0]("bad\"name")` {
			t.Fatalf("rejection %v", err)
		}
	}
}

func TestSnapshotDocumentVersionsAndConcurrentReads(t *testing.T) {
	prog := compile(t, `rule main = item* $$
rule item = [a-zé]`)
	snapshotVariants(t, func(t *testing.T, o ParseOptions) {
		d, err := prog.NewDocumentWith("main", "aé", o)
		if err != nil {
			t.Fatal(err)
		}
		s, err := d.SnapshotWith(o)
		if err != nil {
			t.Fatal(err)
		}
		original := resultJSON(s.Materialize(), s.Error())
		if s.Text() != "aé" || s.Version() != d.Version() || s.Unit() != o.Unit || s.Stats() != d.Stats() {
			t.Fatal("outcome metadata")
		}
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Go(func() {
				for j := 0; j < 100; j++ {
					if resultJSON(s.Materialize(), s.Error()) != original {
						t.Error("concurrent snapshot changed")
					}
					s.Root().Fields()
				}
			})
		}
		if err := d.Edit(0, 1, "z"); err != nil {
			t.Fatal(err)
		}
		latest, err := d.SnapshotWith(o)
		if err != nil {
			t.Fatal(err)
		}
		wg.Wait()
		if latest.Text() != "zé" || latest.Version() == s.Version() || s.Text() != "aé" || resultJSON(s.Materialize(), s.Error()) != original {
			t.Fatal("edit changed snapshot")
		}
		if err := d.Edit(0, 1, "!"); err != nil {
			t.Fatal(err)
		}
		failed, err := d.SnapshotWith(o)
		var syntax *SyntaxError
		if failed == nil || failed.Root().Valid() || !errors.As(err, &syntax) || failed.Text() != "!é" {
			t.Fatal("failed revision", err)
		}
		before := failed.Error().Error()
		syntax.Expected[0] = "mutated"
		syntax.Pos = 999
		if failed.Error().Error() != before {
			t.Fatal("returned error alias")
		}
		copy := failed.Error().(*SyntaxError)
		copy.Expected[0] = "again"
		copy.Line = 999
		if failed.Error().Error() != before {
			t.Fatal("getter error alias")
		}
	})
}

func TestSnapshotDiagnosticCopyAndNilResult(t *testing.T) {
	original := SyntaxErrors{&SyntaxError{Expected: []string{}, Messages: []string{"problem"}}, nil}
	copy := copySnapshotError(&parser{}, original).(SyntaxErrors)
	original[0].Messages[0] = "changed"
	original[0] = nil
	if copy[0].Messages[0] != "problem" || copy[0].Expected == nil || copy[1] != nil {
		t.Fatal("diagnostic copy")
	}
	prog := compile(t, `rule main = "x" => nil`)
	d, _ := prog.NewDocument("main", "x")
	s, err := d.SnapshotWith(ParseOptions{})
	if s == nil || err != nil || s.Root().Valid() || s.Materialize() != nil {
		t.Fatal("nil parse result")
	}
}

func TestSnapshotControlledFreezeBudgetAndRetry(t *testing.T) {
	if !workControlsEnabled {
		t.Skip("work controls reference")
	}
	prog := compile(t, `rule main = .* $$`)
	snapshotVariants(t, func(t *testing.T, o ParseOptions) {
		d, _ := prog.NewDocumentWith("main", strings.Repeat("x", 8192), o)
		if _, err := d.ParseWith(o); err != nil {
			t.Fatal(err)
		}
		// A memo-hit parse succeeds with this allowance. Capturing its
		// existing graph and text must still consume the same invocation.
		o.WorkLimits = limits.WorkLimits{MaxSteps: 100}
		if _, err := d.ParseWith(o); err != nil {
			t.Fatal("memo-hit fixture", err)
		}
		s, err := d.SnapshotWith(o)
		if s != nil || !errors.Is(err, limits.ErrWorkLimit) || d.memo.len() != 0 || d.runs != nil || d.kids != nil || d.vals != nil {
			t.Fatal("freeze interruption", s, err)
		}
		o.WorkLimits = limits.WorkLimits{}
		s, err = d.SnapshotWith(o)
		if s == nil || err != nil || s.Text() != d.Text() {
			t.Fatal("retry", err)
		}
		if resultJSON(s.Materialize(), s.Error()) != resultJSON(prog.ParseWith("main", d.Text(), o)) {
			t.Fatal("fresh mismatch")
		}
		// A cached parse fits, but copying all 8192 input bytes does not.
		o.WorkLimits = limits.WorkLimits{MaxInputWork: 4096}
		if _, err := d.ParseWith(o); err != nil {
			t.Fatal("input-work memo-hit fixture", err)
		}
		s, err = d.SnapshotWith(o)
		var limit *limits.WorkLimitError
		if s != nil || !errors.As(err, &limit) || limit.Kind != limits.InputWorkLimit {
			t.Fatal("text-freeze input-work limit", s, err)
		}
		o.WorkLimits = limits.WorkLimits{}
		if s, err = d.SnapshotWith(o); s == nil || err != nil {
			t.Fatal("input-work retry", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		o.Context = ctx
		before := d.memo
		stats := d.Stats()
		s, err = d.SnapshotWith(o)
		if s != nil || !errors.Is(err, context.Canceled) || d.memo != before || d.Stats() != stats {
			t.Fatal("pre-execution cancellation", err)
		}
	})
}

func TestSnapshotTracePanicCleanup(t *testing.T) {
	prog := compile(t, `rule main = x $$
rule x = x "a" / "a"`)
	snapshotVariants(t, func(t *testing.T, o ParseOptions) {
		d, err := prog.NewDocumentWith("main", "aa", o)
		if err != nil {
			t.Fatal(err)
		}
		marker := &struct{ label string }{"trace panic"}
		o.Trace = func(e TraceEvent) {
			if e.Kind == TraceExit && e.Rule == "x" && e.Memo && e.Matched {
				panic(marker)
			}
		}
		func() {
			defer func() {
				if got := recover(); got != marker {
					t.Fatalf("panic identity: %v", got)
				}
			}()
			d.SnapshotWith(o)
		}()
		if d.memo.len() != 0 || d.runs != nil || d.kids != nil || d.vals != nil {
			t.Fatal("trace panic retained reusable state")
		}
		o.Trace = nil
		s, err := d.SnapshotWith(o)
		if s == nil || err != nil || resultJSON(s.Materialize(), s.Error()) != resultJSON(prog.ParseWith("main", d.Text(), o)) {
			t.Fatal("trace panic retry", err)
		}
	})
}

// snapshotPhaseContext changes state only while the freezer is on the stack.
// This separates a parse-time cancellation from a freeze-time cancellation.
type snapshotPhaseContext struct {
	context.Context
	done       chan struct{}
	seen       bool
	panicValue any
	phase      string
}

func (c *snapshotPhaseContext) Done() <-chan struct{} { return c.done }
func (c *snapshotPhaseContext) Err() error {
	var callers [24]uintptr
	count := runtime.Callers(2, callers[:])
	frames := runtime.CallersFrames(callers[:count])
	for {
		f, more := frames.Next()
		phase := c.phase
		if phase == "" {
			phase = "freezeSnapshotGraph"
		}
		if strings.Contains(f.Function, phase) {
			c.seen = true
			if c.panicValue != nil {
				panic(c.panicValue)
			}
			return context.Canceled
		}
		if !more {
			break
		}
	}
	return nil
}

func TestSnapshotFreezeCancellationAndContextPanic(t *testing.T) {
	if !workControlsEnabled {
		t.Skip("work controls reference")
	}
	prog := compile(t, `rule main = .* $$`)
	snapshotVariants(t, func(t *testing.T, o ParseOptions) {
		d, _ := prog.NewDocumentWith("main", strings.Repeat("x", 4096), o)
		if _, err := d.ParseWith(o); err != nil {
			t.Fatal(err)
		}
		for _, panicCase := range []bool{false, true} {
			ctx := &snapshotPhaseContext{Context: context.Background(), done: make(chan struct{})}
			token := &struct{ message string }{"context panic"}
			if panicCase {
				ctx.panicValue = token
			}
			o.Context = ctx
			if panicCase {
				func() {
					defer func() {
						if got := recover(); got != token {
							t.Fatalf("panic identity: %v", got)
						}
					}()
					d.SnapshotWith(o)
				}()
			} else {
				s, err := d.SnapshotWith(o)
				if s != nil || !errors.Is(err, context.Canceled) {
					t.Fatalf("freeze cancellation: %v %v", s, err)
				}
			}
			if !ctx.seen || d.memo.len() != 0 || d.runs != nil || d.kids != nil || d.vals != nil {
				t.Fatal("freeze cleanup")
			}
			o.Context = nil
			if s, err := d.SnapshotWith(o); s == nil || err != nil {
				t.Fatal("retry", err)
			}
		}
	})
}

func TestSnapshotUnsupportedLegacyFieldPreservesParseError(t *testing.T) {
	prog := compile(t, `rule main = "x"`)
	d, _ := prog.NewDocument("main", "x")
	legacy, err := d.Parse()
	if err != nil {
		t.Fatal(err)
	}
	value.D(legacy).Fields = value.Fields{{Name: "bad", Value: []int{1}}}
	s, err := d.SnapshotWith(ParseOptions{})
	var capture *SnapshotCaptureError
	if s != nil || !errors.As(err, &capture) || capture.ValueType != reflect.TypeOf([]int{}) || !strings.Contains(capture.Path, "bad") || capture.ParseError != nil {
		t.Fatal("unsupported legacy field", s, err)
	}
	if d.Text() != "x" || d.Version().Sequence() != 1 {
		t.Fatal("capture failure mutated document")
	}
	value.D(legacy).Fields = nil
	if s, err := d.SnapshotWith(ParseOptions{}); s == nil || err != nil {
		t.Fatal("retry", err)
	}
}

func TestSnapshotRecoveredAndDepthOutcomes(t *testing.T) {
	for _, fixture := range []struct {
		grammar, text string
		depth         int
		recovered     bool
	}{
		{`rule main = x* $$
rule x = "b" #recover(skip="a")`, "a", 0, true},
		{`rule main = x
rule x = "a"`, "a", 1, false},
	} {
		prog := compile(t, fixture.grammar)
		snapshotVariants(t, func(t *testing.T, o ParseOptions) {
			d, err := prog.NewDocumentWith("main", fixture.text, o)
			if err != nil {
				t.Fatal(err)
			}
			o.MaxDepth = fixture.depth
			s, err := d.SnapshotWith(o)
			if s == nil || err == nil || s.Text() != fixture.text || s.Version() != d.Version() || s.Error() == nil {
				t.Fatal("snapshot error outcome", s, err)
			}
			if fixture.recovered {
				var received SyntaxErrors
				if !errors.As(err, &received) || !s.Root().Valid() || len(received) == 0 {
					t.Fatal("recovered outcome", err)
				}
				before := s.Error().Error()
				received[0].Pos = 999
				received[0].Expected = []string{"changed"}
				received[0].Messages = []string{"changed"}
				if s.Error().Error() != before {
					t.Fatal("returned recovered alias")
				}
				copy := s.Error().(SyntaxErrors)
				copy[0].Pos = 888
				copy[0].Messages = []string{"other"}
				if s.Error().Error() != before {
					t.Fatal("getter recovered alias")
				}
			} else if s.Root().Valid() {
				t.Fatal("depth error retained root")
			}
		})
	}
}
