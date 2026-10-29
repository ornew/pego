package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/gentest"
)

func TestDocumentSeedDependencyCompletion(t *testing.T) {
	t.Parallel()
	for _, imported := range []bool{false, true} {
		p := &core.Parser{Memo: core.NewMemoTable(), MemoAll: true}
		r := &core.Rule{ID: 1, Leader: true}
		st, _, _, hit := p.CallBegin(r, 0)
		if hit {
			t.Fatal("new leader unexpectedly memoized")
		}
		g := p.GrowBegin(st.Key)
		if imported {
			// This helper was computed under an earlier head in this generation.
			e := p.Memo.Alloc()
			*e = core.MemoEntry{Ok: true, Provisional: true}
			p.Memo.Put(core.MemoKey{Rule: 2}, e)
			r = &core.Rule{ID: 2}
		}
		if _, _, _, hit := p.CallBegin(r, 0); !hit {
			t.Fatal("seed or imported helper was not reused")
		}
		r = &core.Rule{ID: 1, Leader: true}
		v, ok := p.GrowEnd(r, &g)
		p.CallEnd(r, &st, v, ok)
		e, _ := p.Memo.Get(st.Key)
		if e.Provisional != imported || p.Provisional != 1 || p.MemoSeedUses != 0 {
			t.Fatalf("imported=%v: provisional=%v, seed uses=%d, repetition uses=%d", imported, e.Provisional, p.MemoSeedUses, p.Provisional)
		}
		if valid := core.AdvanceEntry(e, []core.DocEdit{{Start: 10, End: 10, Delta: 1}}); valid == imported {
			t.Fatalf("imported=%v: entry validity after an unrelated edit=%v", imported, valid)
		}
	}
	t.Run("nested head retains outer dependency", func(t *testing.T) {
		p := &core.Parser{Memo: core.NewMemoTable(), MemoAll: true}
		outer, inner := &core.Rule{ID: 1, Leader: true}, &core.Rule{ID: 2, Leader: true}
		outerCall, _, _, _ := p.CallBegin(outer, 0)
		outerGrow := p.GrowBegin(outerCall.Key)
		innerCall, _, _, _ := p.CallBegin(inner, 0)
		innerGrow := p.GrowBegin(innerCall.Key)
		if _, _, _, hit := p.CallBegin(outer, 0); !hit {
			t.Fatal("outer seed was not reused")
		}
		v, ok := p.GrowEnd(inner, &innerGrow)
		p.CallEnd(inner, &innerCall, v, ok)
		e, _ := p.Memo.Get(innerCall.Key)
		if !e.Provisional || p.MemoProvisionalUses == 0 {
			t.Fatal("inner completion discharged an unresolved outer seed")
		}
		v, ok = p.GrowEnd(outer, &outerGrow)
		p.CallEnd(outer, &outerCall, v, ok)
		e, _ = p.Memo.Get(outerCall.Key)
		if !e.Provisional || p.ActiveSeed != nil || p.MemoSeedUses != 0 {
			t.Fatal("outer completion lost the intermediate dependency")
		}
	})
	t.Run("nested own seeds are resolved", func(t *testing.T) {
		p := &core.Parser{Memo: core.NewMemoTable(), MemoAll: true}
		outer, inner := &core.Rule{ID: 1, Leader: true}, &core.Rule{ID: 2, Leader: true}
		outerCall, _, _, _ := p.CallBegin(outer, 0)
		outerGrow := p.GrowBegin(outerCall.Key)
		innerCall, _, _, _ := p.CallBegin(inner, 0)
		innerGrow := p.GrowBegin(innerCall.Key)
		p.CallBegin(inner, 0)
		p.Pos = 1
		if !p.GrowStep(&innerGrow, nil, true) || p.ActiveSeed != innerGrow.Best {
			t.Fatal("active seed did not advance with growth")
		}
		p.CallBegin(inner, 0)
		v, ok := p.GrowEnd(inner, &innerGrow)
		p.CallEnd(inner, &innerCall, v, ok)
		e, _ := p.Memo.Get(innerCall.Key)
		if e.Provisional || p.ActiveSeed != outerGrow.Best {
			t.Fatal("inner own-seed dependency survived completion")
		}
		p.Pos = 0
		p.CallBegin(outer, 0)
		v, ok = p.GrowEnd(outer, &outerGrow)
		p.CallEnd(outer, &outerCall, v, ok)
		e, _ = p.Memo.Get(outerCall.Key)
		if e.Provisional || p.ActiveSeed != nil || p.Provisional != 3 {
			t.Fatal("outer completion failed to resolve own seeds or changed repetition tracking")
		}
	})
	t.Run("fresh intermediate dependency survives completion", func(t *testing.T) {
		p := &core.Parser{Memo: core.NewMemoTable(), MemoAll: true}
		head, helper := &core.Rule{ID: 1, Leader: true}, &core.Rule{ID: 2}
		headCall, _, _, _ := p.CallBegin(head, 0)
		growth := p.GrowBegin(headCall.Key)
		helperCall, _, _, hit := p.CallBegin(helper, 0)
		if hit {
			t.Fatal("fresh helper unexpectedly memoized")
		}
		p.CallBegin(head, 0) // The fresh helper reads the active seed.
		p.CallEnd(helper, &helperCall, nil, false)
		e, _ := p.Memo.Get(helperCall.Key)
		if !e.Provisional {
			t.Fatal("fresh intermediate dependency was not recorded")
		}
		v, ok := p.GrowEnd(head, &growth)
		p.CallEnd(head, &headCall, v, ok)
		e, _ = p.Memo.Get(headCall.Key)
		if !e.Provisional || p.MemoProvisionalUses == 0 {
			t.Fatal("head completion discharged the fresh helper dependency")
		}
	})
}

// BenchmarkDocumentLeftRecursion measures edits to the last line of a thousand
// direct or indirect left-recursive expressions.
func BenchmarkDocumentLeftRecursion(b *testing.B) {
	for _, kind := range []string{"direct", "indirect", "nested"} {
		src := `rule main = (expr "\n")* $$
rule expr = expr "+" num / num
rule num = [0-9]+ #atomic`
		if kind == "indirect" {
			src = `rule main = (expr "\n")* $$
rule expr = sum / num
rule sum = expr "+" num
rule num = [0-9]+ #atomic`
		}
		if kind == "nested" {
			src = `rule main = (expr "\n")* $$
rule expr = expr "+" term / term
rule term = term "*" num / num
rule num = [0-9]+ #atomic`
		}
		prog, err := Compile(mustParse(src), compile.Options{})
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
				if gentest.ResultJSON(got, gotErr) != gentest.ResultJSON(want, wantErr) {
					b.Fatal("edited Document differs from a fresh parse")
				}
			})
		}
	}
}
