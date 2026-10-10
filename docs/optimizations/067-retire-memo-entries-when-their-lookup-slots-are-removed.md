# 67. Retire memo entries when their lookup slots are removed

- Pruning a stream's lookup slots or replacing a Document key left the old
  entry's result reachable from its allocation slab. Entries now clear their
  node, environment, error and expectation references when retired, and reuse
  their storage. Pruning preserves a seed still owned by an active left-recursion
  frame; replacing a seed retires it after that frame installs its next best.
- Regression measurements on every engine backend and both position units:
  20,000 to 60,000 discarded `abc,abc\n` records retained 42 MiB with memoized
  words, or 135–137 MiB with left-recursive words. With retirement the sampled
  growth is 34–137 KiB. The left-recursive case holds two memo chunks and 257
  allocated entries at 20k, 60k and 200k records; the remaining heap variation
  follows the 1,024-position pruning interval. Replacing the first character of
  a 10 KB Document and parsing after each edit retained 13 MiB between edits
  20 and 300; the corresponding samples now range from 30 KiB lower to 9 KiB higher.
- Effect (median of six interleaved before/after runs, Apple M3 Max, Go 1.27.1,
  `-benchtime=200ms`, code points): `BenchmarkMemoLifecycle`, 10,000 records,
  allocates 11.0–11.3 → 7.8–8.1 MB for memoized words (−28% to −29%), and
  35.9–36.6 → 23.1–23.9 MB for left-recursive words (−35% to −36%). Time changes
  are −1% to −6% and −5% to −9%, respectively. The existing `Arith_LeftRec`
  batch workload allocates 4–5% fewer bytes for parsing and 19–21% fewer for
  recognition; time is within 4% on every backend/unit. Generated runtimes
  are unchanged. These are focused comparisons against f3a2c96 with an overlay
  of its memo allocator, compared with the retirement change in 9353e09;
  this was not a refresh of the full benchmark report.
- Reproduce the focused stream cases with
  `go test ./internal/engine -run '^$' -bench '^BenchmarkMemoLifecycle$' -benchmem`.
  Memory regressions are `TestMemoizedStreamMemoryIsBounded` and
  `TestDocumentReplacedMemoMemoryIsBounded`.
