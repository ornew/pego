package engine

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/value"
)

func TestDocumentPiecesMatchFlatEdits(t *testing.T) {
	insertions := []string{"", "a", "é", "日", "😀", "\n", "\xc3", "\xa9", "\xff", "\xe6\xbc"}
	for _, unit := range []PositionUnit{Bytes, CodePoints} {
		t.Run(unit.String(), func(t *testing.T) {
			const initial = "first\né日\xff\nlast"
			d := core.NewDocumentPieces(initial, unit)
			flatBytes := []byte(initial)
			flatRunes := []rune(initial)
			rng := rand.New(rand.NewSource(76))
			for edit := range 600 {
				n := len(flatBytes)
				if unit == CodePoints {
					n = len(flatRunes)
				}
				start := rng.Intn(n + 1)
				end := start + rng.Intn(n-start+1)
				insert := insertions[rng.Intn(len(insertions))]
				var wantDelta int
				if unit == Bytes {
					wantDelta = len(insert) - (end - start)
					flatBytes = slices.Replace(flatBytes, start, end, []byte(insert)...)
				} else {
					runes := []rune(insert)
					wantDelta = len(runes) - (end - start)
					flatRunes = slices.Replace(flatRunes, start, end, runes...)
				}
				if got := d.Replace(start, end, insert); got != wantDelta {
					t.Fatalf("edit %d: delta %d, want %d", edit, got, wantDelta)
				}
				want := string(flatBytes)
				if unit == CodePoints {
					want = string(flatRunes)
				}
				if got := d.Text(0, d.N); got != want {
					t.Fatalf("edit %d: text %x, want %x", edit, got, want)
				}
				if edit%17 == 0 {
					for pos := 0; pos < d.N; pos++ {
						got, ok := d.At(pos)
						var expected rune
						if unit == Bytes {
							expected = rune(flatBytes[pos])
						} else {
							expected = flatRunes[pos]
						}
						if !ok || got != expected {
							t.Fatalf("edit %d position %d: %U, want %U", edit, pos, got, expected)
						}
					}
					if d.N > 0 {
						a := rng.Intn(d.N)
						b := a + rng.Intn(d.N-a+1)
						wantRange := string(flatBytes[a:b])
						if unit == CodePoints {
							wantRange = string(flatRunes[a:b])
						}
						if got := d.Text(a, b); got != wantRange {
							t.Fatalf("edit %d range [%d,%d): %x, want %x", edit, a, b, got, wantRange)
						}
					}
				}
			}
		})
	}
}

func TestDocumentPiecesCrossBoundaryParsing(t *testing.T) {
	prefix := strings.Repeat("x", core.DocumentPieceMinSize)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		t.Run(backend.String(), func(t *testing.T) {
			program := mustCompile(t, `rule main = "x"* "aé" "b" $$`)
			doc, err := program.NewDocumentWith("main", prefix+"a\xc3b", ParseOptions{Backend: backend, Unit: Bytes})
			if err != nil {
				t.Fatal(err)
			}
			if core.DocumentPieceInput && doc.in.Pieces == nil {
				t.Fatal("expected piece storage")
			}
			if err := doc.Edit(len(prefix)+2, len(prefix)+2, "\xa9"); err != nil {
				t.Fatal(err)
			}
			if got := doc.Text(); got != prefix+"aéb" {
				t.Fatalf("joined text: %x", got)
			}
			if err := doc.Edit(len(prefix)+2, len(prefix)+2, "x"); err == nil {
				t.Fatal("accepted an edit inside a rune spanning pieces")
			}
			got, err := doc.Parse()
			want, werr := program.ParseWith("main", prefix+"aéb", ParseOptions{Backend: backend, Unit: Bytes})
			if resultJSON(got, err) != resultJSON(want, werr) {
				t.Fatalf("cross-piece literal: got %s, want %s", resultJSON(got, err), resultJSON(want, werr))
			}
		})
	}
}

func TestDocumentPiecesCodePointNormalizationAndNodeText(t *testing.T) {
	program := mustCompile(t, `rule main Word = ([a-z]+ $$) #atomic
type Word = node[string]`)
	prefix := strings.Repeat("x", core.DocumentPieceMinSize)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		t.Run(backend.String(), func(t *testing.T) {
			doc, err := program.NewDocumentWith("main", prefix+"abef", ParseOptions{Backend: backend})
			if err != nil {
				t.Fatal(err)
			}
			if core.DocumentPieceInput && doc.in.Pieces == nil {
				t.Fatal("expected piece storage")
			}
			if err := doc.Edit(len(prefix)+2, len(prefix)+2, "cd"); err != nil {
				t.Fatal(err)
			}
			old, err := doc.Parse()
			if err != nil || value.D(old).Text != prefix+"abcdef" {
				t.Fatalf("cross-piece node text: %v, %v", old, err)
			}
			if err := doc.Edit(0, 0, "z"); err != nil {
				t.Fatal(err)
			}
			if _, err := doc.Parse(); err != nil || value.D(old).Text != prefix+"abcdef" {
				t.Fatalf("retained node text changed: %v, %v", old, err)
			}
		})
	}

	invalid := mustCompile(t, `rule main = "x"* "��" $$`)
	doc, err := invalid.NewDocument("main", prefix+"\xc3")
	if err != nil {
		t.Fatal(err)
	}
	if core.DocumentPieceInput && doc.in.Pieces == nil {
		t.Fatal("expected piece storage")
	}
	if err := doc.Edit(len(prefix)+1, len(prefix)+1, "\xa9"); err != nil {
		t.Fatal(err)
	}
	if got := doc.Text(); got != prefix+"��" {
		t.Fatalf("code-point pieces re-decoded across edit: %x", got)
	}
	got, err := doc.Parse()
	want, werr := invalid.ParseWith("main", prefix+"��", ParseOptions{})
	if resultJSON(got, err) != resultJSON(want, werr) {
		t.Fatal(fmt.Sprintf("normalized parse: got %s, want %s", resultJSON(got, err), resultJSON(want, werr)))
	}
}

func TestDocumentPieceLargeByteOffsets(t *testing.T) {
	// Model a slice of a >2 GiB code-point chunk without allocating that input.
	p := core.DocumentPiece{
		Text: "éa", Runes: []rune{'é', 'a'},
		LargeOffs: []int{1<<31 + 4, 1<<31 + 6, 1<<31 + 7},
	}
	if got := p.Substring(0, 1, CodePoints); got != "é" {
		t.Fatalf("substring: %q", got)
	}
	sub := p.Sub(1, 2, CodePoints)
	if got := sub.Substring(0, 1, CodePoints); got != "a" {
		t.Fatalf("sliced large-offset piece: %q", got)
	}
}

func TestDocumentPiecesSmallDocumentPromotion(t *testing.T) {
	program := mustCompile(t, `rule main = [a-z]+ $$`)
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			t.Run(unit.String()+"/"+backend.String(), func(t *testing.T) {
				initial := strings.Repeat("a", core.DocumentPieceMinSize-1)
				opts := ParseOptions{Unit: unit, Backend: backend}
				doc, err := program.NewDocumentWith("main", initial, opts)
				if err != nil {
					t.Fatal(err)
				}
				if doc.in.Pieces != nil {
					t.Fatal("small Document unexpectedly uses piece storage")
				}
				if _, err := doc.Parse(); err != nil {
					t.Fatalf("initial flat parse: %v", err)
				}
				if err := doc.Edit(len(initial), len(initial), "b"); err != nil {
					t.Fatal(err)
				}
				if core.DocumentPieceInput && doc.in.Pieces == nil {
					t.Fatal("Document did not promote at threshold")
				}
				got, err := doc.Parse()
				want, werr := program.ParseWith("main", initial+"b", opts)
				if resultJSON(got, err) != resultJSON(want, werr) {
					t.Fatalf("promoted parse: got %s, want %s", resultJSON(got, err), resultJSON(want, werr))
				}
			})
		}
	}

	invalid := mustCompile(t, `rule main = "a"* "b"? "�" $$`)
	initial := strings.Repeat("a", core.DocumentPieceMinSize-2) + "\xff"
	doc, err := invalid.NewDocument("main", initial)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Parse(); err != nil {
		t.Fatalf("initial invalid UTF-8 parse: %v", err)
	}
	if err := doc.Edit(core.DocumentPieceMinSize-2, core.DocumentPieceMinSize-2, "b"); err != nil {
		t.Fatal(err)
	}
	got, err := doc.Parse()
	want, werr := invalid.ParseWith("main", strings.Repeat("a", core.DocumentPieceMinSize-2)+"b�", ParseOptions{})
	if resultJSON(got, err) != resultJSON(want, werr) {
		t.Fatalf("promoted invalid UTF-8 parse: got %s, want %s", resultJSON(got, err), resultJSON(want, werr))
	}

	// The decision uses logical code points, not UTF-8 byte length.
	wide := strings.Repeat("é", core.DocumentPieceMinSize/2)
	doc, err = program.NewDocumentWith("main", wide, ParseOptions{Unit: CodePoints})
	if err != nil {
		t.Fatal(err)
	}
	if doc.in.Pieces != nil {
		t.Fatal("byte length alone selected pieces for a small code-point Document")
	}
}

func TestDocumentPiecesCompactionPreservesEarlierNodeText(t *testing.T) {
	if !core.DocumentPieceInput {
		t.Skip("piece storage is disabled")
	}
	program := mustCompile(t, `rule main Word = ([a-z]+ $$) #atomic
type Word = node[string]`)
	initial := strings.Repeat("a", core.DocumentPieceMinSize)
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		t.Run(unit.String(), func(t *testing.T) {
			doc, err := program.NewDocumentWith("main", initial, ParseOptions{Unit: unit})
			if err != nil {
				t.Fatal(err)
			}
			old, err := doc.Parse()
			if err != nil || value.D(old).Text != initial {
				t.Fatalf("initial parse: %v, %v", old, err)
			}
			for range 512 {
				pos := doc.in.Loaded()
				if err := doc.Edit(pos, pos, "a"); err != nil {
					t.Fatal(err)
				}
			}
			if got := len(doc.in.Pieces.Parts); got != 1 {
				t.Fatalf("expected compaction after 512 edits, got %d pieces", got)
			}
			current, err := doc.Parse()
			if err != nil || value.D(current).Text != strings.Repeat("a", core.DocumentPieceMinSize+512) {
				t.Fatalf("post-compaction parse: %v, %v", current, err)
			}
			if value.D(old).Text != initial {
				t.Fatalf("retained node text changed: %q", value.D(old).Text)
			}
		})
	}
}
