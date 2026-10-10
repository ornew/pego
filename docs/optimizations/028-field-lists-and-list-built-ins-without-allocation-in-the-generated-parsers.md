# 28. Field lists and list built-ins without allocation in the generated parsers

- The generated runtime still allocated a field list per struct node and per node with captures, and a slice per
  `list`, `map` and `concat` call (changes 15 and 27 in the engine). Field lists now come from chunks
  (`parser.fields`), and the list built-ins gather their elements on `kidStack` and hand the copied-out slice to
  the list node. A predicate that fails with an evaluation error pops what a failed built-in gathered.
- Effect (min of 8 interleaved runs, Apple M3 Max, generated parsers, code points): JSON 10.8 → 10.3 ms, 53.9k → 1.3k
  allocations; CSV 5.2 → 4.9 ms, 45.6k → 0.8k; XML 10.5 → 10.0 ms, 31.5k → 19.1k; Arith_Pratt 7.5 → 7.2 ms, 22.8k →
  0.6k; minilang 12.2 → 11.9 ms, 19.3k → 1.1k. Bytes change by −1% to +3% (partly used chunks).
