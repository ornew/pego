# 21. Changes 18 and 19 in the generated parsers

- The generated runtime (`internal/engine/genrt`) now defers memoization to the second call at a position with the
  same per-rule switch to eager memoization (the generator emits each rule's `seen` number and their count), and calls
  unmemoized rules without captures through `invokePlain`. The generated files in `bench/gen` and
  `examples/json/generated` were regenerated.
- Effect (min of 8 interleaved runs, Apple M3 Max, code points; time and bytes per parse):

  | Workload | Before | After |
  |:--|--:|--:|
  | JSON | 14.8 ms, 37.0 MB | 11.2 ms, 23.8 MB |
  | CSV | 7.2 ms, 27.4 MB | 5.7 ms, 19.4 MB |
  | XML | 11.8 ms, 29.4 MB | 10.7 ms, 25.3 MB |
  | Arith_Pratt | 8.9 ms, 16.9 MB | 7.6 ms, 11.9 MB |
  | Arith_LeftRec | 18.2 ms | 18.8 ms (unchanged bytes) |
  | Minilang | 13.2 ms, 18.6 MB | 12.8 ms, 17.2 MB |
