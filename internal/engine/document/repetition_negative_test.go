package document_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/compile"
	. "github.com/ornew/zeroparse/internal/engine"
	"github.com/ornew/zeroparse/internal/engine/core"
	"github.com/ornew/zeroparse/internal/value"
)

func TestNegativeMaximumDocumentsAndStreams(t *testing.T) {
	// Force recording of this short repetition so edits exercise resumption.
	defer func(n int) { core.MinRecorded = n }(core.MinRecorded)
	core.MinRecorded = 1
	for _, maximum := range negativeMaxima() {
		g := grammarWithNegativeMaximum(t, `rule main = "é"* #stream $$`, maximum)
		p, err := Compile(g, compile.Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range valueModes {
			loaded := saveLoad(t, p, "main", mode)
			for _, prog := range []*Program{p, loaded} {
				for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
					if prog == loaded && b == Closure {
						_, err := loaded.NewDocumentWith("main", "", ParseOptions{Backend: b})
						if mode == CompiledDocument {
							wantClosureRefused(t, err)
						} else if err == nil {
							t.Fatalf("Document on a %s-mode file accepted", mode)
						}
						continue
					}
					for _, unit := range []PositionUnit{CodePoints, Bytes} {
						opts := ParseOptions{Backend: b, Unit: unit}
						streamOpts := opts
						if prog == loaded {
							streamOpts = servedBy(mode, opts)
						}
						var got []string
						err := prog.ParseStreamWith("main", strings.NewReader("ééé"), func(n *value.Node) error {
							got = append(got, resultJSON(n, nil))
							return nil
						}, streamOpts)
						var want []string
						we := p.ParseStreamWith("main", strings.NewReader("ééé"), func(n *value.Node) error {
							want = append(want, resultJSON(n, nil))
							return nil
						}, ParseOptions{Unit: unit})
						if len(got) != 3 || strings.Join(got, "\n") != strings.Join(want, "\n") || fmt.Sprint(err) != fmt.Sprint(we) {
							t.Fatalf("max=%d stream %s/%s got %v, %v", maximum, b, unit, got, err)
						}
						d, err := prog.NewDocumentWith("main", strings.Repeat("é", 100), opts)
						if prog == loaded && mode != CompiledDocument {
							if err == nil || !strings.Contains(err.Error(), "documents need document mode") {
								t.Fatalf("Document on a %s-mode file: %v", mode, err)
							}
							continue
						}
						if err != nil {
							t.Fatal(err)
						}
						for round := 0; round < 3; round++ {
							n, err := d.Parse()
							ref, re := p.ParseWith("main", d.Text(), ParseOptions{Unit: unit})
							if resultJSON(n, err) != resultJSON(ref, re) {
								t.Fatalf("max=%d Document %s/%s round %d: got %s want %s", maximum, b, unit, round, resultJSON(n, err), resultJSON(ref, re))
							}
							if round > 0 && d.Resumed() == 0 {
								t.Fatal("edited Document did not resume repetition elements")
							}
							at := 50
							if unit == Bytes {
								at *= len("é")
							}
							if err := d.Edit(at, at, "é"); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
			}
		}
	}
}
