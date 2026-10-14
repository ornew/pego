# R020: Deferred Memoization

Most rule results in a straightforward full-input parse are never reused.
Recording only a first-call bit avoids allocating a complete memo entry until a
rule is called again at the same position.

## Mechanism and Invariants

`firstCall` marks a rule/position pair. The second call can populate the memo,
bounding repeated evaluation under the policy. A per-rule repeat-rate counter
switches heavily reused rules to eager memoization once repeats exceed the
adaptive threshold. Left-recursion leaders remain eager because their seed
drives growth. Documents and streams remain eager for persistent reuse or
pruning. Before compact counters can overflow, calls and repeats are both
halved, discounting old observations. Once selected, eager mode is sticky for
that parse. Recognition follows its own program's rule identities.

## Scope and State

Applied by default in whole-input parsing and recognition across closure, both
VMs, generated Go Node, typed Go and TypeScript. It is not the Document or
stream policy.

## Reference Control

No universal same-source eager-memo reference control is maintained.
Sparse/dense controls change only the representation of first-call bits, leaving
the deferred decision intact.

## Evidence and Implementation

Historical record 018 reports closure JSON parsing from 22.5 to 18.1 ms and 41.2
to 25.8 MB. JSON recognition falls from 15.9 to 12.0 ms, but adapted
left-recursive full parsing can still cost about 0–5% more. Record 021 ports the
policy to generated Go. These results do not establish a universal benefit or
isolate later sparse storage; unadapted LR cost was 20–33%.

Evidence records: [018](../018-memoizing-from-the-second-call-at-a-position.md),
[021](../021-changes-18-and-19-in-the-generated-parsers.md).

Source owners: [`parser.firstCall`](../../../internal/engine/runtime.go),
[`memoTable.firstCall`](../../../internal/engine/memo.go), [`generated
firstCall`](../../../internal/engine/genrt/runtime.go), [`TypeScript
firstCall`](../../../internal/engine/tsrt/runtime.ts).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [R030](R030-sparse-seen-position-storage.md), [R120](R120-transient-rule-memo-policy.md).

[Optimization Catalog](../README.md)
