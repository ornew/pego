//go:build !pego_work_controls_reference

package engine

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/limits"
)

func TestWorkDocumentPieceConstruction(t *testing.T) {
	if !documentPieceInput {
		t.Skip("piece input is disabled")
	}
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		for _, text := range []string{strings.Repeat("é😀x", 2048), strings.Repeat("x\xff", 4096)} {
			c := newWorkControl(nil, limits.WorkLimits{MaxSteps: 1 << 30})
			in := newControlledDocumentInput(text, unit, c)
			want := newDocumentPieces(text, unit)
			if in.pieces == nil || !reflect.DeepEqual(in.pieces, want) {
				t.Fatalf("%s: controlled preparation changed piece contents", unit)
			}
			if unit == Bytes {
				if c.inputUsed != 0 || c.stepsUsed != 0 {
					t.Fatal("immutable byte string retention was charged as a copy")
				}
			} else if c.inputUsed == 0 || c.stepsUsed == 0 {
				t.Fatal("code point decoding bypassed construction counters")
			}
		}
	}
	c := newWorkControl(nil, limits.WorkLimits{MaxInputWork: 1})
	if err := catchFatal(t, func() { newControlledDocumentInput(strings.Repeat("a", 8192), CodePoints, c) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("piece preparation interruption: %v", err)
	}
}

func TestWorkDocumentPieceTextPublication(t *testing.T) {
	if !documentPieceInput {
		t.Skip("piece input is disabled")
	}
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		pieces := newDocumentPieces(strings.Repeat("x", 8192), unit)
		pieces.replace(4096, 4097, "é")
		in := input{unit: unit, eof: true, pieces: pieces, control: newWorkControl(nil, limits.WorkLimits{MaxInputWork: 4096})}
		if err := catchFatal(t, func() { in.text(0, pieces.n) }); !errors.Is(err, limits.ErrWorkLimit) {
			t.Fatalf("%s: piece materialization bypassed the limit: %v", unit, err)
		}
		if pieces.fullOK || pieces.full != "" {
			t.Fatal("interrupted materialization published a cache")
		}
		in.control = newWorkControl(nil, limits.WorkLimits{MaxInputWork: 1 << 30})
		got := in.text(0, pieces.n)
		if want := strings.Repeat("x", 4096) + "é" + strings.Repeat("x", 4095); got != want || !pieces.fullOK {
			t.Fatalf("%s: retry materialization differs", unit)
		}
	}
}

func TestWorkDocumentPieceLineTableRetry(t *testing.T) {
	prog := compile(t, "var line int\nrule main = {line == 2000} .* $$")
	workDocumentVariants(t, func(t *testing.T, o ParseOptions) {
		d, err := prog.NewDocumentWith("main", strings.Repeat("x\n", 4096), o)
		if err != nil {
			t.Fatal(err)
		}
		o.WorkLimits.MaxInputWork = 100
		if _, err = d.ParseWith(o); !errors.Is(err, limits.ErrWorkLimit) {
			t.Fatalf("line table interruption: %v", err)
		}
		if d.in.lines != nil {
			t.Fatal("interrupted scan retained a partial line table")
		}
		workDocumentAssertRetry(t, d)
	})
}

func TestWorkDocumentPieceLiteralRetry(t *testing.T) {
	text := strings.Repeat("a", 8192)
	prog := compile(t, "rule main = "+`"`+text+`" $$`)
	workDocumentVariants(t, func(t *testing.T, o ParseOptions) {
		d, err := prog.NewDocumentWith("main", text, o)
		if err != nil {
			t.Fatal(err)
		}
		o.WorkLimits.MaxInputWork = 100
		if _, err = d.ParseWith(o); !errors.Is(err, limits.ErrWorkLimit) {
			t.Fatalf("piece literal bypassed the limit: %v", err)
		}
		workDocumentAssertRetry(t, d)
	})
}
