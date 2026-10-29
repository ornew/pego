package engine

import (
	"testing"

	"github.com/ornew/pego/internal/engine/core"
)

// A wrapper can return a growing seed without examining another character.
// Its dependency range must still include the input consumed by that seed.
func TestTraceLeftRecursionSeedRange(t *testing.T) {
	for _, src := range []string{
		`rule main = expr $$
rule expr = step / seed
rule step = expr
rule seed = "é"`,
		`rule main = expr $$
rule expr = step / seed
rule step = expr "é"
rule seed = ""`,
	} {
		prog := mustCompile(t, src)
		for _, input := range []string{"", "é", "éé", "é?"} {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					for _, recognize := range []bool{false, true} {
						o := ParseOptions{Unit: unit, Backend: backend, Recognize: recognize}
						checkTrace(t, prog, "main", input, o)
						o.Trace = func(e TraceEvent) {
							if e.Kind == TraceExit && e.Matched && e.Examined < e.End {
								t.Errorf("%v/%v %q: %s seed range ends at %d, examined %d", backend, unit, input, e.Rule, e.End, e.Examined)
							}
						}
						prog.ParseWith("main", input, o)
					}
				}
			}
			checkTraceDocument(t, prog, input)
		}
	}
}

func TestGrowingSeedDependencyBounds(t *testing.T) {
	for _, c := range []struct {
		src, input string
		behind     bool
	}{
		{`rule main = "!" expr "xy" $$
rule expr = step / seed
rule step = expr
rule seed = "é" &"xy"`, "!éxy", false},
		{`rule main = "\n" expr $$
rule expr = step / seed
rule step = expr
rule seed = ^ "é"`, "\né", true},
	} {
		prog := mustCompile(t, c.src)
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				for _, recognize := range []bool{false, true} {
					seen := false
					o := ParseOptions{Unit: unit, Backend: backend, Recognize: recognize}
					o.Trace = func(e TraceEvent) {
						if e.Kind != TraceExit || e.Rule != "expr" || !e.Memo || !e.Matched {
							return
						}
						seed, found := core.TraceParser(e).Memo.Get(core.MemoKey{Rule: prog.ByName["expr"].ID, Pos: e.Pos})
						if !found || !seed.Growing || !seed.Ok {
							t.Fatal("successful growing seed is not available")
						}
						seen = true
						if c.behind {
							if seed.From != 0 {
								t.Errorf("line-start dependency from=%d, want 0", seed.From)
							}
						} else if seed.Examined != e.End+2 || e.Examined != e.End+2 {
							t.Errorf("lookahead dependency seed=%d, trace=%d, want %d", seed.Examined, e.Examined, e.End+2)
						}
					}
					if _, err := prog.ParseWith("main", c.input, o); err != nil {
						t.Fatal(err)
					}
					if !seen {
						t.Fatal("successful seed path was not exercised")
					}
					checkTrace(t, prog, "main", c.input, o)
				}
			}
		}
		checkTraceDocument(t, prog, c.input)
	}
}
