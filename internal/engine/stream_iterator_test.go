package engine

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type demandReader struct {
	reader io.Reader
	calls  int
}

func (r *demandReader) Read(p []byte) (int, error) { r.calls++; return r.reader.Read(p) }

func TestStreamIteratorParityDemandAndTermination(t *testing.T) {
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, u := range []Unit{CodePoints, Bytes} {
			t.Run(b.String()+"/"+u.String(), func(t *testing.T) {
				p := compile(t, `rule main = item* #stream $$
rule item = "あ" / "b"`)
				o := ParseOptions{Backend: b, Unit: u}
				var want []string
				if err := p.ParseStreamWith("main", strings.NewReader("あbあ"), func(n *Node) error { want = append(want, dump(t, n, nil)); return nil }, o); err != nil {
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
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		p := compile(t, `rule main = (-"a")* #stream $$`)
		it, err := p.Iterate("main", strings.NewReader("aa"), ParseOptions{Backend: b})
		if err != nil {
			t.Fatal(err)
		}
		if n, ok, err := it.Next(); n != nil || !ok || err != nil {
			t.Fatalf("nil element: %v %v %v", n, ok, err)
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
		p = compile(t, `rule main = ""* #stream $$`)
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

// Reader cancellation is owned by the caller. Iterator.Close is deliberately
// not invoked while Next is blocked, because concurrent ownership is invalid.
func TestStreamIteratorCallerCancelsBlockedReader(t *testing.T) {
	p := compile(t, `rule main = "a"* #stream $$`)
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
