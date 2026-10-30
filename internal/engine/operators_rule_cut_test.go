package engine

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/value"
)

func TestOperatorsRulePrefixPartCuts(t *testing.T) {
	t.Parallel()
	for _, disableMemo := range []bool{false, true} {
		prog := mustCompile(t, gentest.PrefixPartCuts, compile.Options{DisableMemo: disableMemo})
		for _, c := range gentest.PrefixPartCutInputs {
			t.Run(fmt.Sprintf("%s/memo=%t", c.Input, !disableMemo), func(t *testing.T) {
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					for _, unit := range []PositionUnit{CodePoints, Bytes} {
						n, err := prog.ParseWith("main", c.Input, ParseOptions{Expectations: true, Backend: backend, Unit: unit})
						if c.Kind == "" {
							if n != nil || err == nil || !strings.Contains(err.Error(), "syntax error") {
								t.Fatalf("%v/%v: expected syntax failure, got %v, %v", backend, unit, n, err)
							}
						} else if n == nil || n.Type() != c.Kind {
							t.Fatalf("%v/%v: got %v, %v; want %s", backend, unit, n, err, c.Kind)
						} else if c.Kind == value.TypeError {
							var recovered SyntaxErrors
							if !errors.As(err, &recovered) || len(recovered) != 1 {
								t.Fatalf("%v/%v: expected one recovered error, got %v", backend, unit, err)
							}
						} else if err != nil {
							t.Fatalf("%v/%v: unexpected error: %v", backend, unit, err)
						}
						if n != nil && n.Type() == "Prefix" {
							if tag := fmt.Sprint(n.Field("Tag")); tag != "1" {
								t.Fatalf("%v/%v: selected wrong prefix: tag %s", backend, unit, tag)
							}
							wantOp := "-"
							switch c.Input[:1] {
							case "c", "n", "v", "r":
								wantOp = "-z"
							case "b":
								wantOp = "-zt"
							case "h":
								wantOp = "-x"
							case "u":
								wantOp = "¬z"
							case "p":
								wantOp = "~"
							case "i":
								wantOp = "a"
							}
							op, ok := n.Field("Op").(*value.Node)
							if !ok || leafText(op) != wantOp {
								t.Fatalf("%v/%v: wrong operator capture: %v; want %q", backend, unit, op, wantOp)
							}
						}
						checkRecognize(t, prog, "main", c.Input, unit, err)
					}
				}
				checkBackends(t, prog, "main", c.Input)
				checkUnits(t, prog, "main", c.Input, Closure)
			})
		}
	}
}

// Compilation is outside the timer. These accepted inputs perform equivalent work on the old and
// new cut contracts; committed failures have deliberately different outcomes and are tested above.
func BenchmarkOperatorsRulePrefixPartCut(b *testing.B) {
	prog, err := Compile(mustParse(gentest.PrefixPartCuts), compile.Options{})
	if err != nil {
		b.Fatal(err)
	}
	for _, c := range []struct{ name, input string }{
		{"operand", "ca"}, {"matched-cut", "c-za"}, {"competing-uncut-fallback", "f-x"},
	} {
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				b.Run(fmt.Sprintf("%s/%s/unit=%d", c.name, backend, unit), func(b *testing.B) {
					opts := ParseOptions{Expectations: true, Backend: backend, Unit: unit}
					b.ReportAllocs()
					for b.Loop() {
						if _, err := prog.ParseWith("main", c.input, opts); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		}
	}
}

// leafText returns the text of the strings in the value of n, in order: the text an operator
// matched when its line is a sequence.
func leafText(n *value.Node) string {
	if n == nil {
		return ""
	}
	if value.ShapeOf(n) == value.ShapeString {
		return value.D(n).Text
	}
	var b strings.Builder
	for _, c := range value.D(n).Children {
		b.WriteString(leafText(c))
	}
	return b.String()
}
