package engine

import (
	"errors"
	"strings"
	"testing"
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
	p := compile(t, `rule main = item+ #stream $$
rule item = (("ab" #recover(skip="X!") "z") / "X!a")`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				calls, elements := 0, 0
				err := p.ParseStreamWith("main", strings.NewReader("X!a"), func(*Node) error {
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
	p := compile(t, `rule main = item+ #stream $$
rule item = ("a" #recover(skip="x")) ("b" #recover(skip="y"))`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		t.Run(backend.String(), func(t *testing.T) {
			var events []string
			err := p.ParseStreamWith("main", strings.NewReader("xy"), func(*Node) error {
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
	p := &parser{recovered: []*SyntaxError{original}, diagnostic: func(e *SyntaxError) error {
		e.Pos = 10
		e.Expected[0] = "after"
		e.Messages[0] = "after"
		return nil
	}}
	if err := p.reportRecovered(); err != nil {
		t.Fatal(err)
	}
	if original.Pos != 3 || original.Expected[0] != "before" || original.Messages[0] != "message" {
		t.Fatalf("callback changed parser-owned error: %+v", original)
	}
}

func TestStreamDiagnosticCallbackAfterFinalElement(t *testing.T) {
	p := compile(t, `rule main = "a"* #stream ("b" #recover(skip="x")) $$`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		t.Run(backend.String(), func(t *testing.T) {
			var events []string
			err := p.ParseStreamWith("main", strings.NewReader("ax"), func(*Node) error {
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
	p := compile(t, `rule main = item+ #stream $$
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
	p := compile(t, `rule main = item+ #stream $$
rule item = "ab" #recover(skip=[^a]+)`)
	sentinel := errors.New("caller panic")
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		t.Run(backend.String(), func(t *testing.T) {
			defer func() {
				if got := recover(); got != sentinel {
					t.Fatalf("callback panic changed: %v", got)
				}
			}()
			_ = p.ParseStreamWith("main", strings.NewReader("abX!ab"), func(*Node) error { return nil }, ParseOptions{
				Backend:            backend,
				DiagnosticCallback: func(*SyntaxError) error { panic(sentinel) },
			})
		})
	}
}
