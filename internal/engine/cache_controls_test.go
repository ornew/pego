package engine

import (
	"reflect"
	"strings"
	"testing"
)

func TestCacheControlsResetPreservesDocument(t *testing.T) {
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				prog := compile(t, `rule main = item* $$
rule item = [a-z]+ "\n"`)
				options := ParseOptions{Backend: backend, Unit: unit, MaxDepth: 12345}
				text := strings.Repeat("word\n", 40)
				d, err := prog.NewDocumentWith("main", text, options)
				if err != nil {
					t.Fatal(err)
				}
				d.ResetHistory()
				if got := d.CacheStats(); got != (CacheStats{}) {
					t.Fatalf("initial reset: %+v", got)
				}
				old, err := d.Parse()
				if err != nil {
					t.Fatal(err)
				}
				if s := d.CacheStats(); s.LiveMemoEntries == 0 || s.EstimatedBytes == 0 {
					t.Fatalf("empty cache: %+v", s)
				}
				first := d.CacheStats()
				if second := d.CacheStats(); second != first {
					t.Fatalf("inspection changed stats: %+v != %+v", second, first)
				}
				before := dump(t, old, nil)
				parseStats := d.Stats()
				d.ResetHistory()
				if got := d.CacheStats(); got != (CacheStats{}) {
					t.Fatalf("reset: %+v", got)
				}
				if d.Text() != text || !reflect.DeepEqual(d.ParseDefaults(), options) || d.Stats() != parseStats {
					t.Fatal("reset changed text, defaults or last stats")
				}
				if err := d.Edit(0, 0, "new\n"); err != nil {
					t.Fatal(err)
				}
				n, err := d.Parse()
				fresh, freshErr := prog.ParseWith("main", d.Text(), options)
				if dump(t, n, err) != dump(t, fresh, freshErr) {
					t.Fatal("reset parse differs from fresh")
				}
				if dump(t, old, nil) != before {
					t.Fatal("old tree moved after reset")
				}
			})
		}
	}
}

func TestCacheControlsRetiredAndRunAccounting(t *testing.T) {
	d := &Document{memo: newMemoTable()}
	live := d.memo.alloc()
	d.memo.put(memoKey{rule: 1, pos: 0}, live)
	retired := d.memo.alloc()
	d.memo.retire(retired)
	d.runs = map[runKey]*runRecord{{site: 1}: {elems: make([]runElem, 7, 11)}}
	got := d.CacheStats()
	if got.LiveMemoEntries != 1 || got.RetiredMemoEntries != 1 || got.RepetitionElements != 7 || got.EstimatedBytes == 0 {
		t.Fatalf("stats: %+v", got)
	}
	if d.memo.free != retired || d.memo.slots[0] != live {
		t.Fatal("inspection mutated cache")
	}
	d.ResetHistory()
	if got := d.CacheStats(); got != (CacheStats{}) {
		t.Fatalf("reset: %+v", got)
	}
}

func TestCacheControlsResetAfterFailureAndAbort(t *testing.T) {
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		p := compile(t, `rule main = "a"* $$`)
		for _, limits := range []WorkLimits{{}, {MaxSteps: 1}} {
			d, err := p.NewDocumentWith("main", "aaa!", ParseOptions{Backend: b})
			if err != nil {
				t.Fatal(err)
			}
			_, err = d.ParseWith(ParseOptions{Backend: b, WorkLimits: limits})
			if err == nil {
				t.Fatal("expected failure or interruption")
			}
			last := d.Stats()
			d.ResetHistory()
			if got := d.CacheStats(); got != (CacheStats{}) {
				t.Fatalf("reset after failure: %+v", got)
			}
			if d.Stats() != last || d.Text() != "aaa!" {
				t.Fatal("reset changed failure stats or text")
			}
			if err := d.Edit(3, 4, ""); err != nil {
				t.Fatal(err)
			}
			if _, err := d.Parse(); err != nil {
				t.Fatal(err)
			}
			if d.CacheStats().LiveMemoEntries == 0 {
				t.Fatal("history did not repopulate")
			}
		}
	}
}
