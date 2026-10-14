# A170: Operators Rule Attempt Scratch

Trying every operator candidate should not allocate a fresh attempt object and
capture-save slice for each try.

## Mechanism and Invariants

`longest` keeps the best attempt by value and uses a parser-owned buffer to save
captures across reset. The winner still follows longest match and
declaration-order ties. Iterative execution passes the attempt through dedicated
parser/frame state instead of enlarging every generic step result. Scratch
cannot alias the winning published value or erase live captures.

## Scope and State

Default Go Operators Rule routes in closure, both VMs and generated Node/typed
Go. TypeScript uses its own JavaScript candidate objects; Go by-value attempt
allocation is not applied there.

## Reference Control

No independent maintained same-source per-candidate allocation control exists.

## Evidence and Implementation

Records 011/013 measure lower Minilang/Operators allocation counts. Record 013
also records a rejected generic-result layout that slowed the iterative VM about
20%. The adopted route's historical effects do not imply every Operators Rule
allocation is eliminated.

Evidence records: [011](../011-generated-parsers-catch-up-with-changes-410.md),
[013](../013-allocation-free-operators-rule-attempts-and-pooled-operators-rule-frames-in-the-engine-and-vms.md).

Source owners: [`parser.longest and saved
captures`](../../../internal/engine/runtime.go), [`Operators Rule attempt
state`](../../../internal/engine/ivm.go), [`generated
longest`](../../../internal/engine/genrt/runtime.go), [`typed Operators Rule
attempts`](../../../internal/engine/genrt/typed.go).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [E030](E030-operators-line-value-elision.md), [A030](A030-parser-frame-reuse.md), [G160](G160-typed-operators-line-bodies.md).

[Optimization Catalog](../README.md)
