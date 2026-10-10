# 34. One frame per plain call in the iterative VM

- A rule call in the iterative VM pushed a call frame, which pushed a body frame: two frame pushes, pops and dynamic
  dispatches per call. The most common call, an unmemoized call of a rule without captures (change 24), is now run
  by the body frame alone (`plainCallFrame`), which does the call's first steps when it is pushed and the last ones
  when the body finishes. The caller decides once whether to memoize and passes the decision to the call frame.
- Effect (min of 8 interleaved runs, Apple M3 Max, iterative VM): full parses JSON 24.3 → 22.0 ms, CSV 9.9 → 9.2 ms,
  XML 24.1 → 21.9 ms, Arith_Pratt 20.3 → 19.6 ms; recognition JSON 18.4 → 14.5 ms, CSV 6.1 → 5.3 ms, XML 21.2 →
  18.9 ms; minilang unchanged within noise.
