# Focused Benchmark Measurements

This directory keeps the raw measurements used to evaluate adopted changes.
[The performance tuning log](../../docs/performance.md) records their interpretation,
tradeoffs and applicable backends in the same commit as the code change.

Each record identifies the baseline, candidate, workloads, commands, runtime and
machine. Retain repeated before/after samples that support the decision; temporary
experiments and unrelated execution logs do not belong here.

The full benchmark suite continues to save `bench/results.txt`, which
`go run ./bench/report` uses to generate `docs/benchmarks.md`.
