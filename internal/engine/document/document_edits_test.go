package document_test

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego/internal/engine/document"

	. "github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/engine/core"
)

func TestDocumentBatchOriginalUTF8Boundaries(t *testing.T) {
	p := mustCompile(t, `rule main = .*`)
	for _, prefix := range []string{"", strings.Repeat("a", core.DocumentPieceMinSize)} {
		text := prefix + "\xc3x\xa9"
		d, _ := p.NewDocumentWith("main", text, ParseOptions{Unit: Bytes})
		n := len(prefix)
		// Deleting x joins the original invalid bytes. The later edit remains valid
		// in original coordinates although it would be interior in sequential text.
		edits := []TextEdit{{n + 1, n + 2, ""}, {n + 2, n + 3, "\xa9"}}
		if err := d.EditBatch(d.Version(), edits...); err != nil {
			t.Fatal(err)
		}
		if d.Text() != prefix+"é" {
			t.Fatalf("joined bytes: %x", d.Text())
		}
	}
	maxInt := int(^uint(0) >> 1)
	if n, e := document.CheckedDocumentLength(maxInt-1, 1); e != nil || n != maxInt {
		t.Fatal("maximum supported length")
	}
	for _, pair := range [][2]int{{maxInt, 1}, {-1, 0}, {0, -1}} {
		if _, err := document.CheckedDocumentLength(pair[0], pair[1]); err == nil {
			t.Fatal("length overflow accepted")
		}
	}
	if utf8.RuneCountInString("\xff\xff") != 2 {
		t.Fatal("oracle precondition")
	}
}

func TestDocumentBatchPreservesPiecePromotion(t *testing.T) {
	if !core.DocumentPieceInput {
		t.Skip("piece input disabled by reference build")
	}
	p := mustCompile(t, `rule main = .* $$`)
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		for _, promoted := range []bool{false, true} {
			text := strings.Repeat("x", core.DocumentPieceMinSize)
			if promoted {
				text = "x"
			}
			d, err := p.NewDocumentWith("main", text, ParseOptions{Unit: unit})
			if err != nil {
				t.Fatal(err)
			}
			if promoted {
				if err := d.Edit(1, 1, strings.Repeat("x", core.DocumentPieceMinSize)); err != nil {
					t.Fatal(err)
				}
			}
			if d.Input().Pieces == nil {
				t.Fatal("missing initial/promoted pieces")
			}
			if err := d.EditBatch(d.Version(), TextEdit{0, d.Input().Loaded(), "é"}); err != nil {
				t.Fatal(err)
			}
			if d.Input().Pieces == nil || d.Text() != "é" {
				t.Fatal("batch reversed promotion")
			}
			if err := d.Edit(d.Input().Loaded(), d.Input().Loaded(), "z"); err != nil {
				t.Fatal(err)
			}
			if d.Input().Pieces == nil || d.Text() != "éz" {
				t.Fatal("ordinary edit reversed promotion")
			}
			old, err := d.Parse()
			fresh, fe := p.ParseWith("main", "éz", ParseOptions{Unit: unit})
			if dump(t, old, err) != dump(t, fresh, fe) {
				t.Fatal("shrink fresh parity")
			}
		}
	}
}

func TestDocumentBatchValidationPreservesState(t *testing.T) {
	p := mustCompile(t, `rule main = ("é" / .)* $$`)
	for _, large := range []bool{false, true} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			for _, back := range []Backend{Closure, Bytecode, BytecodeIterative} {
				text := "éab"
				if large {
					text += strings.Repeat("x", core.DocumentPieceMinSize)
				}
				d, err := p.NewDocumentWith("main", text, ParseOptions{Unit: unit, Backend: back})
				if err != nil {
					t.Fatal(err)
				}
				old, e := d.Parse()
				if e != nil {
					t.Fatal(e)
				}
				beforeTree := dump(t, old, e)
				beforeVersion, beforeText := d.Version(), d.Text()
				input := *d.Input()
				memo, runs, stats, runGen, resumed := d.Memo(), d.Runs(), d.Stats(), d.RunGen(), d.Resumed()
				editsCopy := append([]core.DocEdit(nil), d.Edits()...)
				rejects := [][]TextEdit{
					{{-1, 0, "x"}}, {{0, -1, "x"}}, {{0, d.Input().Loaded() + 1, "x"}},
					{{1, 1, "x"}, {0, 0, "y"}}, {{0, 2, "x"}, {1, 3, "y"}}, {{0, 0, "x"}, {0, 1, "y"}},
				}
				if unit == Bytes {
					rejects = append(rejects, []TextEdit{{0, 0, "x"}, {1, 1, "y"}})
				}
				for _, changes := range rejects {
					saved := append([]TextEdit(nil), changes...)
					if err := d.EditBatch(beforeVersion, changes...); err == nil || !strings.Contains(err.Error(), "edit ") {
						t.Fatal("invalid edits accepted or unindexed error", changes, err)
					}
					if d.Version() != beforeVersion || d.Text() != beforeText || d.Memo() != memo || !reflect.DeepEqual(d.Runs(), runs) || d.Stats() != stats || d.RunGen() != runGen || d.Resumed() != resumed || !reflect.DeepEqual(d.Edits(), editsCopy) || !reflect.DeepEqual(*d.Input(), input) || dump(t, old, nil) != beforeTree || !reflect.DeepEqual(saved, changes) {
						t.Fatal("invalid batch changed observable or reusable state")
					}
				}
				// A no-op batch still commits once and discards reuse, without changing
				// the last parse's statistics or earlier returned result Nodes.
				if err := d.EditBatch(beforeVersion, TextEdit{0, 0, ""}); err != nil {
					t.Fatal(err)
				}
				if d.Version().Sequence() != beforeVersion.Sequence()+1 || d.Memo() == memo || d.Runs() != nil || d.Edits() != nil || d.Stats() != stats || dump(t, old, nil) != beforeTree {
					t.Fatal("batch publication contract")
				}
			}
		}
	}
}

func TestDocumentVersionTransitions(t *testing.T) {
	p := mustCompile(t, `rule main = .*`)
	d, _ := p.NewDocument("main", "abc")
	other, _ := p.NewDocument("main", "abc")
	first := d.Version()
	if first.Sequence() != 1 || first == other.Version() || first == (DocumentVersion{}) || d.Unit() != CodePoints {
		t.Fatal("initial identity/unit")
	}
	for i := 0; i < 2; i++ {
		d.Parse()
		if d.Version() != first {
			t.Fatal("parse advanced version")
		}
	}
	if err := d.EditBatch(first); err != nil || d.Version() != first {
		t.Fatal("empty batch changed revision")
	}
	for _, edit := range []TextEdit{{0, 0, ""}, {0, 1, "a"}, {1, 2, "x"}} {
		before := d.Version()
		if err := d.Edit(edit.Start, edit.End, edit.Text); err != nil {
			t.Fatal(err)
		}
		if d.Version().Sequence() != before.Sequence()+1 || before == d.Version() {
			t.Fatal("single edit must advance exactly once")
		}
	}
	now := d.Version()
	for _, v := range []DocumentVersion{{}, first, other.Version()} {
		if err := d.EditBatch(v); err == nil || d.Version() != now {
			t.Fatal("accepted zero/stale/foreign empty batch")
		}
	}
	defer document.SetMaxEdits(1)()
	d.SetEdits(nil)
	d.Edit(0, 0, "")
	d.Edit(0, 0, "")
	if d.Version().Sequence() != now.Sequence()+2 || len(d.Edits()) != 0 {
		t.Fatal("edit log reset changed public sequence")
	}
	d.SetSequence(^uint64(0))
	last := d.Version()
	text := d.Text()
	memo := d.Memo()
	for _, call := range []func() error{func() error { return d.Edit(0, 0, "") }, func() error { return d.EditBatch(last, TextEdit{0, 0, ""}) }} {
		if err := call(); err == nil || d.Version() != last || d.Text() != text || d.Memo() != memo {
			t.Fatal("overflow was not atomic")
		}
	}
	if err := d.EditBatch(last); err != nil {
		t.Fatal("empty batch at last revision")
	}
}
