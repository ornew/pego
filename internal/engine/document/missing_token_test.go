package document_test

import (
	"testing"

	. "github.com/ornew/zeroparse/internal/engine"
)

func TestMissingTokensDocumentChangesRejectionPreservesBaseline(t *testing.T) {
	prog := mustCompile(t, `rule main = "x" $$`)
	d, err := prog.NewDocument("main", "x")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.EnableChanges(8); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SnapshotWith(ParseOptions{}); err != nil {
		t.Fatal(err)
	}
	d.DrainChanges()
	baseline := d.ChangeBaseline()
	if baseline == nil {
		t.Fatal("missing comparison baseline")
	}
	if n, err := d.ParseWith(ParseOptions{MissingTokens: 1}); n != nil || err == nil {
		t.Fatal("tracked parse accepted unsupported option", n, err)
	}
	if s, err := d.SnapshotWith(ParseOptions{MissingTokens: 1}); s != nil || err == nil {
		t.Fatal("snapshot accepted unsupported option", s, err)
	}
	if events := d.DrainChanges(); len(events) != 0 || d.ChangeBaseline() != baseline {
		t.Fatal("rejected option changed notifications or baseline", events)
	}
	if _, err := d.SnapshotWith(ParseOptions{}); err != nil {
		t.Fatal(err)
	}
	if events := d.DrainChanges(); len(events) != 1 || events[0].Output != OutputUnchanged {
		t.Fatal("valid parse did not compare against original baseline", events)
	}
}
