# M100: Absent Expectation Filter

An expectation already present in the active error record needs an exact
duplicate check. A fixed-size membership filter proves many new expectations
absent before scanning the ordered set.

## Mechanism and Invariants

A hash of the complete expectation ID, including its message flag, selects one
of 64 bits. An unset bit permits insertion without a scan; a set bit requires
the exact ordered check. Collisions cannot remove distinct expectations.
Farthest-position reset, isolation, memo/recovery marks and parser recycling
preserve or clear the filter consistently. Saturation retains linear duplicate
work.

## Scope and State

Default Go expectation paths in closure, both VMs, generated Node/recognition
and typed output. TypeScript is unchanged.

## Reference Control

No maintained same-source filter-disable control exists.

## Evidence and Implementation

Historical record 075 measures typed Python `_pydecimal.py` CodePoints from
26.723 to 23.026 ms and 12.14 to 11.82 MB, with similar scoped Bytes results.
Added parser/mark footprint and hash cost are explicit tradeoffs. Saturated
filters retain linear duplicate scans, and the unadopted TypeScript filter
showed no clear gain. These results do not establish universal constant-time or
zero-overhead expectation handling.

Evidence records: [075](../075-exclude-absent-expectations-before-scanning.md).

Source owners: [`parser.expect and record
marks`](../../../internal/engine/core/runtime.go), [`generated expectation
filter`](../../../runtime/runtime.go), [`VM label/recovery
state`](../../../internal/engine/vm/vm.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A110](A110-expected-set-ids.md).

[Optimization Catalog](../README.md)
