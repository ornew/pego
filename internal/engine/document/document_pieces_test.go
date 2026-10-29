package document_test

import (
	"fmt"
	"strings"
	"testing"

	. "github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/value"
)

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
			if core.DocumentPieceInput && doc.Input().Pieces == nil {
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
	if core.DocumentPieceInput && doc.Input().Pieces == nil {
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
				pos := doc.Input().Loaded()
				if err := doc.Edit(pos, pos, "a"); err != nil {
					t.Fatal(err)
				}
			}
			if got := len(doc.Input().Pieces.Parts); got != 1 {
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

func TestDocumentPiecesCrossBoundaryParsing(t *testing.T) {
	prefix := strings.Repeat("x", core.DocumentPieceMinSize)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		t.Run(backend.String(), func(t *testing.T) {
			program := mustCompile(t, `rule main = "x"* "aé" "b" $$`)
			doc, err := program.NewDocumentWith("main", prefix+"a\xc3b", ParseOptions{Backend: backend, Unit: Bytes})
			if err != nil {
				t.Fatal(err)
			}
			if core.DocumentPieceInput && doc.Input().Pieces == nil {
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
				if doc.Input().Pieces != nil {
					t.Fatal("small Document unexpectedly uses piece storage")
				}
				if _, err := doc.Parse(); err != nil {
					t.Fatalf("initial flat parse: %v", err)
				}
				if err := doc.Edit(len(initial), len(initial), "b"); err != nil {
					t.Fatal(err)
				}
				if core.DocumentPieceInput && doc.Input().Pieces == nil {
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
	if doc.Input().Pieces != nil {
		t.Fatal("byte length alone selected pieces for a small code-point Document")
	}
}
