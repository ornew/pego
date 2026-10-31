package apitest_test

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse"
)

type publicIteratorReader struct {
	*strings.Reader
	reads  int
	closed bool
}

func (r *publicIteratorReader) Read(p []byte) (int, error) {
	r.reads++
	return r.Reader.Read(p)
}

func (r *publicIteratorReader) Close() error {
	r.closed = true
	return nil
}

func TestPublicStreamIteratorLifecycle(t *testing.T) {
	p, err := compileTestSource(`rule main = ("é" / "a")* #stream $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []zp.Backend{zp.Closure, zp.Bytecode, zp.BytecodeIterative} {
		for _, unit := range []zp.PositionUnit{zp.CodePoints, zp.Bytes} {
			r := &publicIteratorReader{Reader: strings.NewReader("éa")}
			it, err := p.Iterate(r, zp.WithBackend(backend), zp.WithUnit(unit))
			if err != nil || r.reads != 0 {
				t.Fatalf("%s/%s eager construction: err=%v reads=%d", backend, unit, err, r.reads)
			}
			first, ok, err := it.Next()
			wantEnd := 1
			if unit == zp.Bytes {
				wantEnd = 2
			}
			if err != nil || !ok || first == nil || first.Start() != 0 || first.End() != wantEnd || r.reads == 0 {
				t.Fatalf("%s/%s first: node=%v ok=%v err=%v reads=%d", backend, unit, first, ok, err, r.reads)
			}
			second, ok, err := it.Next()
			if err != nil || !ok || second == nil || second.Start() != wantEnd || second.End() != wantEnd+1 {
				t.Fatalf("%s/%s second: node=%v ok=%v err=%v", backend, unit, second, ok, err)
			}
			for range 2 {
				if n, ok, err := it.Next(); n != nil || ok || err != nil {
					t.Fatalf("%s/%s terminal result: %v %v %v", backend, unit, n, ok, err)
				}
			}
			it.Close()
			if r.closed {
				t.Fatal("iterator closed caller-owned reader")
			}
		}
	}

	early, err := p.Iterate(strings.NewReader("éa"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := early.Next(); !ok || err != nil {
		t.Fatalf("early iterator first element: ok=%v err=%v", ok, err)
	}
	early.Close()
	early.Close()
	if n, ok, err := early.Next(); n != nil || ok || err != zp.ErrStreamClosed {
		t.Fatalf("early close: %v %v %v", n, ok, err)
	}

	failing, err := p.Iterate(strings.NewReader("é!"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := failing.Next(); !ok || err != nil {
		t.Fatalf("failing iterator first element: ok=%v err=%v", ok, err)
	}
	_, ok, terminal := failing.Next()
	if ok || terminal == nil {
		t.Fatal("missing terminal syntax error")
	}
	failing.Close()
	if n, ok, err := failing.Next(); n != nil || ok || err != terminal {
		t.Fatalf("unstable terminal error: %v %v %v", n, ok, err)
	}
}

func TestPublicStreamIteratorAdmission(t *testing.T) {
	plain, err := compileTestSource(`rule main = "a"* $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	r := &publicIteratorReader{Reader: strings.NewReader("a")}
	if it, err := plain.Iterate(r); it != nil || err == nil || r.reads != 0 {
		t.Fatalf("non-stream admission: it=%v err=%v reads=%d", it, err, r.reads)
	}
	stream, err := compileTestSource(`rule main = "a"* #stream $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	if it, err := stream.Iterate(strings.NewReader("a"), zp.RecognizeOnly()); it != nil || err == nil {
		t.Fatalf("recognition admission: it=%v err=%v", it, err)
	}
	if it, err := stream.Iterate(strings.NewReader("a"), zp.WithBackend(zp.Backend(99))); it != nil || err == nil {
		t.Fatalf("unknown backend admission: it=%v err=%v", it, err)
	}
}
