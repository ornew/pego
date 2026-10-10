# 32. Matching literals against loaded input in one loop

- `matchLiteral` checked one character at a time, each time recording the examined range, checking that the input was
  loaded far enough (stream input is read lazily) and indexing relative to the discarded prefix. When the whole input
  is loaded (every parse except streams), it now compares the literal in one loop and updates the position and the
  examined range once, with the same results (a mismatch still leaves the position after the matched prefix and
  examines the first differing character).
- Effect (min of 8 interleaved runs, Apple M3 Max): closure and recursive bytecode backends 1.5–7% faster (minilang
  closure 16.2 → 15.3 ms, Arith_Pratt closure 10.4 → 9.8 ms, recognition of Arith_Pratt 7.5 → 7.0 ms), except CSV on
  bytecode (unchanged within noise).
