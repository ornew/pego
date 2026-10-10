# 25. Discarding separators in the example grammars

- Counting the nodes allocated against those in the final tree showed that about half of the JSON parser's nodes were
  thrown away. Among them, every element after the first in `rest:(-ws "," -ws m:member)*` kept the `","` as a `Match`
  child of the iteration's `Seq` (a captured repetition keeps its whole CST, see the guideline below): 15,550 nodes per
  parse in JSON and 25,005 in CSV. The JSON, CSV and minilang examples now discard the separator (`-","`); the ASTs
  are unchanged.
- The rest of the discarded nodes are the iteration `Seq` nodes themselves and intermediate lists built by `list`,
  `map` and `concat` in actions.
- Effect (min of 6 runs alternating the old and new grammars, Apple M3 Max, code points): bytes per parse JSON 25.8 →
  23.6 MB, CSV 22.5 → 19.0 MB, minilang 18.9 → 18.6 MB (closure); CSV 3.5% faster on the closure and bytecode backends,
  JSON and minilang unchanged within noise. Generated parsers: JSON 11.2 → 10.9 ms, CSV 5.5 → 5.2 ms.
