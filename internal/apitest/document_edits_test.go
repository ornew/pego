package apitest_test

import (
	"context"
	"errors"
	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/internal/engine/core"
	"testing"
)

func TestDocumentVersionAndBatchAfterInterruption(t *testing.T) {
	p, err := compileTestSource(`rule main = .* $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, unit := range []zp.PositionUnit{zp.CodePoints, zp.Bytes} {
		d, err := p.NewDocument("éabcd", zp.WithUnit(unit))
		if err != nil {
			t.Fatal(err)
		}
		base := d.Version()
		// The reference build has no cancellation or limits to interrupt the parse.
		if core.BudgetControlsEnabled {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if n, err := d.Parse(zp.WithContext(ctx)); n != nil || !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation", n, err)
			}
			if d.Version() != base {
				t.Fatal("cancellation advanced text version")
			}
			if n, err := d.Parse(zp.WithLimits(zp.Limits{Budget: 1})); n != nil || !errors.Is(err, zp.ErrLimit) {
				t.Fatal("budget limit", n, err)
			}
			if d.Version() != base {
				t.Fatal("execution interruption advanced text version")
			}
		}
		if err := d.EditBatch(base, zp.TextEdit{Start: 0, End: 0, Text: "x"}); err != nil {
			t.Fatal(err)
		}
		if d.Version().Sequence() != base.Sequence()+1 || d.Text() != "xéabcd" {
			t.Fatal("batch after interruption")
		}
		if _, err := d.Parse(); err != nil {
			t.Fatal("ordinary retry", err)
		}
	}
}

func TestDocumentEditBatchPublicContract(t *testing.T) {
	parser, err := compileTestSource(`rule main = .* $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, unit := range []zp.PositionUnit{zp.CodePoints, zp.Bytes} {
		doc, err := parser.NewDocument("éabcd", zp.WithUnit(unit))
		if err != nil {
			t.Fatal(err)
		}
		base := doc.Version()
		seen := map[zp.DocumentVersion]string{base: "initial"}
		if doc.Unit() != unit || base.Sequence() != 1 {
			t.Fatal("initial version/unit")
		}
		old, err := doc.Parse()
		if err != nil {
			t.Fatal(err)
		}
		before := old.String()
		offset := 1
		if unit == zp.Bytes {
			offset = 2
		}
		changes := []zp.TextEdit{{Start: offset, End: offset + 1, Text: "XYZ"}, {Start: offset + 3, End: offset + 4, Text: "!"}}
		if err := doc.EditBatch(base, changes...); err != nil {
			t.Fatal(err)
		}
		if doc.Text() != "éXYZbc!" || doc.Version().Sequence() != 2 || seen[base] != "initial" {
			t.Fatal("base coordinates or revision")
		}
		if old.String() != before {
			t.Fatal("batch changed old result")
		}
		if err := doc.EditBatch(base); err == nil {
			t.Fatal("stale empty batch accepted")
		}
		actual, err := doc.Parse()
		if err != nil {
			t.Fatal(err)
		}
		fresh, err := parser.Parse(doc.Text(), zp.WithUnit(unit))
		if err != nil || actual.String() != fresh.String() {
			t.Fatal("fresh parity", err)
		}
		current := doc.Version()
		if err := doc.EditBatch(current); err != nil || doc.Version() != current {
			t.Fatal("empty current batch")
		}
	}
}
