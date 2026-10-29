package engine

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/value"
)

type streamRuntimePanicToken struct{ phase string }

func (p *streamRuntimePanicToken) Error() string { return "caller panic: " + p.phase }
func (*streamRuntimePanicToken) RuntimeError()   {}

type streamCallbackReader struct {
	text       string
	offset     int
	panicValue any
	err        error
	onRead     func()
	closed     bool
}

func (r *streamCallbackReader) Read(b []byte) (int, error) {
	if r.onRead != nil {
		f := r.onRead
		r.onRead = nil
		f()
	}
	if r.offset < len(r.text) {
		n := copy(b, r.text[r.offset:])
		r.offset += n
		return n, nil
	}
	if r.panicValue != nil {
		panic(r.panicValue)
	}
	if r.err != nil {
		return 0, r.err
	}
	return 0, io.EOF
}
func (r *streamCallbackReader) Close() error { r.closed = true; return nil }
func catchStreamPanic(f func()) (val any)    { defer func() { val = recover() }(); f(); return }

func TestStreamCallerPanicIdentity(t *testing.T) {
	prog := mustCompile(t, records)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, u := range []PositionUnit{CodePoints, Bytes} {
			for _, traced := range []bool{false, true} {
				for _, buffered := range []bool{false, true} {
					for _, phase := range []string{"reader", "emit"} {
						for _, runtimeValue := range []bool{false, true} {
							t.Run(fmt.Sprintf("%s/%s/trace=%t/buffered=%t/%s/runtime=%t", b, u, traced, buffered, phase, runtimeValue), func(t *testing.T) {
								var token any = &struct{ phase string }{phase}
								if runtimeValue {
									token = &streamRuntimePanicToken{phase}
								}
								r := &streamCallbackReader{text: "#records\na=1\nbc=22\n"}
								if phase == "reader" {
									r.panicValue = token
								}
								var reader io.Reader = r
								if buffered {
									reader = bufio.NewReader(r)
								}
								opts := ParseOptions{Backend: b, Unit: u}
								var event TraceEvent
								if traced {
									opts.Trace = func(e TraceEvent) {
										e.LineCol(e.Pos)
										e.Text()
										e.Failure()
										if e.Kind == TraceEnter && e.Rule == "main" {
											event = e
										}
									}
								}
								var err error
								var nodes []*value.Node
								var before []string
								got := catchStreamPanic(func() {
									err = prog.ParseStreamWith("main", reader, func(n *value.Node) error {
										nodes = append(nodes, n)
										before = append(before, resultJSON(n, nil))
										if phase == "emit" && len(nodes) == 2 {
											panic(token)
										}
										return nil
									}, opts)
								})
								if got != token || err != nil || len(nodes) != 2 {
									t.Fatalf("panic=%#v error=%v emitted=%d; want exact caller panic and two records", got, err, len(nodes))
								}
								if r.closed {
									t.Fatal("caller reader was closed")
								}
								if traced && (event.Rule != "main" || event.Depth != 1 || event.Pos != 0) {
									t.Fatal("saved event metadata changed")
								}
								if value.D(nodes[0].Field("k").(*value.Node)).Text != "a" || value.D(nodes[1].Field("v").(*value.Node)).Text != "22" {
									t.Fatal("delivered fields changed")
								}
								retryCount := 0
								if err := prog.ParseStreamWith("main", strings.NewReader("#records\nxy=3\n"), func(*value.Node) error { retryCount++; return nil }, ParseOptions{Backend: b, Unit: u}); err != nil || retryCount != 1 {
									t.Fatalf("retry error=%v emitted=%d", err, retryCount)
								}
								for i, n := range nodes {
									if resultJSON(n, nil) != before[i] {
										t.Fatal("panic or retry changed a delivered prefix")
									}
								}
							})
						}
					}
				}
			}
		}
	}
}

func TestStreamCallerReturnedErrorIdentity(t *testing.T) {
	prog := mustCompile(t, records)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, u := range []PositionUnit{CodePoints, Bytes} {
			for _, traced := range []bool{false, true} {
				for _, phase := range []string{"reader", "emit"} {
					t.Run(fmt.Sprintf("%s/%s/trace=%t/%s", b, u, traced, phase), func(t *testing.T) {
						stop := errors.New("caller stopped")
						r := &streamCallbackReader{text: "#records\na=1\nbc=22\n"}
						if phase == "reader" {
							r.err = stop
						}
						opts := ParseOptions{Backend: b, Unit: u}
						if traced {
							opts.Trace = func(e TraceEvent) { e.LineCol(e.Pos) }
						}
						count := 0
						var err error
						got := catchStreamPanic(func() {
							err = prog.ParseStreamWith("main", r, func(*value.Node) error {
								count++
								if phase == "emit" && count == 2 {
									return stop
								}
								return nil
							}, opts)
						})
						if got != nil || err != stop || !errors.Is(err, stop) || count != 2 || r.closed {
							t.Fatalf("panic=%v error=%v count=%d closed=%t", got, err, count, r.closed)
						}
					})
				}
			}
		}
	}
}

func TestStreamReaderPanicInsideTraceKeepsOriginalValue(t *testing.T) {
	prog := mustCompile(t, records)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, u := range []PositionUnit{CodePoints, Bytes} {
			t.Run(b.String()+"/"+u.String(), func(t *testing.T) {
				token := &streamRuntimePanicToken{"nested reader"}
				r := &streamCallbackReader{panicValue: token}
				var err error
				got := catchStreamPanic(func() {
					err = prog.ParseStreamWith("main", r, nil, ParseOptions{Backend: b, Unit: u, Trace: func(e TraceEvent) {
						if e.Kind == TraceEnter && e.Rule == "main" {
							e.p.fill(1)
						}
					}})
				})
				if got != token || err != nil {
					t.Fatalf("panic=%#v error=%v", got, err)
				}
			})
		}
	}
}

func TestStreamNormalCallerReturnKeepsVMFaultAttribution(t *testing.T) {
	for _, b := range []Backend{Bytecode, BytecodeIterative} {
		for _, u := range []PositionUnit{CodePoints, Bytes} {
			for _, phase := range []string{"reader", "emit"} {
				t.Run(fmt.Sprintf("%s/%s/%s", b, u, phase), func(t *testing.T) {
					prog := mustCompile(t, "rule main = item* #stream $$\nrule item = \"a\"")
					if _, err := prog.rule(b, "main"); err != nil {
						t.Fatal(err)
					}
					vm := prog.vmFor(b)
					// Damage runtime lookup state after a normal caller return. This fault is
					// engine-owned and must retain the VM's invalid-module classification.
					damage := func() { vm.runes = nil; vm.bytes = nil; vm.literals = nil }
					r := &streamCallbackReader{text: "aa"}
					if phase == "reader" {
						r.onRead = damage
					}
					count := 0
					var err error
					got := catchStreamPanic(func() {
						err = prog.ParseStreamWith("main", r, func(*value.Node) error {
							count++
							if phase == "emit" {
								damage()
							}
							return nil
						}, ParseOptions{Backend: b, Unit: u})
					})
					if got != nil || err == nil || !strings.Contains(err.Error(), "invalid bytecode:") {
						t.Fatalf("panic=%#v error=%v", got, err)
					}
					if phase == "emit" && count != 1 {
						t.Fatalf("emitted %d before VM fault", count)
					}
				})
			}
		}
	}
}

func TestStreamKeepsCallerBufferedReader(t *testing.T) {
	prog := mustCompile(t, records)
	for _, u := range []PositionUnit{CodePoints, Bytes} {
		reader := bufio.NewReader(strings.NewReader("#records\na=1\n"))
		if p := newStreamParser(&prog.sharedProgram, reader, u); p.reader != reader {
			t.Fatal("stream parser replaced caller's buffered reader")
		}
	}
}
