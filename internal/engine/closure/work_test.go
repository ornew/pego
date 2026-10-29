//go:build !pego_work_controls_reference

package closure

import (
	"errors"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

func TestWorkActionArenaResetInterruption(t *testing.T) {
	retained := value.Ptr(value.Data{Text: "retained"})
	p := &core.Parser{}
	cm := MachineOf(p)
	for range 300 {
		*cm.closures.Alloc() = closure{ctx: &core.EvalCtx{Items: []*value.Node{retained}}}
	}
	p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 64})
	if err := catchFatal(t, func() { evalTerm(p.UseCtx(core.EvalCtx{P: p}), &grammar.NilLit{}) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("arena reset interruption = %v", err)
	}
	p.ReleaseScratch()
	evalTerm(p.UseCtx(core.EvalCtx{P: p}), &grammar.NilLit{})
	if MachineOf(p) != cm || cm.closures.N != 0 || value.D(retained).Text != "retained" {
		t.Fatal("reset retry failed or changed an externally retained value")
	}
	for _, chunk := range cm.closures.Chunks {
		for _, f := range chunk {
			if f.ctx != nil || f.params != nil || f.body != nil {
				t.Fatal("reset retry left references in the arena")
			}
		}
	}
}
