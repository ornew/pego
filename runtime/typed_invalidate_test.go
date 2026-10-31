package runtime

import (
	"math/rand"
	"reflect"
	"slices"
	"testing"
)

// referenceInvalidate is the copying invalidate that the in-place version replaces. It returns the
// retained chains as rule ids by position, and the metadata of the retained entries by rule id.
func referenceInvalidate(d *typedDocumentState, start int) ([][]int32, map[int32]typedDocumentMeta) {
	kept := make([]*tmemoEntry, min(start, len(d.slots)))
	meta := make(map[*tmemoEntry]typedDocumentMeta, min(len(d.meta), start))
	for pos, head := range d.slots[:len(kept)] {
		for e := head; e != nil; e = e.next {
			m, complete := d.meta[e]
			if !complete || e.growing || m.reuse != reuseAfterEdits || m.examined > start {
				continue
			}
			out := &tmemoEntry{}
			*out = *e
			out.next = kept[pos]
			kept[pos] = out
			meta[out] = m
		}
	}
	ids := make([][]int32, len(kept))
	byID := make(map[int32]typedDocumentMeta)
	for pos, head := range kept {
		for e := head; e != nil; e = e.next {
			ids[pos] = append(ids[pos], e.rule)
			byID[e.rule] = meta[e]
		}
		slices.Sort(ids[pos])
	}
	return ids, byID
}

// inPlaceResult reads the chains and metadata of d in the same form as referenceInvalidate.
func inPlaceResult(d *typedDocumentState) ([][]int32, map[int32]typedDocumentMeta, int) {
	ids := make([][]int32, len(d.slots))
	byID := make(map[int32]typedDocumentMeta)
	for pos, head := range d.slots {
		for e := head; e != nil; e = e.next {
			ids[pos] = append(ids[pos], e.rule)
			byID[e.rule] = d.meta[e]
		}
		slices.Sort(ids[pos])
	}
	return ids, byID, len(d.meta)
}

// TestInvalidateMatchesCopyingReference pins the in-place invalidate to the copying version it
// replaced: the same entries survive at the same positions, with the same metadata, and no metadata
// is left for an entry that is no longer linked. The copying version reversed each chain; chain order
// does not affect lookups, which match entries by key, so the order is compared as a set.
func TestInvalidateMatchesCopyingReference(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for round := 0; round < 500; round++ {
		n := 1 + rng.Intn(12)
		build := func() *typedDocumentState {
			r := rand.New(rand.NewSource(int64(round)))
			d := &typedDocumentState{meta: make(map[*tmemoEntry]typedDocumentMeta)}
			d.slots = make([]*tmemoEntry, n)
			id := int32(0)
			for pos := range n {
				for k := r.Intn(4); k > 0; k-- {
					e := &tmemoEntry{rule: id, growing: r.Intn(10) == 0}
					id++
					e.next = d.slots[pos]
					d.slots[pos] = e
					switch r.Intn(5) {
					case 0: // incomplete: no metadata
					default:
						d.meta[e] = typedDocumentMeta{examined: pos + r.Intn(6), reuse: docReuse(r.Intn(8) / 7)}
					}
				}
			}
			return d
		}
		start := rng.Intn(n + 3)
		ref := build()
		wantIDs, wantMeta := referenceInvalidate(ref, start)
		got := build()
		got.invalidate(start)
		gotIDs, gotMeta, metaLen := inPlaceResult(got)
		kept := min(start, n)
		if len(gotIDs) != kept || !reflect.DeepEqual(gotIDs[:kept], wantIDs) {
			t.Fatalf("round %d start %d: chains %v, want %v", round, start, gotIDs, wantIDs)
		}
		if !reflect.DeepEqual(gotMeta, wantMeta) {
			t.Fatalf("round %d start %d: metadata %v, want %v", round, start, gotMeta, wantMeta)
		}
		if metaLen != len(wantMeta) {
			t.Fatalf("round %d start %d: %d metadata entries kept, want %d", round, start, metaLen, len(wantMeta))
		}
		if cap(got.slots) > kept && got.slots[:cap(got.slots)][kept] != nil {
			t.Fatalf("round %d start %d: stale chain beyond the kept length", round, start)
		}
	}
}
