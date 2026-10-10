# 53. Projected repetitions in the engine and the VMs

- Change 52 in every backend. The analysis moved to `project.go` (shared by the closure backend, the bytecode
  compiler and the code generator). It decides from the grammar alone (the engine compiles before type checking):
  a rule's value is never nil when it has a terminal type or its action makes a struct or returns such a capture,
  through `#error` and recursion. The closure backend records the projected `map` calls and its evaluator takes the
  list as it is; the VMs mark the repetition with `NEXT` mode 3 (push the field's slot) and compile the `map` with
  `(e) => $e` in place of the projection, so they need no other new instruction.
- With projections in every backend, results produced with them could only be checked against a program without
  them: the corpus tests now compare every backend and the generated parsers with the engine compiled with
  `noProjections`, and fail if the conditions are loosened (a nil field, `$n`).
- Effect (min of 10 interleaved runs, Apple M3 Max): CSV −22% (closure, 6.1 → 4.7 ms), −16% and −14% (VMs), 14.0 →
  8.7 MB; JSON −9% (closure, 13.5 → 12.3 ms), −8% and −6% (VMs); Minilang within noise.
