# 20. Splicing the memo table on Document edits

- `Document.Edit` rebuilt the memo table on every edit: it walked all entries and `put` each kept one into a new
  table, which allocated a new slot array and searched each chain for an existing key. In the incremental benchmark
  (a one-character edit to minilang followed by a reparse) this was about 30% of the time.
- The table is now spliced in place (`memoTable.splice`): each chain is filtered with the same keep/shift/drop rules,
  the slots from the end of the edit on move by the length difference (one `copy`), and the few entries that stay at
  the edit position (rules that examined no input) are put back. `TestDocumentEditKeepsMemo` checks after random edits
  that exactly the entries the rules allow are kept, at the right positions; it fails if those entries are lost.
- Effect (min of 6 interleaved runs, Apple M3 Max): edit plus reparse 3.50 → 2.29 ms (closure), 3.37 → 2.34 ms
  (bytecode), 3.46 → 2.33 ms (iterative); 6.0 → 3.9 MB allocated per edit.
- What remains is mostly shifting reused subtrees (`shiftNode` copies a reused node tree with its positions moved)
  and copying the input.
