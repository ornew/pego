//go:build !zp_budget_controls_reference

package closure

import (
	"errors"
	"testing"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/engine/core"
	"github.com/ornew/zeroparse/internal/limits"
	"github.com/ornew/zeroparse/internal/value"
)

func TestBudgetActionArenaResetInterruption(t *testing.T) {
	retained := value.Ptr(value.Data{Text: "retained"})
	p := &core.Parser{}
	cm := MachineOf(p)
	for range 300 {
		*cm.closures.Alloc() = closure{ctx: &core.EvalCtx{Items: []*value.Node{retained}}}
	}
	p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 64})
	if err := catchFatal(t, func() { evalTerm(p.UseCtx(core.EvalCtx{P: p}), &grammar.NilLit{}) }); !errors.Is(err, limits.ErrLimit) {
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
