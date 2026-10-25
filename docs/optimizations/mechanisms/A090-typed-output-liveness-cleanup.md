# A090: Typed Output Liveness Cleanup

A retained typed cell can keep unreachable siblings in the same backing chunk
alive. A publication-time reachability sweep can clear those siblings, at the
cost of tracking and tracing output ownership.

## Mechanism and Invariants

The prototype traces from the typed result and parse error before clearing
anything. Interior scalar pointers promote their owner and reachable
descendants; live cells and slice-capacity-visible cells stay intact. Full sweep
is unbudgeted. Bounded sweep preflights directories and imposes
work/key/owner/slot limits, falling back to keep-all on unknown ownership or
exceeded limits. Directory collection during parsing is still uncapped. This
does not release source strings referenced by ordinary Text fields.

## Scope and State

Experimental, private and default-off for generated Go typed direct and
conversion result publication. It was not adopted. Generated Node/recognition,
engine/VM and TypeScript are outside the implementation.

## Reference Control

None. The two measured variants are reproducible only from the revisions of
record 094; a new candidate must be selected inside the shared generated
runtime.

## Evidence and Implementation

Record 094 and design 034 measure direct cloned-Text retained-heap reduction
from about 2.1 MB to KiB, with material small-input latency/allocation costs.
Conversion shows no meaningful retained-heap or peak-RSS reduction. Process RSS
is not live-heap peak; these results leave default adoption open and make no
broad speed claim.

Evidence records: [094](../094-generated-typed-output-liveness.md).

Source owners: [`ownership and measurement
design`](../../design/records/034-typed-output-liveness.md).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A070](A070-typed-output-bootstrap.md).

[Optimization Catalog](../README.md)
