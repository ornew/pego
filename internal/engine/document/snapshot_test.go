package document_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/ornew/pego/internal/engine/document"

	. "github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
)

func TestSnapshotControlledFreezeBudgetAndRetry(t *testing.T) {
	if !core.WorkControlsEnabled {
		t.Skip("work controls reference")
	}
	prog := mustCompile(t, `rule main = .* $$`)
	snapshotVariants(t, func(t *testing.T, o ParseOptions) {
		d, _ := prog.NewDocumentWith("main", strings.Repeat("x", 8192), o)
		if _, err := d.ParseWith(o); err != nil {
			t.Fatal(err)
		}
		// A memo-hit parse succeeds with this allowance. Capturing its
		// existing graph and text must still consume the same invocation.
		o.Limits = limits.Limits{MaxWork: 100}
		if _, err := d.ParseWith(o); err != nil {
			t.Fatal("memo-hit fixture", err)
		}
		s, err := d.SnapshotWith(o)
		if s != nil || !errors.Is(err, limits.ErrLimit) || d.Memo().Len() != 0 || d.Runs() != nil || d.Kids() != nil || d.Machine() != nil {
			t.Fatal("freeze interruption", s, err)
		}
		o.Limits = limits.Limits{}
		s, err = d.SnapshotWith(o)
		if s == nil || err != nil || s.Text() != d.Text() {
			t.Fatal("retry", err)
		}
		if resultJSON(s.Materialize(), s.Error()) != resultJSON(prog.ParseWith("main", d.Text(), o)) {
			t.Fatal("fresh mismatch")
		}
		// A cached parse fits, but copying all 8192 input bytes does not.
		o.Limits = limits.Limits{MaxWork: 4096}
		if _, err := d.ParseWith(o); err != nil {
			t.Fatal("work-limit memo-hit fixture", err)
		}
		s, err = d.SnapshotWith(o)
		var limit *limits.LimitError
		if s != nil || !errors.As(err, &limit) || limit.Kind != limits.LimitWork {
			t.Fatal("text-freeze work limit", s, err)
		}
		o.Limits = limits.Limits{}
		if s, err = d.SnapshotWith(o); s == nil || err != nil {
			t.Fatal("work-limit retry", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		o.Context = ctx
		before := d.Memo()
		stats := d.Stats()
		s, err = d.SnapshotWith(o)
		if s != nil || !errors.Is(err, context.Canceled) || d.Memo() != before || d.Stats() != stats {
			t.Fatal("pre-execution cancellation", err)
		}
	})
}

func TestSnapshotDiagnosticCopyAndNilResult(t *testing.T) {
	original := SyntaxErrors{&SyntaxError{Expected: []string{}, Messages: []string{"problem"}}, nil}
	copy := document.CopySnapshotError(&core.Parser{}, original).(SyntaxErrors)
	original[0].Messages[0] = "changed"
	original[0] = nil
	if copy[0].Messages[0] != "problem" || copy[0].Expected == nil || copy[1] != nil {
		t.Fatal("diagnostic copy")
	}
	prog := mustCompile(t, `rule main = "x" => nil`)
	d, _ := prog.NewDocument("main", "x")
	s, err := d.SnapshotWith(ParseOptions{})
	if s == nil || err != nil || s.Root().Valid() || s.Materialize() != nil {
		t.Fatal("nil parse result")
	}
}

func TestSnapshotDocumentVersionsAndConcurrentReads(t *testing.T) {
	prog := mustCompile(t, `rule main = item* $$
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

func TestSnapshotFreezeCancellationAndContextPanic(t *testing.T) {
	if !core.WorkControlsEnabled {
		t.Skip("work controls reference")
	}
	prog := mustCompile(t, `rule main = .* $$`)
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
			if !ctx.seen || d.Memo().Len() != 0 || d.Runs() != nil || d.Kids() != nil || d.Machine() != nil {
				t.Fatal("freeze cleanup")
			}
			o.Context = nil
			if s, err := d.SnapshotWith(o); s == nil || err != nil {
				t.Fatal("retry", err)
			}
		}
	})
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
		prog := mustCompile(t, fixture.grammar)
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

func TestSnapshotTracePanicCleanup(t *testing.T) {
	prog := mustCompile(t, `rule main = x $$
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
		if d.Memo().Len() != 0 || d.Runs() != nil || d.Kids() != nil || d.Machine() != nil {
			t.Fatal("trace panic retained reusable state")
		}
		o.Trace = nil
		s, err := d.SnapshotWith(o)
		if s == nil || err != nil || resultJSON(s.Materialize(), s.Error()) != resultJSON(prog.ParseWith("main", d.Text(), o)) {
			t.Fatal("trace panic retry", err)
		}
	})
}
