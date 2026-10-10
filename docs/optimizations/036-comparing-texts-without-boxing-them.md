# 36. Comparing texts without boxing them

- `text(...)` returns a string as an interface value, which allocates for the string header. The XML example compares
  the name of every end tag with its start tag (`[text($e) == text($n)]`), which was 18k of its 19k allocations per
  parse. A comparison `text(a) == text(b)` (or `!=`) is now evaluated as a string comparison by the closure backend
  and emitted as one by the code generator. The VMs still box the strings (avoiding it there would take a new
  instruction).
- Effect (min of 12 interleaved runs, Apple M3 Max, XML): closure 14.1 → 13.8 ms, generated 9.9 → 9.6 ms, recognition
  11.7 → 11.4 ms; 19k → 1.1k allocations.
