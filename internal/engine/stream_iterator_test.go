package engine

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/value"
)

type demandReader struct {
	reader io.Reader
	calls  int
}

func (r *demandReader) Read(p []byte) (int, error) { r.calls++; return r.reader.Read(p) }

func TestStreamIteratorParityDemandAndTermination(t *testing.T) {
	t.Parallel()
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, u := range []PositionUnit{CodePoints, Bytes} {
			t.Run(b.String()+"/"+u.String(), func(t *testing.T) {
				p := mustCompile(t, `rule main = item* #stream $$
rule item = "あ" / "b"`)
				o := ParseOptions{Backend: b, Unit: u}
				var want []string
				if err := p.ParseStreamWith("main", strings.NewReader("あbあ"), func(n *value.Node) error { want = append(want, dump(t, n, nil)); return nil }, o); err != nil {
					t.Fatal(err)
				}
				r := &demandReader{reader: strings.NewReader("あbあ")}
				it, err := p.Iterate("main", r, o)
				if err != nil {
					t.Fatal(err)
				}
				if r.calls != 0 {
					t.Fatal("Iterate read before demand")
				}
				for _, w := range want {
					n, ok, err := it.Next()
					if err != nil || !ok || dump(t, n, nil) != w {
						t.Fatalf("Next: %v %v %v", n, ok, err)
					}
				}
				for range 2 {
					if n, ok, err := it.Next(); n != nil || ok || err != nil {
						t.Fatalf("EOF: %v %v %v", n, ok, err)
					}
				}
				it.Close()
				if _, ok, err := it.Next(); ok || err != nil {
					t.Fatalf("Close changed EOF: %v %v", ok, err)
				}
			})
		}
	}
}

func TestStreamIteratorNilEmptyCloseAndError(t *testing.T) {
	t.Parallel()
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		p := mustCompile(t, `rule main = (-"a")* #stream $$`)
		it, err := p.Iterate("main", strings.NewReader("aa"), ParseOptions{Backend: b})
		if err != nil {
			t.Fatal(err)
		}
		// An element without a value is the unit tuple of its range.
		if n, ok, err := it.Next(); n == nil || value.ShapeOf(n) != value.ShapeTuple || len(value.D(n).Children) != 0 || !ok || err != nil {
			t.Fatalf("unit element: %v %v %v", n, ok, err)
		}
		it.Close()
		it.Close()
		if _, ok, err := it.Next(); ok || err != ErrStreamClosed {
			t.Fatalf("closed: %v %v", ok, err)
		}
		it, err = p.Iterate("main", strings.NewReader("a!"), ParseOptions{Backend: b})
		if err != nil {
			t.Fatal(err)
		}
		it.Next()
		_, ok, terminal := it.Next()
		if ok || terminal == nil {
			t.Fatal("missing terminal error")
		}
		it.Close()
		_, ok, again := it.Next()
		if ok || again != terminal {
			t.Fatal("unstable terminal error")
		}
		p = mustCompile(t, `rule main = ""* #stream $$`)
		it, err = p.Iterate("main", strings.NewReader(""), ParseOptions{Backend: b})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok, err := it.Next(); !ok || err != nil {
			t.Fatal("empty element not emitted")
		}
		if _, ok, err := it.Next(); ok || err != nil {
			t.Fatal("empty repetition did not terminate")
		}
	}
}

func TestStreamIteratorCloseBeforeFirstNext(t *testing.T) {
	t.Parallel()
	p := mustCompile(t, `rule main = "a"* #stream $$`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				reader := &demandReader{reader: strings.NewReader("aaa")}
				traces := 0
				it, err := p.Iterate("main", reader, ParseOptions{Backend: backend, Unit: unit, Trace: func(TraceEvent) { traces++ }})
				if err != nil {
					t.Fatal(err)
				}
				it.Close()
				it.Close()
				if reader.calls != 0 || traces != 0 {
					t.Fatalf("Close started parsing: reads=%d traces=%d", reader.calls, traces)
				}
				for range 2 {
					if n, ok, err := it.Next(); n != nil || ok || err != ErrStreamClosed {
						t.Fatalf("Next after Close: node=%v ok=%v err=%v", n, ok, err)
					}
				}
			})
		}
	}
}

func TestStreamIteratorRecoveredErrorsMatchCallback(t *testing.T) {
	t.Parallel()
	p := mustCompile(t, `rule main = item+ #stream $$
rule item = "ab" @error(message="record") #recover(skip=[^a]+)`)
	const input = "abX!abY?ab"
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				options := ParseOptions{Backend: backend, Unit: unit}
				var wantNodes []string
				wantErr := p.ParseStreamWith("main", strings.NewReader(input), func(n *value.Node) error {
					wantNodes = append(wantNodes, dump(t, n, nil))
					return nil
				}, options)
				var wantRecovered SyntaxErrors
				if !errors.As(wantErr, &wantRecovered) || len(wantRecovered) != 2 || len(wantNodes) != 5 {
					t.Fatalf("callback fixture: nodes=%d err=%v", len(wantNodes), wantErr)
				}
				it, err := p.Iterate("main", strings.NewReader(input), options)
				if err != nil {
					t.Fatal(err)
				}
				var gotNodes []string
				var terminal error
				for {
					n, ok, err := it.Next()
					if !ok {
						if n != nil {
							t.Fatalf("terminal node: %v", n)
						}
						terminal = err
						break
					}
					if err != nil {
						t.Fatalf("element error: %v", err)
					}
					gotNodes = append(gotNodes, dump(t, n, nil))
				}
				var gotRecovered SyntaxErrors
				if !reflect.DeepEqual(gotNodes, wantNodes) || !errors.As(terminal, &gotRecovered) || !reflect.DeepEqual(gotRecovered, wantRecovered) {
					t.Fatalf("iterator differs: nodes=%v want=%v, error=%v want=%v", gotNodes, wantNodes, terminal, wantErr)
				}
				for range 2 {
					if n, ok, again := it.Next(); n != nil || ok {
						t.Fatalf("repeated terminal: node=%v ok=%v err=%v", n, ok, again)
					} else {
						var repeated SyntaxErrors
						if !errors.As(again, &repeated) || len(repeated) != len(gotRecovered) || &repeated[0] != &gotRecovered[0] {
							t.Fatalf("terminal error changed: %v", again)
						}
					}
				}
				it.Close()
				var afterClose SyntaxErrors
				if _, ok, err := it.Next(); ok || !errors.As(err, &afterClose) || &afterClose[0] != &gotRecovered[0] {
					t.Fatalf("Close changed recovered terminal: %v", err)
				}
			})
		}
	}
}

// Reader cancellation is owned by the caller. Iterator.Close is deliberately
// not invoked while Next is blocked, because concurrent ownership is invalid.
func TestStreamIteratorCallerCancelsBlockedReader(t *testing.T) {
	t.Parallel()
	p := mustCompile(t, `rule main = "a"* #stream $$`)
	r, w := io.Pipe()
	defer w.Close()
	it, err := p.Iterate("main", r, ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, _, err := it.Next(); done <- err }()
	if err := r.CloseWithError(io.ErrClosedPipe); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("caller cancellation: %v", err)
	}
	it.Close()
}
