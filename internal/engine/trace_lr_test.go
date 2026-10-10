package engine

import "testing"

// A wrapper can return a growing seed without examining another character.
// Its dependency range must still include the input consumed by that seed.
func TestTraceLeftRecursionSeedRange(t *testing.T) {
	for _, src := range []string{
		`def main = expr $$
def expr = step / seed
def step = expr
def seed = "é"`,
		`def main = expr $$
def expr = step / seed
def step = expr "é"
def seed = ""`,
	} {
		prog := compile(t, src)
		for _, input := range []string{"", "é", "éé", "é?"} {
			for _, unit := range []Unit{CodePoints, Bytes} {
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
		{`def main = "!" expr "xy" $$
def expr = step / seed
def step = expr
def seed = "é" &"xy"`, "!éxy", false},
		{`def main = "\n" expr $$
def expr = step / seed
def step = expr
def seed = ^ "é"`, "\né", true},
	} {
		prog := compile(t, c.src)
		for _, unit := range []Unit{CodePoints, Bytes} {
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				for _, recognize := range []bool{false, true} {
					seen := false
					o := ParseOptions{Unit: unit, Backend: backend, Recognize: recognize}
					o.Trace = func(e TraceEvent) {
						if e.Kind != TraceExit || e.Rule != "expr" || !e.Memo || !e.Matched {
							return
						}
						seed, found := e.p.memo.get(memoKey{rule: prog.byName["expr"].id, pos: e.Pos})
						if !found || !seed.growing || !seed.ok {
							t.Fatal("successful growing seed is not available")
						}
						seen = true
						if c.behind {
							if seed.from != 0 {
								t.Errorf("line-start dependency from=%d, want 0", seed.from)
							}
						} else if seed.examined != e.End+2 || e.Examined != e.End+2 {
							t.Errorf("lookahead dependency seed=%d, trace=%d, want %d", seed.examined, e.Examined, e.End+2)
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
