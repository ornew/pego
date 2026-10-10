# 56. Applying a `Document` edit to memo entries when they are looked up

- After change 54, an edit in the middle of a long document spent most of its time in `Edit`: `memoTable.splice`
  visited every memo entry to keep, shift or drop it (about 300,000 at 100,000 lines), and moved every chain after the
  edit. Now the memo table is a gap buffer with the gap at the last edit, so the positions after an edit move by moving
  the gap (the distance from the previous edit), and the edit decides only the entries at the edited positions. Every
  other entry records how many edits it accounts for (`memoEntry.vgen`, 8 more bytes per entry), and `callBegin`
  applies the edits since, with the same rules in the same order, when it finds the entry (`advanceEntry`); one that
  an edit invalidated is a miss. Lookups in other parses pay one comparison.
- Effect (min of 6 interleaved runs, Apple M3 Max): `BenchmarkIncremental` (Minilang) 0.73 → 0.17 ms (closure, −77%),
  −74% and −71% (VMs); `BenchmarkIncrementalLong` (CSV) 8.2 → 3.2 ms (closure, −61%), −31% and −37% (VMs); the
  100,000-line insert/delete 4.9 → 2.7 ms; `Edit` alone at 100,000 lines 3 ms → 0.33 ms (most of what remains copies
  the text), and 2.4 ms → 70 µs on the guide's 283 KB JSON. Allocation per reparse unchanged in steady state (the
  first edit widens the slot array once); batch parses and streams within noise.
