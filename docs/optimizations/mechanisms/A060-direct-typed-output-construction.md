# A060: Direct Typed Output Construction

Generated Go can construct the requested typed structs while parsing, avoiding
an intermediate Node tree and a later conversion walk.

## Mechanism and Invariants

The typed runtime mirrors Node matching, rollback and errors but uses generated
type-specific constructors and typed Match/Error wrappers. Supported grammars
return the same value as Node conversion. Unsupported typed result shapes keep
conversion fallback. Result spans, sharing and freshness follow the typed
contract; direct body inlining and output allocators are separate optimizations.

## Scope and State

Default eligible generated Go typed entry points. Node parsing remains available
and TypeScript typed exports use their own Node-conversion implementation.
Closure and VMs do not produce these generated Go structs directly.

## Reference Control

No universal public direct-versus-conversion implementation selector exists.
Internal conversion fixtures compare selected result shapes; they are not an
all-grammar direct-runtime disable control.

## Evidence and Implementation

Record 048 compares typed and Node operations and several implementation steps.
JSON typed parsing 9.9 to 7.7 ms returns a different representation, so that is
not a single-mechanism ablation. Direct/conversion retention and allocation must
remain separate.

Evidence records: [048](../048-a-typed-runtime-for-parseast.md).

Source owners: [`typed constructors and conversion
generation`](../../../internal/engine/gen_types.go),
[`tparser`](../../../internal/engine/genrt/typed.go), [`typed runtime
design`](../../design/012-typed-values.md).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G060](G060-ordinary-typed-direct-rules.md), [A070](A070-typed-output-bootstrap.md), [A250](A250-typed-object-slabs.md), [A260](A260-typed-list-slabs.md).

[Optimization Catalog](../README.md)
