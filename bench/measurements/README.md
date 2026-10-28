# Benchmark Measurement Records

Keep focused benchmark output as local working data. Commit its summary,
reproduction commands and tradeoffs in [the performance log](../../docs/performance.md)
with the code change; source commits and benchmark code support reruns.

This directory is reserved for exceptional raw records whose long-term value
cannot be preserved by a summary and rerun. Routine before/after runs and
execution logs do not belong in Git.

`go run ./bench/report` saves the full benchmark suite to `bench/results.txt`, the
generated TypeScript timings to `bench/results-ts.txt` and the generated parser
sizes to `bench/results-size.txt`, and generates `docs/benchmarks.md` from them.
