package document_test

import (
	"math/rand"
	"testing"

	"github.com/ornew/pego/internal/engine/document"

	"github.com/ornew/pego/internal/compile"
	. "github.com/ornew/pego/internal/engine"
)

func TestDocumentIndirectLeftRecursionEdits(t *testing.T) {
	defer document.SetMaxEdits(17)()
	const src = `rule main = (l:([^\n] / ![ab] r2) "\n" / r1 / m:[^\n]* "\n")* [^\n]* $$
rule r1 = (r2 / "b") #repeat(0, 2)
rule r2 = r1 / r2 "a" "b"`
	for _, options := range []compile.Options{{}, {DisableMemo: true}} {
		prog := mustCompile(t, src, options)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				opts := ParseOptions{Backend: backend, Unit: unit}
				d, err := prog.NewDocumentWith("main", "b", opts)
				if err != nil {
					t.Fatal(err)
				}
				check := func(step int) {
					t.Helper()
					got, gotErr := d.Parse()
					want, wantErr := prog.ParseWith("main", d.Text(), opts)
					if resultJSON(got, gotErr) != resultJSON(want, wantErr) {
						t.Fatalf("%s/%s (DisableMemo=%v) step %d text %q: Document %s; fresh %s", backend, unit, options.DisableMemo, step, d.Text(), resultJSON(got, gotErr), resultJSON(want, wantErr))
					}
				}
				check(0)
				for step, edit := range []struct {
					start, end int
					text       string
				}{
					{0, 0, "\n"},
					{0, 1, ""},
					{1, 1, "\n"},
					{0, 1, "a"},
					{0, 0, "\n"},
				} {
					if err := d.Edit(edit.start, edit.end, edit.text); err != nil {
						t.Fatal(err)
					}
					check(step + 1)
				}
				// Repeat the same grammar through nullable, failing and growing paths,
				// including enough edits to reset the bounded edit history.
				rng := rand.New(rand.NewSource(20))
				pieces := []string{"", "a", "b", "\n", "ab", "\n\n"}
				for step := range 200 {
					length := len(d.Text()) // All edits here are ASCII in either unit.
					start := rng.Intn(length + 1)
					end := start + rng.Intn(min(3, length-start)+1)
					if err := d.Edit(start, end, pieces[rng.Intn(len(pieces))]); err != nil {
						t.Fatal(err)
					}
					check(step + 6)
				}
			}
		}
	}
}
