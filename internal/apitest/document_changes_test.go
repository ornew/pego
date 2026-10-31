package apitest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/internal/engine/core"
)

func TestDocumentChangesPublicOptIn(t *testing.T) {
	p, err := compileTestSource(`type Entry = { Name string }
rule main Entry = x:([a-zé]+ #atomic) $$ => Entry{Name: text($x)}`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []zp.Backend{zp.Closure, zp.Bytecode, zp.BytecodeIterative} {
		for _, unit := range []zp.PositionUnit{zp.CodePoints, zp.Bytes} {
			d, err := p.NewDocument("aa", zp.WithBackend(backend), zp.WithUnit(unit))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.Parse(); err != nil {
				t.Fatal(err)
			}
			if len(d.DrainChanges()) != 0 {
				t.Fatal("tracking enabled by default")
			}
			if err := d.EnableChanges(0); err == nil {
				t.Fatal("invalid limit")
			}
			if err := d.EnableChanges(8); err != nil {
				t.Fatal(err)
			}
			if err := d.EnableChanges(8); err == nil {
				t.Fatal("re-enable discarded state")
			}
			original, err := d.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			events := d.DrainChanges()
			if len(events) != 1 || events[0].Kind != zp.ChangeOutcome || events[0].Output != zp.OutputMayHaveChanged {
				t.Fatal(events)
			}
			from := d.Version()
			if err := d.EditBatch(from, zp.TextEdit{Start: 0, End: 2, Text: "bb"}); err != nil {
				t.Fatal(err)
			}
			if _, err := d.Parse(); err != nil {
				t.Fatal(err)
			}
			events = d.DrainChanges()
			if len(events) != 2 || events[0].Kind != zp.ChangeEdit || events[0].From != from || events[0].To != d.Version() || events[0].Unit != unit || events[1].Kind != zp.ChangeOutcome || events[1].From != original.Version() || events[1].Output != zp.OutputChanged {
				t.Fatal(events)
			}
			if value, _ := original.Root().Field("Name"); value != "aa" {
				t.Fatal("retained snapshot changed")
			}
			events[0].Edits[0].Text = "caller owned"
			if _, err := d.Snapshot(); err != nil {
				t.Fatal(err)
			}
			if got := d.DrainChanges()[0].Output; got != zp.OutputUnchanged {
				t.Fatal(got)
			}
			// The reference build has no cancellation to interrupt the Snapshot.
			if core.BudgetControlsEnabled {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				if s, err := d.Snapshot(zp.WithContext(ctx)); s != nil || !errors.Is(err, context.Canceled) {
					t.Fatal(s, err)
				}
				if got := d.DrainChanges()[0].Output; got != zp.OutputUnavailable {
					t.Fatal(got)
				}
				if _, err := d.Snapshot(); err != nil {
					t.Fatal(err)
				}
				if got := d.DrainChanges()[0].Output; got != zp.OutputMayHaveChanged {
					t.Fatal("comparison bridged interruption", got)
				}
			}
		}
	}
}

func TestDocumentChangesPublicFullRefresh(t *testing.T) {
	p, err := compileTestSource(`rule main = .* $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	d, err := p.NewDocument("é")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.EnableChanges(1); err != nil {
		t.Fatal(err)
	}
	from := d.Version()
	if err := d.Edit(0, 1, "a"); err != nil {
		t.Fatal(err)
	}
	if err := d.Edit(0, 1, "b"); err != nil {
		t.Fatal(err)
	}
	events := d.DrainChanges()
	if len(events) != 1 || events[0].Kind != zp.ChangeFullRefresh || !events[0].FullRefresh || events[0].From != from || events[0].To != d.Version() || events[0].Edits != nil {
		t.Fatal(events)
	}
}
