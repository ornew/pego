# 16. Start positions and lambdas from chunks in the VMs

- `PUSHPOS` pushed the start position of a sequence, repetition or atomic expression onto the value stack (`[]any`),
  and converting an `int` to an interface allocates for values above 255. It now pushes a `*int` taken from a chunk
  (`parser.newPos`); the consumers (`SEQ`, `ATOMIC`, `ENDREPEAT`) dereference it.
- `EFUNC` allocated each `vmFunc`; lambdas never outlive the evaluation, and they now come from a chunk too
  (`parser.newFunc`).
- Effect (min of 6 interleaved runs, codepoints, Apple M3 Max): allocations per parse JSON 32k → 1.9k, CSV 41k → 1.2k,
  XML 47k → 19k, minilang 14k → 1.4k, for both VMs. Bytes and time are unchanged (within noise); the gain is fewer
  objects for the GC.
- The remaining XML allocations are `text(...)` results: a string converted to an interface needs a header on the
  heap (comparisons of two texts avoid it since change 36).
