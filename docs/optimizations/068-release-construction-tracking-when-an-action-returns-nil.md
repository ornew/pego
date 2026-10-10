# 68. Release construction tracking when an action returns nil

- An action that constructed temporary structs and returned nil skipped cleanup
  of its construction tracker. Streams therefore held every temporary node.
  Successful, nil and rejected action results now release tracking references;
  Go clears the discarded slice tail as well as truncating it. Predicates and
  assignments use the same cleanup, without changing values held in the
  environment or returned trees. Generated Node/typed Go, direct typed nested
  constructors and TypeScript mirror the cleanup; TS uses a finally block.
- For `item = "a" -> foldl(nil, list(new V{N:1}), (acc,v)=>nil)`, a discarded
  stream retained 20,000/60,000 tracked structs before cleanup. A closure
  diagnostic sampled 8.3 → 24.0 MB of live heap (15 MiB additional retention).
  The fixed regression on every engine/unit keeps tracking capacity at one;
  heap growth from 20k to 60k is 0–6 KiB in the recorded run. Returned nodes
  keep their identity, fields, spans and final-result labeling.
- Six interleaved before/after runs, Apple M3 Max, Go 1.27.1,
  `-benchtime=200ms`, 10,000 code-point stream elements: closure B/op
  4.20 → 3.89 MB (−7.4%), bytecode/iterative 4.92 → 4.61 MB (−6.3%);
  17 fewer allocations per parse. Median time changes are −20%, −12% and
  −15%. The existing `Arith_Pratt` batch workload has unchanged allocation
  volume: engine median time changes are within 2%, generated Node Go is
  1.6–4.6% slower and generated typed Go 4.2% slower in this sample. Retain
  the cleanup for bounded retention; these figures do not claim every action
  path is faster. TS retained-state/semantic tests pass, but TS timing is not
  measured here. The full benchmark refresh follows the stream-memory work.
- The baseline is daa364d (code unchanged from 9353e09), compared with the
  action-reference cleanup in f259b37. Restore `eval.go`/`vm.go` and the
  generated benchmark parsers for the baseline overlay. Reproduce with
  `go test ./internal/engine -run '^$' -bench '^BenchmarkNilActionStream$' -benchmem`.
  Tests cover backing-array references and prefix ownership, stream heap on
  both units/all engines, generated Go/typed direct rules and TS nil/error paths.
