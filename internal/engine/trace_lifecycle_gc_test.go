package engine

import (
	"fmt"
	"io"
	"runtime"
	"strings"
	"testing"
	"unsafe"
	"weak"

	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/value"
)

type traceLifecycleSentinel struct{ value [128]byte }

//go:noinline
func traceLifecycleTouch(s *traceLifecycleSentinel) byte { return s.value[0] }

type traceLifecycleReader struct {
	text string
	pos  int
	pad  [128]byte
}

func (r *traceLifecycleReader) Read(b []byte) (int, error) {
	if r.pos == len(r.text) {
		return 0, io.EOF
	}
	n := copy(b, r.text[r.pos:])
	r.pos += n
	return n, nil
}

//go:noinline
func traceLifecycleDocument(t *testing.T, p *Program, b Backend, u PositionUnit) (TraceEvent, weak.Pointer[byte], weak.Pointer[traceLifecycleSentinel]) {
	s := &traceLifecycleSentinel{}
	s.value[0] = 42
	text := strings.Repeat("a", 4096)
	sr := weak.Make(s)
	ir := weak.Make(unsafe.StringData(text))
	runtime.GC()
	if sr.Value() == nil || ir.Value() == nil {
		t.Fatal("positive control failed")
	}
	var e TraceEvent
	d, err := p.NewDocumentWith("main", text, ParseOptions{Backend: b, Unit: u, Trace: func(v TraceEvent) {
		if traceLifecycleTouch(s) != 42 {
			panic("sentinel")
		}
		if v.Kind == TraceEnter && v.Rule == "main" {
			e = v
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := d.Parse(); n == nil || err != nil {
		t.Fatalf("parse %v", err)
	}
	runtime.KeepAlive(text)
	runtime.KeepAlive(s)
	runtime.KeepAlive(d)
	return e, ir, sr
}

//go:noinline
func traceLifecycleStream(t *testing.T, p *Program, b Backend, u PositionUnit) (TraceEvent, weak.Pointer[traceLifecycleReader], weak.Pointer[traceLifecycleSentinel], *value.Node) {
	s := &traceLifecycleSentinel{}
	s.value[0] = 42
	r := &traceLifecycleReader{text: "#records\na=1\nbc=22\n"}
	sr := weak.Make(s)
	rr := weak.Make(r)
	runtime.GC()
	if sr.Value() == nil || rr.Value() == nil {
		t.Fatal("positive control failed")
	}
	var e TraceEvent
	var held *value.Node
	err := p.ParseStreamWith("main", r, func(n *value.Node) error {
		if held == nil {
			held = n
		}
		return nil
	}, ParseOptions{Backend: b, Unit: u, Trace: func(v TraceEvent) {
		if traceLifecycleTouch(s) != 42 {
			panic("sentinel")
		}
		if v.Kind == TraceEnter && v.Rule == "main" {
			e = v
		}
	}})
	if err != nil || held == nil {
		t.Fatalf("stream %v", err)
	}
	runtime.KeepAlive(r)
	runtime.KeepAlive(s)
	return e, rr, sr, held
}
func TestTraceLifecycleReleasesOwnersAfterGC(t *testing.T) {
	t.Parallel()
	doc := mustCompile(t, `rule main = "a"* $$`)
	stream := mustCompile(t, gentest.Records)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, u := range []PositionUnit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("document/%s/%s", b, u), func(t *testing.T) {
				e, ir, sr := traceLifecycleDocument(t, doc, b, u)
				for range 4 {
					runtime.GC()
				}
				if ir.Value() != nil {
					t.Error("document event retains original input")
				}
				if sr.Value() != nil {
					t.Error("document event retains callback sentinel")
				}
				runtime.KeepAlive(e)
				runtime.KeepAlive(doc)
			})
			t.Run(fmt.Sprintf("stream/%s/%s", b, u), func(t *testing.T) {
				e, rr, sr, n := traceLifecycleStream(t, stream, b, u)
				before := n.String()
				for range 4 {
					runtime.GC()
				}
				if rr.Value() != nil {
					t.Error("stream event retains reader")
				}
				if sr.Value() != nil {
					t.Error("stream event retains callback sentinel")
				}
				if before != n.String() || value.D(n.Field("k").(*value.Node)).Text != "a" {
					t.Fatal("emitted node changed")
				}
				runtime.KeepAlive(e)
				runtime.KeepAlive(stream)
				runtime.KeepAlive(n)
			})
		}
	}
}
