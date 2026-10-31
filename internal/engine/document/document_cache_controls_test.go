package document

import (
	"testing"

	"github.com/ornew/zeroparse/internal/engine/core"
)

func TestCacheControlsRetiredAndRunAccounting(t *testing.T) {
	d := &Document{memo: core.NewMemoTable()}
	live := d.memo.Alloc()
	d.memo.Put(core.MemoKey{Rule: 1, Pos: 0}, live)
	retired := d.memo.Alloc()
	d.memo.Retire(retired)
	d.runs = map[core.RunKey]*core.RunRecord{{Site: 1}: {Elems: make([]core.RunElem, 7, 11)}}
	got := d.CacheStats()
	if got.LiveMemoEntries != 1 || got.RetiredMemoEntries != 1 || got.RepetitionElements != 7 || got.EstimatedBytes == 0 {
		t.Fatalf("stats: %+v", got)
	}
	if d.memo.Free != retired || d.memo.Slots[0] != live {
		t.Fatal("inspection mutated cache")
	}
	d.ResetHistory()
	if got := d.CacheStats(); got != (CacheStats{}) {
		t.Fatalf("reset: %+v", got)
	}
}
