# 70. Keep repetition records across unchanged Document parses

- A root memo hit on an unchanged `Document.Parse` previously published an empty
  run map, losing the long repetition's next-edit resumption. Unchanged parses
  now keep the map from the same edit generation; rebuilding a repetition can
  replace its record in that map. Only an edit advances the generation. A parse
  after an edit still creates a new map; two unparsed edits, aborted execution
  and edit-log rollover retain the existing invalidation rules. The unchanged
  path copies no records and allocates no replacement map. All engine backends
  apply this; generated parsers have no Document API.
- Five alternating pairs, Apple M3 Max, Go 1.27.1, 200 ms per sample, compare
  baseline `95f4e3a` with this same-generation run-map retention. The focused
  workload has 100,000 `ab\n` lines and replaces one character in the middle.
  Compilation, document construction, initial parsing and the optional unchanged
  parse are outside the timer; Edit plus the final Parse are measured. With the
  redundant parse, 0 → 99,999 elements resume on all engines and both units.
  Code-point medians are:

  | Backend | Time, before → after | B/op, before → after | Allocs/op |
  |:--|:--|:--|:--|
  | Closure | 6.90 → 0.778 ms (−88.7%) | 48.83 → 1.15 MB | 40 → 11 |
  | Recursive VM | 8.03 → 1.413 ms (−82.4%) | 57.76 → 10.08 MB | 73 → 44 |
  | Iterative VM | 9.06 → 1.417 ms (−84.4%) | 57.76 → 10.08 MB | 81 → 52 |

  Byte-position timings fall 89.1%, 83.1% and 84.9%, respectively, with the same
  allocation counts and almost identical bytes. Without the redundant parse,
  all six controls retain 99,999 resumed elements and unchanged allocation
  counts/bytes (at most 4 B/op difference); time medians range −4.9% to +5.8%
  with overlapping sample ranges. This restores later edit reuse; it does not
  measure the cost of the unchanged parse itself or claim a batch speedup.
- The existing minilang Document/fresh and 50,000-record CSV incremental controls
  also have five alternating 200 ms pairs. All nine sample ranges overlap;
  median times change −3.4% to +0.9%, allocations stay unchanged and B/op varies
  −0.8% to +0.02%. Their edit schedules do not include a redundant Parse, so
  these are regression controls rather than evidence of the new-path speedup.
- Retention extends the lifetime of valid current-generation records across
  unchanged calls, as needed for the next edit; it does not archive generations.
  Tests compare values, spans and errors against fresh parses after repeated
  no-edit calls, nested edits with shifts in both directions, ordinary failures
  and recovery, multi-edit gaps and edit-log resets. A primed root-memo trace
  panic clears the shared run map/memo/scratch, then retry and edit reuse work.
  Existing action/depth and provisional-left-recursion invalidation tests pass.
- Reproduce with
  `go test ./internal/engine -run '^$' -bench '^BenchmarkDocumentReparse$' -benchtime=200ms -benchmem`
  and `go test ./bench -run '^$' -bench '^BenchmarkIncremental(Long)?$' -benchtime=200ms -benchmem`.
  Build separate baseline/candidate binaries with the same benchmark source;
  restore only `internal/engine/document.go` from the baseline via a Go overlay
  and alternate their runs. Raw samples remain local. The next full benchmark
  checkpoint follows the core analysis optimization group (P03/P13/P27).
