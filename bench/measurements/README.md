# Benchmark Measurement Records

Keep focused benchmark output as local working data. Commit its summary,
reproduction commands and tradeoffs in [the performance log](../../docs/performance.md)
with the code change; source commits and benchmark code support reruns.

This directory is reserved for exceptional raw records whose long-term value
cannot be preserved by a summary and rerun. Routine before/after runs and
execution logs do not belong in Git.

The full benchmark suite continues to save `bench/results.txt`, which
`go run ./bench/report` uses to generate `docs/benchmarks.md`.
