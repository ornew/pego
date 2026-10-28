package engine

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"unsafe"
	"weak"
)

//go:noinline
func batchInputReleaseFixture(t *testing.T, prog *Program, backend Backend, unit PositionUnit, active bool) (weak.Pointer[byte], TraceEvent) {
	t.Helper()
	text := strings.Repeat("a", 256<<10)
	// Observe the original immutable string's backing allocation, rather than
	// the independently copied buffers that scratch is permitted to cache.
	ref := weak.Make(unsafe.StringData(text))
	runtime.GC()
	if ref.Value() == nil {
		t.Fatal("live input disappeared before parsing")
	}
	var event TraceEvent
	opts := ParseOptions{Backend: backend, Unit: unit, Recognize: true, Trace: func(e TraceEvent) {
		if e.Kind == TraceEnter && e.Rule == "main" {
			event = e
		}
	}}
	if active {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		opts.Context = ctx
	}
	n, err := prog.ParseWith("main", text, opts)
	if n != nil || err != nil || event.p == nil {
		t.Fatalf("recognition node=%v error=%v event parser=%p", n, err, event.p)
	}
	runtime.KeepAlive(text)
	return ref, event
}

func TestWorkBatchTraceEventReleasesOriginalInputAfterGC(t *testing.T) {
	prog := mustCompile(t, `rule main = "a"* $$`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			for _, active := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/active=%t", backend, unit, active), func(t *testing.T) {
					if active && !workControlsEnabled {
						t.Skip("active controls are unavailable in the reference build")
					}
					ref, event := batchInputReleaseFixture(t, prog, backend, unit, active)
					released := false
					for range 4 {
						runtime.GC()
						if ref.Value() == nil {
							released = true
							break
						}
					}
					if !released {
						t.Error("saved batch event still keeps original input reachable")
					}
					// The event and compiled program deliberately outlive collection.
					// No Node is returned, and expired event accessors are not called.
					runtime.KeepAlive(event)
					runtime.KeepAlive(prog)
				})
			}
		}
	}
}
