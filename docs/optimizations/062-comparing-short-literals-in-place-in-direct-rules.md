# 62. Comparing short literals in place in direct rules

- `matchLiteral` was the next call in the JSON `ParseAST` profile (about 4%), mostly for one-character literals such
  as `","` and `":"`. Direct rules now compare a literal of up to four code points with `p.in` in place and call
  `matchLiteral` only when it does not match there (which records the expectation) or in `Bytes`. A test grammar
  covers literals of one to five code points, non-ASCII ones and literals cut off by the end of the input, in both
  units.
- Effect (min of 12 interleaved runs, Apple M3 Max, `ParseAST`): JSON 3.34 → 3.16 ms (−5%), XML 3.89 → 3.80 ms
  (−2%), Outline 2.14 → 1.97 ms (−8%); the calculators within noise (min of 8 runs).
