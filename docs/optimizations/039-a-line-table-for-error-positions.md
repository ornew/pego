# 39. A line table for error positions

- Every syntax error, recovered ones included, computed its line and column by scanning the input from the start.
  With many recovered errors this is quadratic: on the error-recovery benchmark (minilang with one line in seven
  broken) it was 14% of the time. On fully loaded input the line and column now come from a table of line starts,
  built at the first error, by binary search; stream input, whose start is discarded, keeps scanning from the
  committed position. Generated parsers do the same.
- Effect (min of 10–12 interleaved runs, Apple M3 Max, Recovery workload): full parses 15–32% faster (closure 19.2 →
  14.6 ms, generated 16.9 → 11.5 ms), recognition 17–30% faster (closure 15.5 → 10.9 ms).
