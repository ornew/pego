# 29. Shifting reused subtrees into the parser's chunks

- After an edit, a memo result after the edit is reused with its positions shifted: `shiftNode` copies its node tree.
  Every copied node, child list and field list was a separate heap allocation, and every copy made a new map of the
  nodes already copied (to keep shared subtrees shared). In the incremental benchmark this was 89% of the bytes
  allocated per edit and reparse.
- Copies now come from the parser's chunks (`newNode`, `nodes`, `fields`), and the map is kept by the parser and
  cleared between copies (and dropped when it grew past 1,024 entries, so clearing stays cheap).
- Effect (min of 8 interleaved runs, Apple M3 Max, one-character edit to minilang plus reparse): 1.72 → 1.20 ms
  (closure), 1.76 → 1.20 ms (bytecode), 1.77 → 1.25 ms (iterative); 20k → 0.15k allocations, 3.0 → 2.4 MB.
