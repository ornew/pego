package engine

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/engine/core"
	"github.com/ornew/zeroparse/internal/gentest"
	"github.com/ornew/zeroparse/internal/value"
)

func assertTraceParserDetached(t *testing.T, p *core.Parser, aborted bool) {
	t.Helper()
	if p == nil {
		t.Fatal("trace callback did not expose a parser")
	}
	if p.Aborted != aborted {
		t.Fatalf("aborted=%t want %t", p.Aborted, aborted)
	}
	var inspect func(reflect.Value, string)
	inspect = func(v reflect.Value, path string) {
		switch v.Kind() {
		case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface, reflect.UnsafePointer:
			if !v.IsNil() {
				t.Errorf("finished parser retains %s (%s)", path, v.Kind())
			}
		case reflect.String:
			if v.Len() != 0 {
				t.Errorf("finished parser retains %s (string)", path)
			}
		case reflect.Struct:
			for i := range v.NumField() {
				inspect(v.Field(i), path+"."+v.Type().Field(i).Name)
			}
		case reflect.Array:
			for i := range v.Len() {
				inspect(v.Index(i), fmt.Sprintf("%s[%d]", path, i))
			}
		}
	}
	inspect(reflect.ValueOf(*p), "parser")
}

func traceLifecycleRecover(f func()) (got any) { defer func() { got = recover() }(); f(); return }

type traceLifecycleClosingReader struct {
	io.Reader
	closed bool
}

func (r *traceLifecycleClosingReader) Close() error { r.closed = true; return nil }

func TestTraceLifecycleStreamDetachment(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, gentest.Records)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, u := range []PositionUnit{CodePoints, Bytes} {
			for _, outcome := range []string{"success", "syntax", "reader-error", "emit-error", "trace-panic", "emit-panic"} {
				t.Run(fmt.Sprintf("%s/%s/%s", b, u, outcome), func(t *testing.T) {
					stop := errors.New("stream stopped")
					token := &struct{ value string }{"caller panic"}
					text := "#records\na=1\nbc=22\n"
					var reader io.Reader = strings.NewReader(text)
					if outcome == "syntax" {
						reader = strings.NewReader("#records\na=1\nbad=x\n")
					}
					if outcome == "reader-error" {
						reader = io.MultiReader(strings.NewReader("#records\na=1\nbad="), &failOnRead{err: stop})
					}
					r := &traceLifecycleClosingReader{Reader: reader}
					var e TraceEvent
					var nodes []*value.Node
					var snapshots []string
					err := error(nil)
					got := traceLifecycleRecover(func() {
						err = prog.ParseStreamWith("main", r, func(n *value.Node) error {
							nodes = append(nodes, n)
							snapshots = append(snapshots, gentest.ResultJSON(n, nil))
							if outcome == "emit-error" {
								return stop
							}
							if outcome == "emit-panic" {
								panic(token)
							}
							return nil
						}, ParseOptions{Backend: b, Unit: u, Trace: func(v TraceEvent) {
							v.LineCol(v.Pos)
							v.Text()
							v.Failure()
							if v.Kind == TraceEnter && v.Rule == "main" {
								e = v
							}
							if outcome == "trace-panic" && v.Kind == TraceEnter && v.Rule == "record" && v.Pos > 9 {
								panic(token)
							}
						}})
					})
					switch outcome {
					case "success":
						if got != nil || err != nil || len(nodes) != 2 {
							t.Fatalf("err=%v panic=%v nodes=%d", err, got, len(nodes))
						}
					case "syntax":
						var se *SyntaxError
						if got != nil || !errors.As(err, &se) || len(nodes) != 1 {
							t.Fatalf("err=%v panic=%v nodes=%d", err, got, len(nodes))
						}
					case "reader-error", "emit-error":
						if got != nil || !errors.Is(err, stop) || len(nodes) != 1 {
							t.Fatalf("err=%v panic=%v nodes=%d", err, got, len(nodes))
						}
					default:
						if got != token || err != nil || len(nodes) != 1 {
							t.Fatalf("err=%v panic=%v nodes=%d", err, got, len(nodes))
						}
					}
					assertTraceParserDetached(t, core.TraceParser(e), false) // Stream has no Document invalidation flag.
					if e.Rule != "main" || e.Depth != 1 || e.Pos != 0 {
						t.Fatal("saved stream metadata changed")
					}
					if r.closed {
						t.Fatal("parser closed caller's reader")
					}
					if value.D(nodes[0].Field("k").(*value.Node)).Text != "a" || value.D(nodes[0].Field("v").(*value.Node)).Text != "1" {
						t.Fatal("emitted fields changed")
					}
					if err := prog.ParseStreamWith("main", strings.NewReader("#records\nxy=3\n"), func(*value.Node) error { return nil }, ParseOptions{Backend: b, Unit: u}); err != nil {
						t.Fatal(err)
					}
					for i, n := range nodes {
						if gentest.ResultJSON(n, nil) != snapshots[i] {
							t.Fatal("teardown/retry changed an emitted prefix")
						}
					}
				})
			}
		}
	}
}
