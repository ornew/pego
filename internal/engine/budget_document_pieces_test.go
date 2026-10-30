//go:build !pego_budget_controls_reference

package engine

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
)

func TestBudgetDocumentPieceConstruction(t *testing.T) {
	t.Parallel()
	if !core.DocumentPieceInput {
		t.Skip("piece input is disabled")
	}
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		for _, text := range []string{strings.Repeat("é😀x", 2048), strings.Repeat("x\xff", 4096)} {
			c := core.NewBudgetControl(nil, limits.Limits{Budget: 1 << 30})
			in := core.NewControlledDocumentInput(text, unit, c)
			want := core.NewDocumentPieces(text, unit)
			if in.Pieces == nil || !reflect.DeepEqual(in.Pieces, want) {
				t.Fatalf("%s: controlled preparation changed piece contents", unit)
			}
			if unit == Bytes {
				if c.Used != 0 {
					t.Fatal("immutable byte string retention was charged as a copy")
				}
			} else if c.Used == 0 {
				t.Fatal("code point decoding bypassed construction counters")
			}
		}
	}
	c := core.NewBudgetControl(nil, limits.Limits{Budget: 1})
	if err := catchFatal(t, func() { core.NewControlledDocumentInput(strings.Repeat("a", 8192), CodePoints, c) }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("piece preparation interruption: %v", err)
	}
}

func TestBudgetDocumentPieceTextPublication(t *testing.T) {
	t.Parallel()
	if !core.DocumentPieceInput {
		t.Skip("piece input is disabled")
	}
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		pieces := core.NewDocumentPieces(strings.Repeat("x", 8192), unit)
		pieces.Replace(4096, 4097, "é")
		in := core.Input{Unit: unit, Eof: true, Pieces: pieces, Control: core.NewBudgetControl(nil, limits.Limits{Budget: 4096})}
		if err := catchFatal(t, func() { in.Text(0, pieces.N) }); !errors.Is(err, limits.ErrLimit) {
			t.Fatalf("%s: piece materialization bypassed the limit: %v", unit, err)
		}
		if pieces.FullOK || pieces.Full != "" {
			t.Fatal("interrupted materialization published a cache")
		}
		in.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 1 << 30})
		got := in.Text(0, pieces.N)
		if want := strings.Repeat("x", 4096) + "é" + strings.Repeat("x", 4095); got != want || !pieces.FullOK {
			t.Fatalf("%s: retry materialization differs", unit)
		}
	}
}
