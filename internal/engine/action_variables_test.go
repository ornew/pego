package engine

import "testing"

// Four alternatives force reuse both with deferred batch memoization and with
// Document's first-call memoization. No predicate reads x on behalf of r.
var actionVariableCases = []genCase{
	{"rule action variables", `
type V struct { N int }
def main = [x=1] r "b" / [x=2] r "c" / [x=3] r "d" / [x=4] r
def r = leaf -> new V{N:x}
def leaf = "a"`, []string{"a", "ab", "ac", "ad"}},
	{"transitive action variables", `
type V struct { N int }
def main = [x=1] r "b" / [x=2] r "c" / [x=3] r "d" / [x=4] r
def r = inner
def inner = leaf -> new V{N:x}
def leaf = "a"`, []string{"a", "ab", "ac", "ad"}},
	{"pratt operand action variables", `
type V struct { N int }
def main = [x=1] r "b" / [x=2] r "c" / [x=3] r "d" / [x=4] r
def r = pratt { operand leaf -> new V{N:x} }
def leaf = "a"`, []string{"a", "ab", "ac", "ad"}},
	{"pratt prefix action variables", `
type V struct { N int }
def main = [x=1] r "b" / [x=2] r "c" / [x=3] r "d" / [x=4] r
def r = pratt { operand leaf level { prefix "-" -> new V{N:x} } }
def leaf = "a"`, []string{"-a", "-ab", "-ac", "-ad"}},
	{"pratt infix action variables", `
type V struct { N int }
def main = [x=1] r "b" / [x=2] r "c" / [x=3] r "d" / [x=4] r
def r = pratt { operand leaf level { infix left "+" -> new V{N:x} } }
def leaf = "a"`, []string{"a+a", "a+ab", "a+ac", "a+ad"}},
	{"pratt postfix action variables", `
type V struct { N int }
def main = [x=1] r "b" / [x=2] r "c" / [x=3] r "d" / [x=4] r
def r = pratt { operand leaf level { postfix "!" -> new V{N:x} } }
def leaf = "a"`, []string{"a!", "a!b", "a!c", "a!d"}},
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
					for _, unit := range []Unit{CodePoints, Bytes} {
						opts := ParseOptions{Backend: backend, Unit: unit}
						input := c.inputs[0]
						n, err := prog.ParseWith("main", input, opts)
						if err != nil || n == nil || len(n.Children) != 1 || n.Children[0].Field("N") != 4 {
							t.Errorf("memo disabled=%v, %v/%v: got %v, %v; want N=4", disable, backend, unit, n, err)
						}
						doc, err := prog.NewDocumentWith("main", input, opts)
						if err != nil {
							t.Fatal(err)
						}
						for i := 0; i < 2; i++ {
							n, err := doc.Parse()
							if err != nil || n == nil || len(n.Children) != 1 || n.Children[0].Field("N") != 4 {
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
	prog := compile(t, `
type V struct { N int }
def main = text:@(?a-z)+ [n=len($text)] r
def r = "!" -> new V{N:n}`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
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
			if err != nil || n == nil || n.Children[1].Field("N") != 2 {
				t.Errorf("%v/%v: got %v, %v; want edited N=2", backend, unit, n, err)
			}
		}
	}
}
