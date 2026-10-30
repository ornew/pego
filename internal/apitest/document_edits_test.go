package apitest_test

import (
	"context"
	"errors"
	"github.com/ornew/pego"
	"github.com/ornew/pego/internal/engine/core"
	"testing"
)

func TestDocumentVersionAndBatchAfterInterruption(t *testing.T) {
	p, err := compileTestSource(`rule main = .* $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, unit := range []pego.PositionUnit{pego.CodePoints, pego.Bytes} {
		d, err := p.NewDocument("éabcd", pego.WithUnit(unit))
		if err != nil {
			t.Fatal(err)
		}
		base := d.Version()
		// The reference build has no cancellation or limits to interrupt the parse.
		if core.BudgetControlsEnabled {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if n, err := d.Parse(pego.WithContext(ctx)); n != nil || !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation", n, err)
			}
			if d.Version() != base {
				t.Fatal("cancellation advanced text version")
			}
			if n, err := d.Parse(pego.WithLimits(pego.Limits{Budget: 1})); n != nil || !errors.Is(err, pego.ErrLimit) {
				t.Fatal("budget limit", n, err)
			}
			if d.Version() != base {
				t.Fatal("execution interruption advanced text version")
			}
		}
		if err := d.EditBatch(base, pego.TextEdit{Start: 0, End: 0, Text: "x"}); err != nil {
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
	for _, unit := range []pego.PositionUnit{pego.CodePoints, pego.Bytes} {
		doc, err := parser.NewDocument("éabcd", pego.WithUnit(unit))
		if err != nil {
			t.Fatal(err)
		}
		base := doc.Version()
		seen := map[pego.DocumentVersion]string{base: "initial"}
		if doc.Unit() != unit || base.Sequence() != 1 {
			t.Fatal("initial version/unit")
		}
		old, err := doc.Parse()
		if err != nil {
			t.Fatal(err)
		}
		before := old.String()
		offset := 1
		if unit == pego.Bytes {
			offset = 2
		}
		changes := []pego.TextEdit{{Start: offset, End: offset + 1, Text: "XYZ"}, {Start: offset + 3, End: offset + 4, Text: "!"}}
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
		fresh, err := parser.Parse(doc.Text(), pego.WithUnit(unit))
		if err != nil || actual.String() != fresh.String() {
			t.Fatal("fresh parity", err)
		}
		current := doc.Version()
		if err := doc.EditBatch(current); err != nil || doc.Version() != current {
			t.Fatal("empty current batch")
		}
	}
}
