# 31. Specialized character-class tests in the closure backend

- The closure backend tested a character against a class by looping over the class's ranges in the grammar AST.
  Classes with one or two ranges (most of them) now compare against constants captured by the test function; larger
  classes test ASCII characters with a bitmap and others against a copy of the ranges.
- An ASCII bitmap alone had measured no gain (see the experiments table); removing the loop and the AST access for the
  common small classes is what pays.
- Effect (min of 10 interleaved runs, Apple M3 Max, closure backend): full parses 1.2–4.9% faster (CSV 7.1 → 6.8 ms,
  XML 15.1 → 14.7 ms), recognition 1.6–9.4% faster (CSV 3.7 → 3.3 ms, XML 12.7 → 12.1 ms).
