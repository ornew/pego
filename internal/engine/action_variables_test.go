package engine

import (
	"testing"

	"github.com/ornew/pego/internal/value"
)

// Four alternatives force reuse both with deferred batch memoization and with
// Document's first-call memoization. No predicate reads x on behalf of r.
var actionVariableCases = []genCase{
	{"rule action variables", `type V = { N int }
var x int
rule main = {x := 1} r "b" / {x := 2} r "c" / {x := 3} r "d" / {x := 4} r
rule r = leaf => V{N: x}
rule leaf = "a"`, []string{"a", "ab", "ac", "ad"}},
	{"transitive action variables", `type V = { N int }
var x int
rule main = {x := 1} r "b" / {x := 2} r "c" / {x := 3} r "d" / {x := 4} r
rule r = inner
rule inner = leaf => V{N: x}
rule leaf = "a"`, []string{"a", "ab", "ac", "ad"}},
	{"operatorsRule operand action variables", `type V = { N int }
var x int
rule main = {x := 1} r "b" / {x := 2} r "c" / {x := 3} r "d" / {x := 4} r
rule r = #operators {
    operand leaf => V{N: x}
}
rule leaf = "a"`, []string{"a", "ab", "ac", "ad"}},
	{"operatorsRule prefix action variables", `type V = { N int }
var x int
rule main = {x := 1} r "b" / {x := 2} r "c" / {x := 3} r "d" / {x := 4} r
rule r = #operators {
    operand leaf
    level { prefix "-" => V{N: x} }
}
rule leaf = "a"`, []string{"-a", "-ab", "-ac", "-ad"}},
	{"operatorsRule infix action variables", `type V = { N int }
var x int
rule main = {x := 1} r "b" / {x := 2} r "c" / {x := 3} r "d" / {x := 4} r
rule r = #operators {
    operand leaf
    level { infix left "+" => V{N: x} }
}
rule leaf = "a"`, []string{"a+a", "a+ab", "a+ac", "a+ad"}},
	{"operatorsRule postfix action variables", `type V = { N int }
var x int
rule main = {x := 1} r "b" / {x := 2} r "c" / {x := 3} r "d" / {x := 4} r
rule r = #operators {
    operand leaf
    level { postfix "!" => V{N: x} }
}
rule leaf = "a"`, []string{"a!", "a!b", "a!c", "a!d"}},
}

func TestMemoActionVariables(t *testing.T) {
	for _, c := range actionVariableCases {
		t.Run(c.name, func(t *testing.T) {
			for _, disable := range []bool{false, true} {
				prog := compile(t, c.src, Options{DisableMemo: disable})
				if vars := prog.byName["r"].vars; len(vars) != 1 || vars[0] != "x" {
					t.Errorf("r variable dependencies: %v", vars)
				}
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					for _, unit := range []PositionUnit{CodePoints, Bytes} {
						opts := ParseOptions{Backend: backend, Unit: unit}
						input := c.inputs[0]
						n, err := prog.ParseWith("main", input, opts)
						if err != nil || n == nil || len(value.D(n).Children) != 1 || value.D(n).Children[0].Field("N") != 4 {
							t.Errorf("memo disabled=%v, %v/%v: got %v, %v; want N=4", disable, backend, unit, n, err)
						}
						doc, err := prog.NewDocumentWith("main", input, opts)
						if err != nil {
							t.Fatal(err)
						}
						for i := 0; i < 2; i++ {
							n, err := doc.Parse()
							if err != nil || n == nil || len(value.D(n).Children) != 1 || value.D(n).Children[0].Field("N") != 4 {
								t.Errorf("Document parse %d, memo disabled=%v, %v/%v: got %v, %v; want N=4", i, disable, backend, unit, n, err)
							}
						}
					}
				}
			}
		})
	}
}

func TestDocumentActionVariableEdit(t *testing.T) {
	prog := compile(t, `type V = { N int }
var n int
rule main = text:[a-z]+ #atomic {n := len($text)} r
rule r = "!" => V{N: n}`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			doc, err := prog.NewDocumentWith("main", "a!", ParseOptions{Backend: backend, Unit: unit})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := doc.Parse(); err != nil {
				t.Fatal(err)
			}
			if err := doc.Edit(0, 1, "bb"); err != nil {
				t.Fatal(err)
			}
			n, err := doc.Parse()
			if err != nil || n == nil || value.D(n).Children[1].Field("N") != 2 {
				t.Errorf("%v/%v: got %v, %v; want edited N=2", backend, unit, n, err)
			}
		}
	}
}
