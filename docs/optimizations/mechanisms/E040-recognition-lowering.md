# E040: Recognition Lowering

Recognition checks conformance without publishing a parse tree. A separate
compiled program avoids unused values and actions while retaining the values
needed to decide predicates.

## Mechanism and Invariants

`Program.recognizer` builds a separate rule graph with independent memo
identities. Unused captures and actions are omitted; value-producing actions
required by predicates still execute and can fail. Syntax errors, recovered
errors and matching rollback preserve the recognition contract. Recognition is
an explicitly selected operation, not the ordinary full parse with a hidden
optimization flag.

## Scope and State

Engine recognition is available in closure and both VMs when the grammar is
present. Generated Go and TypeScript emit recognition when requested; typed Go
packages use their generated recognition entry point rather than constructing
typed output. The default full-parse operation remains valued.

## Reference Control

No separate same-operation all-valued recognition comparator is maintained.
Comparing Parse with Recognize measures different requested results, and does
not isolate any one value-elision mechanism.

## Evidence and Implementation

Historical record 007 measures recognition about 1.5–1.8 times faster than full
parsing; 047 reports generated recognition versus Node parsing. These are
operation comparisons with their stated workloads, not mechanism ablations or
equivalent-output decoder comparisons.

Evidence records: [007](../007-recognition-mode-3f7e9bf.md),
[047](../047-recognition-in-generated-parsers.md).

Source owners: [`Program.recognizer`](../../../internal/engine/parse.go),
[`recognize compilation and predicate capture
analysis`](../../../internal/engine/compile.go), [`recognizer
generation`](../../../internal/engine/gen.go), [`generated recognize entry
point`](../../../internal/engine/gen_ts.go).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [E010](E010-value-free-rule-bodies.md), [G020](G020-lazy-backend-preparation.md), [A160](A160-whole-input-scratch-pooling.md).

[Optimization Catalog](../README.md)
