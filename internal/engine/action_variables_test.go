package engine

import (
	"testing"

	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/gentest"
	"github.com/ornew/zeroparse/internal/value"
)

func TestMemoActionVariables(t *testing.T) {
	t.Parallel()
	for _, c := range gentest.ActionVariableCases {
		t.Run(c.Name, func(t *testing.T) {
			for _, disable := range []bool{false, true} {
				prog := mustCompile(t, c.Src, compile.Options{DisableMemo: disable})
				if vars := prog.ByName["r"].Vars; len(vars) != 1 || vars[0] != "x" {
					t.Errorf("r variable dependencies: %v", vars)
				}
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					for _, unit := range []PositionUnit{CodePoints, Bytes} {
						opts := ParseOptions{Backend: backend, Unit: unit}
						input := c.Inputs[0]
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
	t.Parallel()
	prog := mustCompile(t, `type V = { N int }
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
