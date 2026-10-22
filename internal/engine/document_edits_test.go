package engine

import (
	"fmt"
	"math/rand"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

//go:noinline
func documentVersionForCollection(t *testing.T, finalized chan<- struct{}) DocumentVersion {
	t.Helper()
	p := compile(t, `rule main = .*`)
	d, err := p.NewDocument("main", strings.Repeat("x", 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	runtime.SetFinalizer(d, func(*Document) { finalized <- struct{}{} })
	return d.Version()
}

func TestDocumentVersionDoesNotRetainDocument(t *testing.T) {
	finalized := make(chan struct{}, 1)
	version := documentVersionForCollection(t, finalized)
	defer runtime.KeepAlive(version)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		select {
		case <-finalized:
			if version.Sequence() != 1 {
				t.Fatal("retained token changed")
			}
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatal("version token retained its document")
}

func TestDocumentVersionTransitions(t *testing.T) {
	p := compile(t, `rule main = .*`)
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
	oldMax := maxEdits
	maxEdits = 1
	defer func() { maxEdits = oldMax }()
	d.edits = nil
	d.Edit(0, 0, "")
	d.Edit(0, 0, "")
	if d.Version().Sequence() != now.Sequence()+2 || len(d.edits) != 0 {
		t.Fatal("edit log reset changed public sequence")
	}
	d.sequence = ^uint64(0)
	last := d.Version()
	text := d.Text()
	memo := d.memo
	for _, call := range []func() error{func() error { return d.Edit(0, 0, "") }, func() error { return d.EditBatch(last, TextEdit{0, 0, ""}) }} {
		if err := call(); err == nil || d.Version() != last || d.Text() != text || d.memo != memo {
			t.Fatal("overflow was not atomic")
		}
	}
	if err := d.EditBatch(last); err != nil {
		t.Fatal("empty batch at last revision")
	}
}

func TestDocumentBatchValidationPreservesState(t *testing.T) {
	p := compile(t, `rule main = ("é" / .)* $$`)
	for _, large := range []bool{false, true} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			for _, back := range []Backend{Closure, Bytecode, BytecodeIterative} {
				text := "éab"
				if large {
					text += strings.Repeat("x", documentPieceMinSize)
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
				input := d.in
				memo, runs, stats, runGen, resumed := d.memo, d.runs, d.stats, d.runGen, d.resumed
				editsCopy := append([]docEdit(nil), d.edits...)
				rejects := [][]TextEdit{
					{{-1, 0, "x"}}, {{0, -1, "x"}}, {{0, d.in.loaded() + 1, "x"}},
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
					if d.Version() != beforeVersion || d.Text() != beforeText || d.memo != memo || !reflect.DeepEqual(d.runs, runs) || d.stats != stats || d.runGen != runGen || d.resumed != resumed || !reflect.DeepEqual(d.edits, editsCopy) || !reflect.DeepEqual(d.in, input) || dump(t, old, nil) != beforeTree || !reflect.DeepEqual(saved, changes) {
						t.Fatal("invalid batch changed observable or reusable state")
					}
				}
				// A no-op batch still commits once and discards reuse, without changing
				// the last parse's statistics or earlier returned result Nodes.
				if err := d.EditBatch(beforeVersion, TextEdit{0, 0, ""}); err != nil {
					t.Fatal(err)
				}
				if d.Version().Sequence() != beforeVersion.Sequence()+1 || d.memo == memo || d.runs != nil || d.edits != nil || d.stats != stats || dump(t, old, nil) != beforeTree {
					t.Fatal("batch publication contract")
				}
			}
		}
	}
}

func batchOracle(text string, unit PositionUnit, edits []TextEdit) string {
	if unit == Bytes {
		var out strings.Builder
		pos := 0
		for _, e := range edits {
			out.WriteString(text[pos:e.Start])
			out.WriteString(e.Text)
			pos = e.End
		}
		out.WriteString(text[pos:])
		return out.String()
	}
	input := []rune(text)
	out := []rune{}
	pos := 0
	for _, e := range edits {
		out = append(out, input[pos:e.Start]...)
		out = append(out, []rune(e.Text)...)
		pos = e.End
	}
	out = append(out, input[pos:]...)
	return string(out)
}

func TestDocumentBatchFreshParity(t *testing.T) {
	replacements := []string{"", "a", "\n", "é", "😀", "\xc3", "\xa9", "\xff", "@name\n", "x = 1+2\n"}
	for gi, grammar := range []string{`rule main = item* $$
rule item = .`, incrementalGrammar, `rule main = ("a" @error("need a") #recover(skip=.))* $$`} {
		original := compile(t, grammar)
		programs := []*Program{original}
		loaded := saveLoad(t, original, "main", CompiledDocument)
		programs = append(programs, loaded)
		for pi, p := range programs {
			for _, back := range []Backend{Closure, Bytecode, BytecodeIterative} {
				if pi == 1 && back == Closure {
					_, err := p.NewDocumentWith("main", "", ParseOptions{Backend: back})
					wantClosureRefused(t, err)
					continue
				}
				for _, unit := range []PositionUnit{CodePoints, Bytes} {
					for _, large := range []bool{false, true} {
						t.Run(fmt.Sprintf("%d/%d/%s/%s/%v", gi, pi, back, unit, large), func(t *testing.T) {
							initial := "é\xc3x\xa9\n"
							if large {
								initial += strings.Repeat("a", documentPieceMinSize)
							}
							d, err := p.NewDocumentWith("main", initial, ParseOptions{Backend: back, Unit: unit})
							if err != nil {
								t.Fatal(err)
							}
							rng := rand.New(rand.NewSource(39))
							for i := 0; i < 60; i++ {
								// Include an ordinary edit not yet parsed when the batch is captured.
								if i%3 == 0 {
									if err := d.Edit(0, 0, "a"); err != nil {
										t.Fatal(err)
									}
								}
								before := d.Text()
								positions := decodedBoundaries(before)
								if unit == CodePoints {
									for j := range positions {
										positions[j] = j
									}
								}
								var edits []TextEdit
								at := 0
								for j := 0; j < 3 && at < len(positions); j++ {
									start := at + rng.Intn(len(positions)-at)
									end := start + rng.Intn(min(3, len(positions)-start))
									edits = append(edits, TextEdit{positions[start], positions[end], replacements[rng.Intn(len(replacements))]})
									at = end + 1
								}
								wantText := batchOracle(before, unit, edits)
								base := d.Version()
								if err := d.EditBatch(base, edits...); err != nil {
									t.Fatal(err)
								}
								if d.Text() != wantText || d.Version().Sequence() != base.Sequence()+1 {
									t.Fatal("original-coordinate text/version mismatch")
								}
								got, err := d.Parse()
								// A document-mode program parses whole texts only when traced.
								want, werr := p.ParseWith("main", wantText, servedBy(CompiledDocument, ParseOptions{Backend: back, Unit: unit}))
								if dump(t, got, err) != dump(t, want, werr) {
									t.Fatalf("batch%d: %q got%s want%s", i, wantText, dump(t, got, err), dump(t, want, werr))
								}
							}
						})
					}
				}
			}
		}
	}
}

func TestDocumentBatchOriginalUTF8Boundaries(t *testing.T) {
	p := compile(t, `rule main = .*`)
	for _, prefix := range []string{"", strings.Repeat("a", documentPieceMinSize)} {
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
	if n, e := checkedDocumentLength(maxInt-1, 1); e != nil || n != maxInt {
		t.Fatal("maximum supported length")
	}
	for _, pair := range [][2]int{{maxInt, 1}, {-1, 0}, {0, -1}} {
		if _, err := checkedDocumentLength(pair[0], pair[1]); err == nil {
			t.Fatal("length overflow accepted")
		}
	}
	if utf8.RuneCountInString("\xff\xff") != 2 {
		t.Fatal("oracle precondition")
	}
}

func TestDocumentBatchPreservesPiecePromotion(t *testing.T) {
	if !documentPieceInput {
		t.Skip("piece input disabled by reference build")
	}
	p := compile(t, `rule main = .* $$`)
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		for _, promoted := range []bool{false, true} {
			text := strings.Repeat("x", documentPieceMinSize)
			if promoted {
				text = "x"
			}
			d, err := p.NewDocumentWith("main", text, ParseOptions{Unit: unit})
			if err != nil {
				t.Fatal(err)
			}
			if promoted {
				if err := d.Edit(1, 1, strings.Repeat("x", documentPieceMinSize)); err != nil {
					t.Fatal(err)
				}
			}
			if d.in.pieces == nil {
				t.Fatal("missing initial/promoted pieces")
			}
			if err := d.EditBatch(d.Version(), TextEdit{0, d.in.loaded(), "é"}); err != nil {
				t.Fatal(err)
			}
			if d.in.pieces == nil || d.Text() != "é" {
				t.Fatal("batch reversed promotion")
			}
			if err := d.Edit(d.in.loaded(), d.in.loaded(), "z"); err != nil {
				t.Fatal(err)
			}
			if d.in.pieces == nil || d.Text() != "éz" {
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
