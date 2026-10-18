package engine

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCacheControlsResetPreservesDocument(t *testing.T) {
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
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
				oldSnapshot, err := d.SnapshotWith(options)
				if err != nil {
					t.Fatal(err)
				}
				if snapshotSharingEnabled && d.previousSnapshot.Value() == nil {
					t.Fatal("snapshot sharing candidate was not recorded")
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
				if d.previousSnapshot.Value() != nil || oldSnapshot.Text() != text || !oldSnapshot.Root().Valid() {
					t.Fatal("reset retained snapshot sharing candidate or changed old snapshot")
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

func TestCacheControlsReuseCounters(t *testing.T) {
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			prog := compile(t, `rule main = line* $$
rule line = [a-z]+ #atomic "\n"`)
			d, err := prog.NewDocumentWith("main", strings.Repeat("ab\n", 20), ParseOptions{Backend: backend, Unit: unit})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.Parse(); err != nil {
				t.Fatal(err)
			}
			if _, err := d.Parse(); err != nil {
				t.Fatal(err)
			}
			if st := d.Stats(); st.MemoHits != 1 || st.ResumedElements != 0 || st.Reused != 1 {
				t.Fatalf("%s/%s root memo reuse: %+v", backend, unit, st)
			}
			if err := d.Edit(30, 31, "x"); err != nil {
				t.Fatal(err)
			}
			if _, err := d.Parse(); err != nil {
				t.Fatal(err)
			}
			if st := d.Stats(); st.ResumedElements == 0 || st.Reused != st.MemoHits+st.ResumedElements {
				t.Fatalf("%s/%s resumed reuse: %+v", backend, unit, st)
			}
		}
	}
}

func TestCacheControlsResetAfterLargeEditAndResumption(t *testing.T) {
	prog := compile(t, `rule main = line* $$
rule line = [a-z]+ #atomic "\n"`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				options := ParseOptions{Backend: backend, Unit: unit}
				d, err := prog.NewDocumentWith("main", strings.Repeat("ab\n", 2000), options)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := d.Parse(); err != nil {
					t.Fatal(err)
				}
				if err := d.Edit(3*500, 3*1500, strings.Repeat("cd\n", 800)); err != nil {
					t.Fatal(err)
				}
				d.ResetHistory()
				got, gotErr := d.Parse()
				fresh, freshErr := prog.ParseWith("main", d.Text(), options)
				if actual, want := dump(t, got, gotErr), dump(t, fresh, freshErr); actual != want {
					t.Fatalf("large edit after reset differs from fresh: got %s; want %s", actual, want)
				}
				if err := d.Edit(30, 31, "x"); err != nil {
					t.Fatal(err)
				}
				if _, err := d.Parse(); err != nil {
					t.Fatal(err)
				}
				if d.Stats().ResumedElements == 0 {
					t.Fatal("fixture did not resume repetition elements")
				}
				d.ResetHistory()
				got, gotErr = d.Parse()
				fresh, freshErr = prog.ParseWith("main", d.Text(), options)
				if actual, want := dump(t, got, gotErr), dump(t, fresh, freshErr); actual != want {
					t.Fatalf("resumption followed by reset differs from fresh: got %s; want %s", actual, want)
				}
			})
		}
	}
}

func TestCacheControlsResetPreservesFreshErrorResults(t *testing.T) {
	for _, tc := range []struct {
		name, grammar, input string
		recovered            bool
	}{
		{"recovered", `rule main = item* $$
rule item = "ab" #recover(skip=[^a]+)`, "abXab", true},
		{"ordinary", `rule main = "ab"+ $$`, "abX", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prog := compile(t, tc.grammar)
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				for _, unit := range []PositionUnit{CodePoints, Bytes} {
					options := ParseOptions{Backend: backend, Unit: unit}
					d, err := prog.NewDocumentWith("main", tc.input, options)
					if err != nil {
						t.Fatal(err)
					}
					_, err = d.Parse()
					if tc.recovered {
						var recovered SyntaxErrors
						if !errors.As(err, &recovered) || len(recovered) == 0 {
							t.Fatalf("%s/%s fixture did not recover: %v", backend, unit, err)
						}
					} else {
						var ordinary *SyntaxError
						if !errors.As(err, &ordinary) {
							t.Fatalf("%s/%s fixture did not fail ordinarily: %v", backend, unit, err)
						}
					}
					d.ResetHistory()
					got, gotErr := d.Parse()
					fresh, freshErr := prog.ParseWith("main", d.Text(), options)
					if tc.recovered {
						var gotRecovered, freshRecovered SyntaxErrors
						if !errors.As(gotErr, &gotRecovered) || !errors.As(freshErr, &freshRecovered) || !reflect.DeepEqual(gotRecovered, freshRecovered) {
							t.Fatalf("%s/%s recovered diagnostics differ: got %v; want %v", backend, unit, gotErr, freshErr)
						}
					} else {
						var gotOrdinary, freshOrdinary *SyntaxError
						if !errors.As(gotErr, &gotOrdinary) || !errors.As(freshErr, &freshOrdinary) || !reflect.DeepEqual(gotOrdinary, freshOrdinary) {
							t.Fatalf("%s/%s ordinary diagnostics differ: got %v; want %v", backend, unit, gotErr, freshErr)
						}
					}
					if actual, want := dump(t, got, gotErr), dump(t, fresh, freshErr); actual != want {
						t.Fatalf("%s/%s reset result differs from fresh: got %s; want %s", backend, unit, actual, want)
					}
				}
			}
		})
	}
}
