package engine

import (
	"math/rand"
	"testing"

	"github.com/ornew/zeroparse/internal/engine/core"
)

// A Document that edits a grammar with hidden rules gives what a fresh parse gives, on every
// backend, with the repetitions resuming too.
func TestDocumentMatchesFreshParseWithHiddenRules(t *testing.T) {
	const grammarSrc = `rule main = (_stmt "\n")* $$
rule _stmt = _lhs "=" _rhs
rule _lhs = [a-z]+ #atomic " "?
rule _rhs = [0-9]+ #atomic (_op [0-9]+ #atomic)*
rule _op = "+" / "-"`
	run := func(t *testing.T, b Backend) {
		prog := mustCompile(t, grammarSrc)
		o := ParseOptions{Backend: b}
		rng := rand.New(rand.NewSource(7))
		pieces := []string{"a", "b", "=", "1", "+", "23", "-", "\n", " ", "x=1\n"}
		doc, err := prog.NewDocumentWith("main", "x=1+2\nab =3\ny= 4-5+6\n", o)
		if err != nil {
			t.Fatal(err)
		}
		doc.Parse()
		for i := 0; i < 1500; i++ {
			cur := []rune(doc.Text())
			start := rng.Intn(len(cur) + 1)
			end := start + rng.Intn(min(4, len(cur)-start)+1)
			ins := ""
			for k := rng.Intn(3); k > 0; k-- {
				ins += pieces[rng.Intn(len(pieces))]
			}
			if err := doc.Edit(start, end, ins); err != nil {
				t.Fatal(err)
			}
			n, err := doc.Parse()
			got := dump(t, n, err)
			fn, ferr := prog.Parse("main", doc.Text())
			if want := dump(t, fn, ferr); got != want {
				t.Fatalf("edit %d: [%d,%d) -> %q\ntext %q\n got  %s\n want %s", i, start, end, ins, doc.Text(), got, want)
			}
		}
	}
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		t.Run(b.String(), func(t *testing.T) { run(t, b) })
	}
	defer func(n int) { core.MinRecorded = n }(core.MinRecorded)
	core.MinRecorded = 1
	t.Run("resume", func(t *testing.T) { run(t, Closure) })
}
