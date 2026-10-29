package engine

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/value"
)

// snapshotPhaseContext changes state only while the freezer is on the stack.
// This separates a parse-time cancellation from a freeze-time cancellation.
type snapshotPhaseContext struct {
	context.Context
	done       chan struct{}
	seen       bool
	panicValue any
	phase      string
}

func (c *snapshotPhaseContext) Done() <-chan struct{} { return c.done }
func (c *snapshotPhaseContext) Err() error {
	var callers [24]uintptr
	count := runtime.Callers(2, callers[:])
	frames := runtime.CallersFrames(callers[:count])
	for {
		f, more := frames.Next()
		phase := c.phase
		if phase == "" {
			phase = "freezeSnapshotGraph"
		}
		if strings.Contains(f.Function, phase) {
			c.seen = true
			if c.panicValue != nil {
				panic(c.panicValue)
			}
			return context.Canceled
		}
		if !more {
			break
		}
	}
	return nil
}

func TestSnapshotUnsupportedLegacyFieldPreservesParseError(t *testing.T) {
	prog := mustCompile(t, `rule main = "x"`)
	d, _ := prog.NewDocument("main", "x")
	legacy, err := d.Parse()
	if err != nil {
		t.Fatal(err)
	}
	value.D(legacy).Fields = value.Fields{{Name: "bad", Value: []int{1}}}
	s, err := d.SnapshotWith(ParseOptions{})
	var capture *SnapshotCaptureError
	if s != nil || !errors.As(err, &capture) || capture.ValueType != reflect.TypeOf([]int{}) || !strings.Contains(capture.Path, "bad") || capture.ParseError != nil {
		t.Fatal("unsupported legacy field", s, err)
	}
	if d.Text() != "x" || d.Version().Sequence() != 1 {
		t.Fatal("capture failure mutated document")
	}
	value.D(legacy).Fields = nil
	if s, err := d.SnapshotWith(ParseOptions{}); s == nil || err != nil {
		t.Fatal("retry", err)
	}
}
