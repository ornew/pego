package engine

import (
	"math/rand"
	"strings"
	"testing"
)

func TestDocumentIndirectLeftRecursionEdits(t *testing.T) {
	defer func(n int) { maxEdits = n }(maxEdits)
	maxEdits = 17
	const src = `
def main = (l:(((?^\n) / (!(?ab) r2))) "\n" / r1 / m:(?^\n)* "\n")* (?^\n)* $$
def r1 = ((r2 / "b")){0,2}
def r2 = (r1 / (r2 "a" "b"))`
	for _, options := range []Options{{}, {DisableMemo: true}} {
		prog := compile(t, src, options)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []Unit{CodePoints, Bytes} {
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

func TestDocumentSeedDependencyCompletion(t *testing.T) {
	for _, imported := range []bool{false, true} {
		p := &parser{memo: newMemoTable(), memoAll: true}
		r := &rule{id: 1, leader: true}
		st, _, _, hit := p.callBegin(r, 0)
		if hit {
			t.Fatal("new leader unexpectedly memoized")
		}
		g := p.growBegin(st.key)
		if imported {
			// This helper was computed under an earlier head in this generation.
			e := p.memo.alloc()
			*e = memoEntry{ok: true, provisional: true}
			p.memo.put(memoKey{rule: 2}, e)
			r = &rule{id: 2}
		}
		if _, _, _, hit := p.callBegin(r, 0); !hit {
			t.Fatal("seed or imported helper was not reused")
		}
		r = &rule{id: 1, leader: true}
		v, ok := p.growEnd(r, &g)
		p.callEnd(r, &st, v, ok)
		e, _ := p.memo.get(st.key)
		if e.provisional != imported || p.provisional != 1 || p.memoSeedUses != 0 {
			t.Fatalf("imported=%v: provisional=%v, seed uses=%d, repetition uses=%d", imported, e.provisional, p.memoSeedUses, p.provisional)
		}
		if valid := advanceEntry(e, []docEdit{{start: 10, end: 10, delta: 1}}); valid == imported {
			t.Fatalf("imported=%v: entry validity after an unrelated edit=%v", imported, valid)
		}
	}
	t.Run("nested head retains outer dependency", func(t *testing.T) {
		p := &parser{memo: newMemoTable(), memoAll: true}
		outer, inner := &rule{id: 1, leader: true}, &rule{id: 2, leader: true}
		outerCall, _, _, _ := p.callBegin(outer, 0)
		outerGrow := p.growBegin(outerCall.key)
		innerCall, _, _, _ := p.callBegin(inner, 0)
		innerGrow := p.growBegin(innerCall.key)
		if _, _, _, hit := p.callBegin(outer, 0); !hit {
			t.Fatal("outer seed was not reused")
		}
		v, ok := p.growEnd(inner, &innerGrow)
		p.callEnd(inner, &innerCall, v, ok)
		e, _ := p.memo.get(innerCall.key)
		if !e.provisional || p.memoProvisionalUses == 0 {
			t.Fatal("inner completion discharged an unresolved outer seed")
		}
		v, ok = p.growEnd(outer, &outerGrow)
		p.callEnd(outer, &outerCall, v, ok)
		e, _ = p.memo.get(outerCall.key)
		if !e.provisional || p.activeSeed != nil || p.memoSeedUses != 0 {
			t.Fatal("outer completion lost the intermediate dependency")
		}
	})
	t.Run("nested own seeds are resolved", func(t *testing.T) {
		p := &parser{memo: newMemoTable(), memoAll: true}
		outer, inner := &rule{id: 1, leader: true}, &rule{id: 2, leader: true}
		outerCall, _, _, _ := p.callBegin(outer, 0)
		outerGrow := p.growBegin(outerCall.key)
		innerCall, _, _, _ := p.callBegin(inner, 0)
		innerGrow := p.growBegin(innerCall.key)
		p.callBegin(inner, 0)
		p.pos = 1
		if !p.growStep(&innerGrow, nil, true) || p.activeSeed != innerGrow.best {
			t.Fatal("active seed did not advance with growth")
		}
		p.callBegin(inner, 0)
		v, ok := p.growEnd(inner, &innerGrow)
		p.callEnd(inner, &innerCall, v, ok)
		e, _ := p.memo.get(innerCall.key)
		if e.provisional || p.activeSeed != outerGrow.best {
			t.Fatal("inner own-seed dependency survived completion")
		}
		p.pos = 0
		p.callBegin(outer, 0)
		v, ok = p.growEnd(outer, &outerGrow)
		p.callEnd(outer, &outerCall, v, ok)
		e, _ = p.memo.get(outerCall.key)
		if e.provisional || p.activeSeed != nil || p.provisional != 3 {
			t.Fatal("outer completion failed to resolve own seeds or changed repetition tracking")
		}
	})
	t.Run("fresh intermediate dependency survives completion", func(t *testing.T) {
		p := &parser{memo: newMemoTable(), memoAll: true}
		head, helper := &rule{id: 1, leader: true}, &rule{id: 2}
		headCall, _, _, _ := p.callBegin(head, 0)
		growth := p.growBegin(headCall.key)
		helperCall, _, _, hit := p.callBegin(helper, 0)
		if hit {
			t.Fatal("fresh helper unexpectedly memoized")
		}
		p.callBegin(head, 0) // The fresh helper reads the active seed.
		p.callEnd(helper, &helperCall, nil, false)
		e, _ := p.memo.get(helperCall.key)
		if !e.provisional {
			t.Fatal("fresh intermediate dependency was not recorded")
		}
		v, ok := p.growEnd(head, &growth)
		p.callEnd(head, &headCall, v, ok)
		e, _ = p.memo.get(headCall.key)
		if !e.provisional || p.memoProvisionalUses == 0 {
			t.Fatal("head completion discharged the fresh helper dependency")
		}
	})
}

// BenchmarkDocumentLeftRecursion measures edits to the last line of a thousand
// direct or indirect left-recursive expressions.
func BenchmarkDocumentLeftRecursion(b *testing.B) {
	for _, kind := range []string{"direct", "indirect", "nested"} {
		src := `def main = (expr "\n")* $$
def expr = expr "+" num / num
def num = @(?0-9)+`
		if kind == "indirect" {
			src = `def main = (expr "\n")* $$
def expr = sum / num
def sum = expr "+" num
def num = @(?0-9)+`
		}
		if kind == "nested" {
			src = `def main = (expr "\n")* $$
def expr = expr "+" term / term
def term = term "*" num / num
def num = @(?0-9)+`
		}
		prog, err := Compile(mustParse(src), Options{})
		if err != nil {
			b.Fatal(err)
		}
		text := strings.Repeat("1+2+3\n", 1000)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			b.Run(kind+"/"+backend.String(), func(b *testing.B) {
				doc, err := prog.NewDocumentWith("main", text, ParseOptions{Backend: backend})
				if err != nil {
					b.Fatal(err)
				}
				if _, err := doc.Parse(); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				i := 0
				for b.Loop() {
					if err := doc.Edit(len(text)-2, len(text)-1, string(rune('3'+i%2))); err != nil {
						b.Fatal(err)
					}
					i++
					if _, err := doc.Parse(); err != nil {
						b.Fatal(err)
					}
				}
				got, gotErr := doc.Parse()
				want, wantErr := prog.ParseWith("main", doc.Text(), ParseOptions{Backend: backend})
				if resultJSON(got, gotErr) != resultJSON(want, wantErr) {
					b.Fatal("edited Document differs from a fresh parse")
				}
			})
		}
	}
}
