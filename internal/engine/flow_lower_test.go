package engine

import (
	"testing"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/ir/core"
	"github.com/ornew/zeroparse/internal/ir/flow"
	flowlower "github.com/ornew/zeroparse/internal/ir/flow/lower"
)

// TestFlowVerifiesCorpus lowers every typed rule of the corpus that Flow IR covers, from the Core
// IR of each mode of the Core comparison with the generator's lowering options (observation in
// the observable mode), and verifies the functions: the rules that LocalRule
// accepts to local functions, and the bodies of the other rules and the lines of Operators Rules
// to framed ones.
func TestFlowVerifiesCorpus(t *testing.T) {
	for _, mode := range coreGenModes {
		t.Run(mode.name, func(t *testing.T) {
			lowered, framed, observed, skips := 0, 0, 0, 0
			for _, gen := range coreGenerations(t, mode) {
				g := gen.gen
				m, err := g.TypedCore(map[core.Node]grammar.Expr{})
				if err != nil {
					t.Fatalf("%s: %v", gen.name, err)
				}
				// The options of the generator, which lowers while it generates the typed table.
				table := g.Table
				g.Table = "trules"
				opts := g.FlowOptions(m)
				g.Table = table
				if opts.Observe != g.Observability {
					t.Fatalf("%s: lowering options do not follow the observability of the mode", gen.name)
				}
				if g.Observability {
					observed++
				}
				check := func(f *flow.Func, err error) {
					if err == nil {
						err = flow.Verify(f)
						skips += observeSkips(f.Body)
					}
					if err != nil {
						t.Errorf("%s: %v", gen.name, err)
					}
				}
				for _, r := range m.Rules {
					if flowlower.LocalRule(r) {
						check(flowlower.Lower(m, r, r.Name, opts))
						lowered++
						continue
					}
					if o, ok := r.Body.(*core.OperatorsRule); ok {
						for _, l := range o.Lines() {
							if flowlower.FramedBody(l.Body) {
								check(flowlower.LowerFrame(m, r, l.Body, l.Scope, l.Body.Attrs().Want, r.Name, opts))
								framed++
							}
						}
					} else if flowlower.FramedBody(r.Body) {
						check(flowlower.LowerFrame(m, r, r.Body, r.Scope, r.Want, r.Name, opts))
						framed++
					}
				}
			}
			t.Logf("lowered %d rules and %d framed bodies; %d grammars observed, %d observed skips", lowered, framed, observed, skips)
			if lowered < 1000 || framed < 100 {
				t.Errorf("lowered %d rules and %d framed bodies", lowered, framed)
			}
			if mode.opts.Observability && (observed < 100 || skips < 100) || !mode.opts.Observability && skips != 0 {
				t.Errorf("lowered %d grammars with observation, with %d observed skips", observed, skips)
			}
		})
	}
}

// observeSkips counts the ObserveSkip statements of ss and of the statements nested in them.
func observeSkips(ss []flow.Stmt) int {
	n := 0
	for _, s := range ss {
		switch s := s.(type) {
		case *flow.ObserveSkip:
			n++
		case *flow.Block:
			n += observeSkips(s.Body)
		case *flow.Loop:
			n += observeSkips(s.Body)
		case *flow.If:
			n += observeSkips(s.Then) + observeSkips(s.Else)
		}
	}
	return n
}
