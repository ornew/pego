# 41. Building the offset table while decoding, for full parses

- Since change 30 the code-point offset table is built on demand, which spares recognition a pass but costs full parses
  (which nearly always need token text) a separate pass at their first token. Full parses and `Document` now build it
  in the decoding pass (`newTextInput` with offsets), as generated parsers do (change 40); recognition keeps building
  it on demand.
- Effect (min of 10 interleaved runs, Apple M3 Max, code points): full parses 1–5% faster on the closure and bytecode
  backends (XML closure 15.3 → 14.5 ms, JSON closure 15.4 → 14.7 ms); recognition and incremental parsing unchanged.
