//go:build pego_work_controls_reference

package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

func TestWorkStreamReferenceAdmission(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, gentest.Records)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, o := range []ParseOptions{{Context: ctx}, {Limits: limits.Limits{MaxWork: 1}}, {Limits: limits.Limits{MaxWork: 1}}} {
		r := &streamCallbackReader{text: "#records\na=1\n"}
		callbacks := 0
		r.onRead = func() { callbacks++ }
		o.Trace = func(TraceEvent) { callbacks++ }
		err := prog.ParseStreamWith("main", r, func(*value.Node) error { callbacks++; return nil }, o)
		if err == nil || !strings.Contains(err.Error(), "reference build") || callbacks != 0 || r.offset != 0 || r.closed {
			t.Fatalf("error=%v callbacks=%d offset=%d", err, callbacks, r.offset)
		}
	}
	for _, o := range []ParseOptions{{}, {Context: context.Background()}} {
		if err := prog.ParseStreamWith("main", strings.NewReader("#records\na=1\n"), nil, o); err != nil {
			t.Fatal(err)
		}
	}
}
