# 24. The plain call path in the iterative VM

- The iterative VM sent every rule call, memoized or not, through `callBegin` and `callEnd` (examined-range
  bookkeeping, state copies) and through the full `invokeBegin` / `invokeEnd`. Its call frame now takes the same paths
  as `parser.call`: unmemoized calls of rules without captures run the steps of `invokePlain`, other unmemoized calls
  skip the memo bookkeeping, and only memoized calls use `callBegin` and `callEnd`.
- `callBegin` used to decide again whether to memoize, so the recursive backends called `firstCall` twice for each
  memoized call and counted every repeat twice (change 18). The caller now decides once. The threshold for eager
  memoization went from 8 to 16 so that rules switch as before (minilang allocates the same as before).
- Effect (min of 6 interleaved runs, Apple M3 Max), iterative VM: full parses JSON 33.2 → 26.0 ms, XML 31.6 → 25.5 ms,
  Arith_Pratt 24.1 → 21.7 ms, Arith_LeftRec 44.7 → 39.8 ms, minilang 30.7 → 28.2 ms; recognition JSON 28.5 → 20.3 ms,
  XML 29.7 → 23.2 ms. Other backends unchanged.
