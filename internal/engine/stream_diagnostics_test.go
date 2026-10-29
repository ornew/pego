package engine

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/value"
)

type diagnosticBoundaryReader struct {
	sent bool
	late error
}

func (r *diagnosticBoundaryReader) Read(dst []byte) (int, error) {
	if r.sent {
		return 0, r.late
	}
	r.sent = true
	return copy(dst, "abX!ab"), nil
}

func TestStreamDiagnosticCallbackSkipsSpeculativeRecovery(t *testing.T) {
	p := mustCompile(t, `rule main = item+ #stream $$
rule item = (("ab" #recover(skip="X!") "z") / "X!a")`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				calls, elements := 0, 0
				err := p.ParseStreamWith("main", strings.NewReader("X!a"), func(*value.Node) error {
					elements++
					return nil
				}, ParseOptions{Backend: backend, Unit: unit, DiagnosticCallback: func(*SyntaxError) error {
					calls++
					return nil
				}})
				if err != nil || elements != 1 || calls != 0 {
					t.Fatalf("speculative recovery escaped: elements=%d diagnostics=%d err=%v", elements, calls, err)
				}
			})
		}
	}
}

func TestStreamDiagnosticCallbackReportsAllElementRecoveriesBeforeEmit(t *testing.T) {
	p := mustCompile(t, `rule main = item+ #stream $$
rule item = ("a" #recover(skip="x")) ("b" #recover(skip="y"))`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		t.Run(backend.String(), func(t *testing.T) {
			var events []string
			err := p.ParseStreamWith("main", strings.NewReader("xy"), func(*value.Node) error {
				events = append(events, "element")
				return nil
			}, ParseOptions{Backend: backend, DiagnosticCallback: func(*SyntaxError) error {
				events = append(events, "diagnostic")
				return nil
			}})
			var recovered SyntaxErrors
			if !errors.As(err, &recovered) || len(recovered) != 2 || strings.Join(events, ",") != "diagnostic,diagnostic,element" {
				t.Fatalf("multiple recoveries: events=%v err=%v", events, err)
			}
		})
	}
}

func TestStreamDiagnosticCallbackCopiesErrorSlices(t *testing.T) {
	original := &SyntaxError{Pos: 3, Expected: []string{"before"}, Messages: []string{"message"}}
	p := &core.Parser{Recovered: []*SyntaxError{original}, Diagnostic: func(e *SyntaxError) error {
		e.Pos = 10
		e.Expected[0] = "after"
		e.Messages[0] = "after"
		return nil
	}}
	if err := p.ReportRecovered(); err != nil {
		t.Fatal(err)
	}
	if original.Pos != 3 || original.Expected[0] != "before" || original.Messages[0] != "message" {
		t.Fatalf("callback changed parser-owned error: %+v", original)
	}
}

func TestStreamDiagnosticCallbackAfterFinalElement(t *testing.T) {
	p := mustCompile(t, `rule main = "a"* #stream ("b" #recover(skip="x")) $$`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		t.Run(backend.String(), func(t *testing.T) {
			var events []string
			err := p.ParseStreamWith("main", strings.NewReader("ax"), func(*value.Node) error {
				events = append(events, "element")
				return nil
			}, ParseOptions{Backend: backend, DiagnosticCallback: func(*SyntaxError) error {
				events = append(events, "diagnostic")
				return nil
			}})
			var recovered SyntaxErrors
			if !errors.As(err, &recovered) || len(recovered) != 1 || strings.Join(events, ",") != "element,diagnostic" {
				t.Fatalf("suffix recovery: events=%v err=%v", events, err)
			}
		})
	}
}

func TestStreamDiagnosticCallbackWithoutElementReceiverStopsBeforeLaterRead(t *testing.T) {
	p := mustCompile(t, `rule main = item+ #stream $$
rule item = "ab" #recover(skip=[^a]+)`)
	stop := errors.New("diagnostic stop")
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		t.Run(backend.String(), func(t *testing.T) {
			r := &diagnosticBoundaryReader{late: errors.New("reader continued")}
			err := p.ParseStreamWith("main", r, nil, ParseOptions{Backend: backend, DiagnosticCallback: func(*SyntaxError) error { return stop }})
			if err != stop {
				t.Fatalf("diagnostic was delayed until after another read: %v", err)
			}
		})
	}
}

func TestStreamDiagnosticCallbackPanicPropagates(t *testing.T) {
	p := mustCompile(t, `rule main = item+ #stream $$
rule item = "ab" #recover(skip=[^a]+)`)
	sentinel := errors.New("caller panic")
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		t.Run(backend.String(), func(t *testing.T) {
			defer func() {
				if got := recover(); got != sentinel {
					t.Fatalf("callback panic changed: %v", got)
				}
			}()
			_ = p.ParseStreamWith("main", strings.NewReader("abX!ab"), func(*value.Node) error { return nil }, ParseOptions{
				Backend:            backend,
				DiagnosticCallback: func(*SyntaxError) error { panic(sentinel) },
			})
		})
	}
}

func TestStreamDiagnosticTailSkipsSpeculativeRecovery(t *testing.T) {
	p := mustCompile(t, `rule main = item+ #stream $$
rule item = (("ab" #recover(skip="X!") "z") / "X!a")`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		calls := 0
		err := p.ParseStreamWith("main", strings.NewReader("X!a"), nil, ParseOptions{
			Backend: backend, DiagnosticLimit: 1, DiagnosticCallback: func(*SyntaxError) error {
				calls++
				return nil
			},
		})
		if err != nil || calls != 0 {
			t.Fatalf("%v speculative recovery escaped: callbacks=%d err=%v", backend, calls, err)
		}
	}
}

func TestStreamDiagnosticTailLongInput(t *testing.T) {
	p := mustCompile(t, `rule main = item+ #stream $$
rule item = "ab" #recover(skip=[^a]+)`)
	const count = 1000
	input := strings.Repeat("abX!", count)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			o := ParseOptions{Backend: backend, Unit: unit, DiagnosticLimit: 3}
			callbacks, elements := 0, 0
			maxWorking := 0
			var rootRecovered []*SyntaxError
			var firstItemRecovered []*SyntaxError
			o.DiagnosticCallback = func(*SyntaxError) error { callbacks++; return nil }
			o.Trace = func(e TraceEvent) {
				if e.Kind == TraceExit && e.Rule == "item" && len(core.TraceParser(e).Recovered) > maxWorking {
					maxWorking = len(core.TraceParser(e).Recovered)
				}
				if e.Kind == TraceExit && e.Rule == "item" && firstItemRecovered == nil && len(e.Recovered()) > 0 {
					firstItemRecovered = e.Recovered()
				}
				if e.Kind == TraceExit && e.Rule == "main" {
					rootRecovered = e.Recovered()
				}
			}
			err := p.ParseStreamWith("main", strings.NewReader(input), func(*value.Node) error { elements++; return nil }, o)
			var truncated *TruncatedSyntaxErrors
			if !errors.As(err, &truncated) || truncated.Total != count || truncated.Dropped != count-3 ||
				len(truncated.Errors) != 3 || callbacks != count || elements != 2*count || maxWorking > 1 {
				t.Fatalf("%v/%v tail: callbacks=%d elements=%d working=%d err=%v", backend, unit, callbacks, elements, maxWorking, err)
			}
			for i, e := range truncated.Errors {
				if e.Pos != (count-3+i)*4+2 {
					t.Fatalf("%v/%v tail[%d] position=%d", backend, unit, i, e.Pos)
				}
			}
			if !reflect.DeepEqual(rootRecovered, []*SyntaxError(truncated.Errors)) {
				t.Fatalf("%v/%v root trace tail=%v, terminal tail=%v", backend, unit, rootRecovered, truncated.Errors)
			}
			if len(firstItemRecovered) != 1 || firstItemRecovered[0].Pos != 2 {
				t.Fatalf("%v/%v retained item trace changed: %v", backend, unit, firstItemRecovered)
			}
		}
	}
}

func TestStreamDiagnosticTailSaturatesCounts(t *testing.T) {
	tail := core.StreamDiagnosticTail{Limit: 1, Total: ^uint64(0) - 1, Dropped: ^uint64(0) - 1}
	tail.Add(&SyntaxError{Pos: 1})
	tail.Add(&SyntaxError{Pos: 2})
	tail.Add(&SyntaxError{Pos: 3})
	tail.Add(&SyntaxError{Pos: 4})
	result, ok := tail.Result(&core.Parser{}).(*TruncatedSyntaxErrors)
	if !ok || result.Total != ^uint64(0) || result.Dropped != ^uint64(0) ||
		!result.CountsSaturated || len(result.Errors) != 1 || result.Errors[0].Pos != 4 {
		t.Fatalf("saturated counts: %+v", result)
	}
}

func TestStreamDiagnosticTailPreviewSuffix(t *testing.T) {
	tail := core.StreamDiagnosticTail{Limit: 2}
	for pos := range 3 {
		tail.Add(&SyntaxError{Pos: pos})
	}
	suffix := []*SyntaxError{{Pos: 3}, {Pos: 4}}
	preview := tail.Preview(&core.Parser{}, suffix)
	if len(preview) != 2 || preview[0].Pos != 3 || preview[1].Pos != 4 {
		t.Fatalf("suffix preview: %v", preview)
	}
	if got := tail.Result(&core.Parser{}).(*TruncatedSyntaxErrors); got.Errors[0].Pos != 1 || got.Errors[1].Pos != 2 {
		t.Fatalf("preview changed collector: %v", got.Errors)
	}
}

func TestStreamDiagnosticTailPrefixRecovery(t *testing.T) {
	p := mustCompile(t, `rule main = ("h" #recover(skip="!")) item+ #stream $$
rule item = "a" #recover(skip="!")`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		calls, elements := 0, 0
		err := p.ParseStreamWith("main", strings.NewReader("!!"), func(*value.Node) error {
			elements++
			return nil
		}, ParseOptions{Backend: backend, DiagnosticLimit: 1, DiagnosticCallback: func(*SyntaxError) error {
			calls++
			return nil
		}})
		var truncated *TruncatedSyntaxErrors
		if !errors.As(err, &truncated) || truncated.Total != 2 || truncated.Dropped != 1 ||
			calls != 2 || elements != 1 {
			t.Fatalf("%v prefix recovery: calls=%d elements=%d err=%v", backend, calls, elements, err)
		}
	}
}

func TestStreamDiagnosticTailCommitScratchOwnership(t *testing.T) {
	prog := mustCompile(t, `rule main = "a"`)
	for _, traced := range []bool{false, true} {
		p := core.NewParser(&prog.sharedProgram, "a", CodePoints, false)
		p.DiagnosticTail.Limit = 1
		if traced {
			p.Tr = &core.Tracer{}
		}
		current := &SyntaxError{Pos: 7}
		stale := &SyntaxError{Pos: 8}
		p.Recovered = make([]*SyntaxError, 8)
		p.Recovered[0] = current
		for i := 1; i < 8; i++ {
			p.Recovered[i] = stale
		}
		p.Recovered = p.Recovered[:1]
		p.DiagnosticTail.Add(current)
		backing := &p.Recovered[0]
		held := p.Recovered[:1:1]
		p.Commit(1)
		if p.DiagnosticTail.Errors[0] != current || p.Diagnosed != 0 {
			t.Fatalf("traced=%v lost retained diagnostic", traced)
		}
		if traced {
			if p.Recovered != nil || held[0] != current {
				t.Fatalf("trace snapshot changed: recovered=%v held=%v", p.Recovered, held)
			}
			continue
		}
		if len(p.Recovered) != 0 || cap(p.Recovered) != 8 {
			t.Fatalf("scratch not reused: len=%d cap=%d", len(p.Recovered), cap(p.Recovered))
		}
		for i, e := range p.Recovered[:cap(p.Recovered)] {
			if e != nil {
				t.Fatalf("scratch retained stale diagnostic at %d", i)
			}
		}
		p.Recovered = append(p.Recovered, &SyntaxError{Pos: 9})
		if &p.Recovered[0] != backing || p.DiagnosticTail.Errors[0] != current {
			t.Fatal("scratch reuse changed collector ownership")
		}
	}
}
