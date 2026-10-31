package apitest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/internal/value"
)

func TestDocumentSnapshotPublicContract(t *testing.T) {
	p, err := compileTestSource(`rule main = "a"+ $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, unit := range []zp.PositionUnit{zp.CodePoints, zp.Bytes} {
		d, err := p.NewDocument("aa", zp.WithUnit(unit))
		if err != nil {
			t.Fatal(err)
		}
		s, err := d.Snapshot()
		if err != nil || s == nil || !s.Root().Valid() || s.Text() != "aa" || s.Unit() != unit || s.Version() != d.Version() {
			t.Fatal("snapshot", err)
		}
		legacy := s.Materialize()
		if legacy == nil {
			t.Fatal("materialize")
		}
		value.SetNodeStart(legacy, 999)
		if s.Root().Start() == 999 {
			t.Fatal("snapshot aliases materialized result")
		}
		if err := d.Edit(0, 1, "b"); err != nil {
			t.Fatal(err)
		}
		failed, err := d.Snapshot()
		var syntax *zp.SyntaxError
		if failed == nil || !errors.As(err, &syntax) || failed.Text() != "ba" || failed.Version() == s.Version() || failed.Root().Valid() || s.Text() != "aa" {
			t.Fatal("failed version", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		interrupted, err := d.Snapshot(zp.WithContext(ctx))
		if interrupted != nil || err == nil {
			t.Fatal("cancelled snapshot", err)
		}
		_, parseErr := d.Parse(zp.WithContext(ctx))
		if parseErr == nil || parseErr.Error() != err.Error() {
			t.Fatalf("snapshot and Parse disagree on budget-control availability: %v / %v", err, parseErr)
		}
	}
}
