# E050: Projected Repetition Values

A captured repetition often builds an element record only so an action can
extract one field. Projection collects that field while matching, avoiding
intermediate records and the later field traversal.

## Mechanism and Invariants

`ruleProjections` accepts a capture used only as `map($x, (e) => $e.f)`. No
predicate may read `$x`, positional body values must be unused, both capture
names must occur once, and every element must produce a non-nil `f`. These
checks prevent replacing a field-access error with a successful projection.
Operators Rule bodies are excluded. The VM lowers collection through `NEXT` mode
3; Go and TypeScript emit equivalent collection and action rewrites. Matching
and capture rollback still run normally.

## Scope and State

Applied by default to eligible repetitions in closure, recursive VM, iterative
VM, generated Go Node, typed Go and TypeScript. It does not make arbitrary `map`
calls into fused operations.

## Reference Control

Private core compilation option `noProjections` constructs a same-source general
route for differential tests. Corpus comparisons use that engine reference to
check all optimized backends. It is not a public parser option or a standalone
disable selector for every generated output.

## Evidence and Implementation

Historical record 052 reports generated CSV changing from 3.9 to 2.7 ms and 14.0
to 8.7 MB; 053 reports closure CSV from 6.1 to 4.7 ms and smaller improvements
in both VMs. Corpus checks compare against the no-projection engine. Those
historical timings do not establish the current isolated effect or TypeScript
timing.

Evidence records: [052](../052-projected-repetitions-in-generated-parse.md),
[053](../053-projected-repetitions-in-the-engine-and-the-vms.md).

Source owners: [`ruleProjections`](../../../internal/engine/project.go),
[`compiler.projectRepeat`](../../../internal/engine/compile.go), [`projected
repetition lowering`](../../../internal/engine/bcompile.go),
[`tsGen.projectRepeat`](../../../internal/engine/gen_ts.go).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [E010](E010-value-free-rule-bodies.md), [E060](E060-concat-input-fusion.md).

[Optimization Catalog](../README.md)
