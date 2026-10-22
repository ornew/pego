package pego_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/ornew/pego"
)

func TestDocumentDiagnosticOwnership(t *testing.T) {
	p, err := compileTestSource(`rule main = item* $$
rule item = "a;" @error(message="original") #recover(skip=[^;]+ ";")`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
		for _, unit := range []pego.PositionUnit{pego.CodePoints, pego.Bytes} {
			for _, changes := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/changes=%v", backend, unit, changes), func(t *testing.T) {
					opts := []pego.ParseOption{pego.WithBackend(backend), pego.WithUnit(unit)}
					d, err := p.NewDocument("?é;", opts...)
					if err != nil {
						t.Fatal(err)
					}
					if changes {
						if err := d.EnableChanges(2); err != nil {
							t.Fatal(err)
						}
					}
					_, want := p.Parse("?é;", opts...)
					_, first := d.Parse()
					list, ok := first.(pego.SyntaxErrors)
					if !ok || len(list) != 1 || !reflect.DeepEqual(first, want) {
						t.Fatalf("first diagnostics: %v / %v", first, want)
					}
					e := list[0]
					e.Pos, e.Line, e.Col = -1, -1, -1
					if len(e.Expected) > 0 {
						e.Expected[0] = "mutated"
					}
					e.Expected = append(e.Expected, "mutated")
					e.Messages[0] = "mutated"
					list[0] = nil
					_, second := d.Parse()
					if !reflect.DeepEqual(second, want) {
						t.Fatalf("caller changed cached diagnostics: %#v / %#v", second, want)
					}
					end := 3
					if unit == pego.Bytes {
						end = 4
					}
					if err := d.Edit(0, end, "??;"); err != nil {
						t.Fatal(err)
					}
					_, _ = d.Parse()
					if !reflect.DeepEqual(second, want) {
						t.Fatal("later parse changed retained diagnostics")
					}

					if err := d.Edit(0, 3, "??"); err != nil {
						t.Fatal(err)
					}
					_, want = p.Parse("??", opts...)
					_, failure := d.Parse()
					ordinary, ok := failure.(*pego.SyntaxError)
					if !ok {
						t.Fatalf("ordinary failure: %T", failure)
					}
					ordinary.Pos, ordinary.Line, ordinary.Col = -1, -1, -1
					if len(ordinary.Expected) > 0 {
						ordinary.Expected[0] = "mutated"
					}
					ordinary.Messages = []string{"mutated"}
					if _, again := d.Parse(); !reflect.DeepEqual(again, want) {
						t.Fatalf("caller changed ordinary failure: %v / %v", again, want)
					}
				})
			}
		}
	}
}
