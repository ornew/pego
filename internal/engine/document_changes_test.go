package engine

import (
	"testing"
)

func newChangeTestDocument(t *testing.T, prog *Program, text string, o ParseOptions, limit int) *ChangeDocument {
	t.Helper()
	d, err := prog.NewChangeDocumentWith("main", text, o, limit)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDocumentChangesOverflowRequiresFullRefresh(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = .* $$`)
	d := newChangeTestDocument(t, prog, "a", ParseOptions{}, 1)
	base := d.Version()
	for _, text := range []string{"b", "c", "d"} {
		if err := d.Edit(0, 1, text); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Snapshot(); err != nil {
		t.Fatal(err)
	}
	changes := d.DrainChanges()
	if len(changes) != 1 || !changes[0].FullRefresh || changes[0].From != base || changes[0].To != d.Version() || changes[0].Unit != d.Unit() || changes[0].Edits != nil || changes[0].Output != OutputMayHaveChanged {
		t.Fatal("overflow marker", changes)
	}
	if err := d.Edit(0, 1, "e"); err != nil {
		t.Fatal(err)
	}
	if changes := d.DrainChanges(); len(changes) != 1 || changes[0].FullRefresh {
		t.Fatal("drain did not reset queue", changes)
	}
	if _, err := prog.NewChangeDocumentWith("main", "", ParseOptions{}, 0); err == nil {
		t.Fatal("zero queue limit accepted")
	}
}

func TestDocumentChangesByteBoundaryRejectionAndNormalizedText(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = .* $$`)
	d := newChangeTestDocument(t, prog, "é", ParseOptions{Unit: Bytes}, 8)
	v := d.Version()
	if err := d.Edit(1, 2, "x"); err == nil {
		t.Fatal("split character accepted")
	}
	if d.Version() != v || len(d.DrainChanges()) != 0 {
		t.Fatal("failed boundary changed notifications")
	}
	cp := newChangeTestDocument(t, prog, "a", ParseOptions{Unit: CodePoints}, 8)
	invalid := string([]byte{0xff})
	if err := cp.Edit(0, 1, invalid); err != nil {
		t.Fatal(err)
	}
	changes := cp.DrainChanges()
	if changes[0].Edits[0].Text != invalid || cp.Text() != "\ufffd" {
		t.Fatal("operation text and normalized state confused")
	}
}
