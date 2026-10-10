# 59. Resuming long repetitions in the VMs

- Change 54 in both bytecode VMs, which share `exec`: the VM finds the repetitions it can resume when it loads a
  module, from the bytecode alone (a `REPEAT`, its `ITER`, the element code, `NEXT` and `ENDREPEAT`; the element must
  contain no `PRED` or `ASSIGN` and call no rule that reads variables, and its elements are moved past a
  length-changing edit only if it calls no positional rule), so the instruction set is unchanged. In a `Document`
  parse, `REPEAT` takes over the old run and reuses the elements before the edit, `ITER` resynchronizes and starts an
  element whose examined range and expectations are kept apart (a new entry kind, `eRunIter`, undoes that on
  failure, and a cut marks it like an `ITER`), `NEXT` records the element and `ENDREPEAT` stores the run. The steps are
  shared with the closure backend (`runState` in `resume.go`).
- Effect (min of 6 interleaved runs, Apple M3 Max): `BenchmarkIncrementalLong` (50,000-record CSV) 12.0 → 4.1 ms on
  the recursive VM (−66%) and 12.5 → 4.1 ms on the iterative VM (−67%); `BenchmarkIncremental` (Minilang) −11% and
  −15%; batch parses and streams within noise.
