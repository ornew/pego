# G100: Generated Node Body Inlining

Generated Go Node parsers can remove per-expression method dispatch for
supported bodies while retaining runtime-owned rule invocation and results.

## Mechanism and Invariants

Value-free plain bodies print from Flow IR as direct rules; the other bodies
(value-building and memoized bodies, cuts, recovery, LR leaders and Operators
Rule lines) print as framed bodies that the runtime calls under the frame it
owns. Rule wrappers still own depth, memoization, naming, rollback and recovery
state. Direct bodies may call framed rules through the rule table. External
rule-table entry bodies leave invocation ownership to their caller.

## Scope and State

Default eligible generated Go Node parsing and recognition. Typed Go prints its
rules from Flow IR through its own dispatch; TypeScript is unchanged.

## Reference Control

No single private option disables this complete Node inlining family. Typed
route controls do not select Node reference bodies.

## Evidence and Implementation

Historical record 076 measures generated JSON CodePoints recognition from 3.169
to 1.899 ms for value-free inlining, with generated source growth. Record 077
then measures selected Node workload median ratios 0.817–0.937 against its own
parent, with different source/build tradeoffs. These comparisons concern
supported bodies and successive revisions; they do not establish all-rule or
TypeScript benefits.

Evidence records: [076](../076-inline-value-free-plain-generated-go-rules.md),
[077](../077-inline-node-expressions-in-generated-go-parsers.md).

Source owners: [`leanRuleOK`](../../../internal/gen/generator.go), [`leanRuleBody`](../../../internal/engine/gen_flowrule.go),
[`generator.flowNodeFrameBody`](../../../internal/engine/gen_flow.go),
[`Node route selection`](../../../internal/engine/gen.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G060](G060-ordinary-typed-direct-rules.md), [M080](M080-direct-code-point-access.md), [M130](M130-short-literal-comparisons.md).

[Optimization Catalog](../README.md)
