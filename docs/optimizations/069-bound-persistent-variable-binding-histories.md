# 69. Bound persistent variable binding histories

- Every assignment previously prepended a binding even when the name already
  existed. A long repetition retained all shadowed values, and reading an outer
  variable traversed them all. The current environment now holds one binding
  per name. Replacing a changed non-head value copies its prefix and shares the
  suffix, preserving saved environments for rule scopes and backtracking.
  Assigning an equal scalar value reuses the environment. A monotonic name hint
  skips searches for first assignments and records names from the first
  assignment, including abandoned branches. Closure, both VMs, generated
  Go/typed Go and TypeScript use this logic.
- For a stream with three assignments per record and three distinct names, the
  old current list has 60,001/180,001 bindings at 20k/60k emitted records; the
  new one has three at both points. Old forced-GC heap growth is about 5.5 MiB;
  the new recorded growth is 0–6 KiB across engines and position units.
  Live snapshots, scalar data and memo keys retain what their contracts require;
  this change bounds assignment history, not arbitrary user values or nesting.
- Six interleaved before/after runs against f259b37, Apple M3 Max, Go 1.27.1,
  `-benchtime=200ms`, recognition with code points: reading `limit` while
  repeatedly assigning `i` takes 169–171 → 0.65–0.82 ms at 16,000 characters.
  The new timings grow linearly from 2k through 16k, whereas the old timings
  grow quadratically. Nearest-variable time drops 13–14%, and equal-value
  reuse reduces allocation from about 0.77 MB/16,000 allocations to
  2.5–2.8 KB/9–15 allocations. Alternating constant names also avoid copying.
- Changed values cost more allocation: incrementing two alternating counters
  while reading `limit` takes 345–350 → 2.34–2.59 ms at 16k, but allocation
  rises 1.86 → 3.33 MB (+78–79%) and about 63,600 → 95,500 allocations (+50%).
  A failed choice arm that changes both counters before retrying takes
  403–431 → 4.49–4.94 ms at 16k, with 3.65 → 6.65 MB (+82%) and
  about 127,100 → 191,000 allocations (+50%).
  Copying the prefix buys immutable rollback and bounded lookup/retention.
  Many distinct first assignments also pay for the hint map: 16 names take
  0.46 → 1.27 µs/2.3 → 4.2 KB, 128 take 2.22 → 9.06 µs/7.7 → 21.5 KB, and
  1,024 take 16.9 → 71.6 µs/50.7 → 160.1 KB. These synthetic startup costs
  are accepted for the retention fix; they are not general speedups.
- The existing Outline batch workload stays within 2.7% in median time for
  parsing/recognition on every engine, generated Node Go and generated typed
  Go. The hint map adds about two allocations; bytes increase by at most
  0.03% in this sample.
  TS live-state and parity tests cover semantics; no TS speedup is measured.
  The full benchmark report is refreshed at the stream-memory checkpoint.
- Six pairs compare f259b37 (runtime and generated benchmark fixtures restored
  with a Go overlay) with the unique-binding change in ec2f903. Restore
  `runtime.go`, `eval.go`, `vm.go` and `bench/gen/*/parser.go`; an unused
  prepend-only `bind` helper lets new snapshot tests compile on the baseline,
  whose assignment paths and parser layout remain unchanged. The rollback
  workloads and heap regressions are in the benchmark/test code. Reproduce with
  `go test ./internal/engine -run '^$' -bench 'Benchmark(EnvironmentBindings|DistinctBindings)$' -benchmem`.
  Tests check saved snapshots against independent value maps, choice/rule scope,
  positive/negative lookahead, streams and actual generated Go/TS rule state.
