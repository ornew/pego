package document_test

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/engine/document"

	. "github.com/ornew/zeroparse/internal/engine"
	"github.com/ornew/zeroparse/internal/engine/core"
)

// TestDocumentEditKeepsMemo checks that an edit keeps exactly the memo entries the rules in
// Document's comment allow, at their new positions. The second grammar has a memoized rule that
// examines no input, whose entries at the edit position stay in place.
func TestDocumentEditKeepsMemo(t *testing.T) {
	cases := []struct{ grammar, text string }{
		{incrementalGrammar, "x = 1+2\nab,cd\n@pos\n\ny = (3)*-4\n"},
		{"rule main = (sep word sep)* $$\nrule sep = none none\nrule none = \"\"\nrule word = [a-z]+ #atomic \" \"?", "ab cd ef gh "},
	}
	pieces := []string{"a", "b", "x = ", "1", "+", "(", ")", ",", "\n", "@", " ", "zz "}
	for _, c := range cases {
		doc, err := mustCompile(t, c.grammar).NewDocument("main", c.text)
		if err != nil {
			t.Fatal(err)
		}
		doc.Parse()
		rng := rand.New(rand.NewSource(7))
		for i := 0; i < 2000; i++ {
			cur := []rune(doc.Text())
			start := rng.Intn(len(cur) + 1)
			end := start + rng.Intn(min(4, len(cur)-start)+1)
			ins := ""
			for k := rng.Intn(3); k > 0; k-- {
				ins += pieces[rng.Intn(len(pieces))]
			}
			delta := len([]rune(ins)) - (end - start)
			want := map[*core.MemoEntry]int{}
			doc.Memo().Each(func(e *core.MemoEntry) {
				if !core.AdvanceEntry(e, doc.Edits()) {
					return // invalidated by an earlier edit, and not looked up since
				}
				switch {
				case e.Growing || e.Provisional:
				case e.Examined <= start:
					want[e] = e.Pos
				case e.From >= end && !e.Positional && len(e.Errs) == 0:
					want[e] = e.Pos + delta
				}
			})
			if err := doc.Edit(start, end, ins); err != nil {
				t.Fatal(err)
			}
			// Entries are brought up to date when they are looked up: do it for every entry.
			got := map[*core.MemoEntry]int{}
			m := doc.Memo()
			for idx, head := range m.Slots {
				at := idx
				if idx >= m.Gap+m.GapLen {
					at -= m.GapLen
				} else if idx >= m.Gap && head != nil {
					t.Fatalf("edit %d: entry in the gap", i)
				}
				for e := head; e != nil; e = e.Next {
					if !core.AdvanceEntry(e, doc.Edits()) {
						continue
					}
					if e.Pos != at {
						t.Fatalf("edit %d: entry for position %d is listed at %d", i, e.Pos, at)
					}
					got[e] = at
				}
			}
			if len(got) != len(want) {
				t.Fatalf("edit %d: [%d,%d) -> %q: %d entries, want %d", i, start, end, ins, len(got), len(want))
			}
			for e, at := range want {
				if p, ok := got[e]; !ok || p != at {
					t.Fatalf("edit %d: [%d,%d) -> %q: entry at %d (kept %v), want %d", i, start, end, ins, p, ok, at)
				}
			}
			doc.Parse()
		}
	}
}

// TestDocumentEditLogLimit checks parses across resets of a full edit log.
func TestDocumentEditLogLimit(t *testing.T) {
	defer document.SetMaxEdits(5)()
	prog := mustCompile(t, `rule main = line* $$
rule line = x:[a-z]+ #atomic "\n"`)
	doc, err := prog.NewDocument("main", "ab\ncd\nef\n")
	if err != nil {
		t.Fatal(err)
	}
	doc.Parse()
	rng := rand.New(rand.NewSource(5))
	for i := 0; i < 300; i++ {
		for k := rng.Intn(4); k >= 0; k-- {
			n := len([]rune(doc.Text()))
			at := rng.Intn(n)
			if rng.Intn(2) == 0 {
				doc.Edit(at, at, []string{"q", "zz\n"}[rng.Intn(2)])
			} else if t := doc.Text(); t[at] != '\n' && at > 0 && t[at-1] != '\n' {
				doc.Edit(at, at+1, "")
			}
		}
		n, err := doc.Parse()
		fn, ferr := prog.Parse("main", doc.Text())
		if got, want := dump(t, n, err), dump(t, fn, ferr); got != want {
			t.Fatalf("round %d: text %q\n got  %s\n want %s", i, doc.Text(), got, want)
		}
	}
}

// TestDocumentEditText compares edited text and random decoded spans with a
// fresh flat input, including multi-byte characters and invalid UTF-8.
func TestDocumentEditText(t *testing.T) {
	prog := mustCompile(t, `rule main = .*`)
	pieces := []string{"a", "é", "日本", "\n", "\xff", "z\xe3\x81", ""}
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		doc, err := prog.NewDocumentWith("main", "héllo, 世界\n", ParseOptions{Unit: unit})
		if err != nil {
			t.Fatal(err)
		}
		ref := []rune("héllo, 世界\n") // the expected text, in code points
		refBytes := []byte("héllo, 世界\n")
		rng := rand.New(rand.NewSource(3))
		for i := 0; i < 3000; i++ {
			piece := pieces[rng.Intn(len(pieces))]
			var start, end int
			if unit == Bytes {
				// Edit at character boundaries of the current bytes.
				var bounds []int
				for k := 0; k <= len(refBytes); k++ {
					if core.ValidBoundary(string(refBytes), Bytes, k) == nil {
						bounds = append(bounds, k)
					}
				}
				start = bounds[rng.Intn(len(bounds))]
				end = start
				for _, b := range bounds {
					if b >= start && b <= start+4 && rng.Intn(2) == 0 {
						end = b
					}
				}
				refBytes = append(append(append([]byte{}, refBytes[:start]...), piece...), refBytes[end:]...)
			} else {
				start = rng.Intn(len(ref) + 1)
				end = start + rng.Intn(min(3, len(ref)-start)+1)
				ref = append(append(append([]rune{}, ref[:start]...), []rune(piece)...), ref[end:]...)
			}
			if err := doc.Edit(start, end, piece); err != nil {
				t.Fatal(err)
			}
			wantText := string(refBytes)
			if unit == CodePoints {
				wantText = string(ref)
			}
			want := core.NewInput(wantText, unit)
			got := doc.Input()
			if got.Loaded() != want.Loaded() || doc.Text() != wantText {
				t.Fatalf("%v, edit %d: [%d,%d) -> %q: text differs from a fresh read", unit, i, start, end, piece)
			}
			for _, pos := range []int{0, min(start, got.Loaded()), min(end, got.Loaded()), got.Loaded()} {
				gr, gs, ge, gok := got.Decode(pos)
				wr, ws, we, wok := want.Decode(pos)
				if gr != wr || gs != ws || ge != we || gok != wok {
					t.Fatalf("%v, edit %d, position %d: decoded %U/%d/%d/%v, want %U/%d/%d/%v", unit, i, pos, gr, gs, ge, gok, wr, ws, we, wok)
				}
			}
			if _, err := doc.Parse(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestDocumentKeepsInputTables checks that piece-local code-point offsets and
// the line-start table stay with the Document across parsing and editing.
func TestDocumentKeepsInputTables(t *testing.T) {
	doc, err := mustCompile(t, `rule main = ([a-z]+ #atomic "\n")* $$`).NewDocument("main", "ab\ncd\n!")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Parse(); err == nil {
		t.Fatal("expected a syntax error")
	}
	if doc.Input().Lines == nil {
		t.Fatal("line starts missing")
	}
	if doc.Input().Pieces != nil && (len(doc.Input().Pieces.Parts) == 0 || doc.Input().Pieces.Parts[0].Offs == nil) || doc.Input().Pieces == nil && doc.Input().Offs == nil {
		t.Fatal("text offsets missing")
	}
	if err := doc.Edit(6, 7, "ef\n"); err != nil {
		t.Fatal(err)
	}
	if doc.Input().Pieces != nil {
		for _, p := range doc.Input().Pieces.Parts {
			if p.Offs == nil {
				t.Fatal("piece offsets missing after edit")
			}
		}
	} else if doc.Input().Offs == nil {
		t.Fatal("flat offsets missing after edit")
	}
	if n, err := doc.Parse(); err != nil || n.String() != `([("ab" "\n") ("cd" "\n") ("ef" "\n")])@main` {
		t.Fatalf("got %v, %v", n, err)
	}
}

func testDocumentResumesRepetitions(t *testing.T, backend Backend) {
	cases := []struct {
		name, grammar string
		shifts        bool // elements after an edit are reused
	}{
		{"captures", `rule main = stmt* $$
rule stmt = k:key " = " v:val ";\n" / "{\n" body:stmt* "}\n" / "\n"
rule key = [a-z]+ #atomic
rule val = [0-9]+ #atomic / "[" items:([0-9]+ #atomic ",")* "]"`, true},
		// @stream has no effect on a Document.
		{"stream", `rule main = stmt* #stream $$
rule stmt = k:key " = " v:val ";\n" / "{\n" body:stmt* "}\n" / "\n"
rule key = [a-z]+ #atomic
rule val = [0-9]+ #atomic / "[" items:([0-9]+ #atomic ",")* "]"`, true},
		{"inline", `rule main = ([a-z]+ #atomic " = " ([0-9]+ #atomic / "[" ([0-9]+ #atomic ",")* "]") ";\n" / "\n")* $$`, true},
		// Positions in the values of the elements: they are not moved past an edit.
		{"positional", `type P = { At int }
rule main = stmt* $$
rule stmt = assign / "\n"
rule assign = k:key " = " v:val ";\n" => P{At: $k.Start}
rule key = [a-z]+ #atomic
rule val = [0-9]+ #atomic / "[" items:([0-9]+ #atomic ",")* "]"`, false},
	}
	// Mostly edits that keep the text valid.
	pieces := []string{"a", "b", "1", "22", "q = 5;\n", "\n", "\n\n", "{\nk = 1;\n}\n", "[", "2,", "]", " = ", ""}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prog := mustCompile(t, c.grammar)
			var b strings.Builder
			for i := 0; i < 60; i++ {
				fmt.Fprintf(&b, "x = %d;\nyz = [1,22,3,];\n\n", i)
			}
			doc, err := prog.NewDocumentWith("main", b.String(), ParseOptions{Backend: backend})
			if err != nil {
				t.Fatal(err)
			}
			doc.Parse()
			rng := rand.New(rand.NewSource(3))
			check := func(round int, what string) (int, error) {
				got, err := doc.Parse()
				fn, ferr := prog.Parse("main", doc.Text())
				if g, w := dump(t, got, err), dump(t, fn, ferr); g != w {
					t.Fatalf("round %d, %s\ntext %q\n got  %s\n want %s", round, what, doc.Text(), g, w)
				}
				return doc.Resumed(), err
			}
			tails, valid := 0, 0
			for i := 0; i < 600; i++ {
				cur := []rune(doc.Text())
				start := rng.Intn(len(cur))
				for rng.Intn(2) == 0 && start > 0 && cur[start-1] != '\n' {
					start-- // often at a line start
				}
				end := start
				if rng.Intn(3) == 0 {
					end += rng.Intn(min(2, len(cur)-start) + 1)
				}
				ins := pieces[rng.Intn(len(pieces))]
				if c.name == "inline" && strings.ContainsAny(ins, "{}") {
					ins = ""
				}
				if err := doc.Edit(start, end, ins); err != nil {
					t.Fatal(err)
				}
				_, err := check(i, fmt.Sprintf("[%d,%d) -> %q", start, end, ins))
				// Undo the edit: the elements after it move back.
				if err := doc.Edit(start, start+len([]rune(ins)), string(cur[start:end])); err != nil {
					t.Fatal(err)
				}
				resumed, _ := check(i, "undo")
				if err != nil || len([]rune(ins)) == end-start {
					continue
				}
				// The edited text parsed and the edit moved the text after it: the elements after the
				// edit are reused on the undo, unless their values contain positions.
				valid++
				if before := strings.Count(string(cur[:start]), "\n"); resumed > before+10 {
					tails++
				}
			}
			if c.shifts && tails < valid*3/4 || !c.shifts && tails > 0 || valid < 50 {
				t.Errorf("elements after the edit reused in %d of %d rounds", tails, valid)
			}
		})
	}
}

// TestDocumentResumesRepetitions checks that a reparse resumes long repetitions around the edit
// (core/resume.go), with results equal to parsing from scratch.
func TestDocumentResumesRepetitions(t *testing.T) {
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		t.Run(b.String(), func(t *testing.T) { testDocumentResumesRepetitions(t, b) })
	}
}
