# 48. A typed runtime for ParseAST

- `ParseAST` (`pego gen -types`) converted the tree of `Parse`, about 6% slower than `Parse`. For grammars whose
  results have Go types of their own, it now runs a typed runtime that builds those values directly
  (design record 012). Steps that mattered, on JSON (262 KB):
  - the runtime alone, with values as `any`: 9.8 ms, 22.4 k allocations (variadic struct constructors);
  - generated constructors `tmk_T`: 9.7 ms, 1.1 k allocations, 15 MB;
  - projected repetitions (`map($rest, (r) => $r.f)`): 9.0 ms, 12 MB;
  - frames reused when their rule returns: 7.6 MB (time within noise);
  - scratch memory pooled across parses: 2.4 MB (time within noise; the remaining cost is not allocation);
  - rule calls with methods of their own, direct actions, one recovery per parse for action errors and constructors
    of action results taking the rule's range: 8.5 → 7.6 ms.
- Effect (min of 4 runs, Apple M3 Max), `ParseAST` against `Parse` of the same generated parser: JSON 9.9 → 7.7 ms
  (20.0 → 2.4 MB), XML 10.0 → 8.0 ms (25.2 → 3.2 MB), Arith_LeftRec 18.7 → 15.0 ms (39.4 → 4.1 MB), Outline 4.5 →
  3.5 ms (13.5 → 2.6 MB), Arith_Pratt 7.8 → 8.0 ms (12.2 → 3.1 MB).
- Later, struct constructors that make the result of a Pratt line's action also take the rule's range directly:
  Arith_Pratt 7.7 → 7.2 ms (min of 12 interleaved runs), now faster than `Parse` (7.8 ms). The same per-rule call
  methods for the Node runtime's `Parse` measured within ±4% (noise) and were not adopted.
- On macOS most of the time a large allocation costs is the `madvise` calls of the Go runtime; profiles there
  attribute it to the code that touches new memory, and GOGC=off makes it worse (every span is new).
