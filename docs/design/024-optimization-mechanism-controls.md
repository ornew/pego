# 024. Reproducible Controls for Optimization Mechanisms

- **Status**: Partially Implemented
- **Author**: @ornew
- **Date**: 2026-10-10

## Summary

PEGO's numbered optimization records preserve implementation details and
evidence for individual changes, while commits provide chronology. A
comparison with an older commit can mix the target optimization with later
correctness fixes, compiler changes and unrelated optimizations. This design
defines stable mechanism identities and same-revision reference controls so
that a mechanism can be checked and measured against its alternative at the
same source revision. Selection happens while compiling or generating a
parser, or when building a dedicated reference binary, so the normal matching
path does not pay for a per-call optimization flag. Existing behavior remains
the default. A current mechanism catalog is maintained separately from the
numbered measurement history; control coverage remains incremental.

## Motivation

`docs/optimizations/` describes mechanisms and preserves evidence for numbered
changes. Those records are not a stable inventory of the mechanisms active
today: one mechanism may span several records, and records can be superseded
or refined. The current applicability table is organized by record rather
than by mechanism.

Historical before/after measurements answer whether a change helped relative
to its parent. They do not isolate that mechanism from all changes on the
current branch. A latest-revision reference implementation can answer the
second question, provided it shares the current surrounding code and passes
the same semantic checks. Full benchmark checkpoints answer a third question:
how the whole current tree performs on the standard workload. These results
have different baselines and must remain distinguishable.

The [ordinary typed-cut route](../optimizations/078-local-cuts-in-typed-direct-rules.md#78-local-cuts-in-typed-direct-rules)
is a concrete example. The generated typed Go parser can be emitted through
direct or general rule bodies at generation time. Its reference route does
not need a flag in the generated parser's matching loop. The internal control
selects the general body for the full cut-bearing direct-rule group; it does
not independently disable each local cut flag or other direct-method
mechanism. This pattern should be made reusable where a mechanism has a
meaningful reference path.

## Goals

- Identify current optimization mechanisms independently of numbered history
  entries and backlog IDs.
- Record each mechanism's runtime or generator, applicability, dependencies,
  interactions, reference route and benchmark controls.
- Allow equivalence checks and paired measurements of optimized and reference
  routes built from the same revision.
- Keep control selection out of matching hot paths and preserve today's
  optimized behavior as the default.
- Distinguish same-revision attribution, historical before/after evidence and
  full-suite snapshots in documentation and benchmark reports.
- Maintain the current mechanism catalog separately from historical records.
- Add reference controls incrementally when a concrete attribution question
  justifies them; cataloging a mechanism does not imply that a control exists.

## Non-Goals

- Do not change parser semantics, public parse APIs or the default generated
  output as part of this design.
- Do not add a public flag for selecting implementation variants.
- Do not rewrite all numbered optimization records or claim that every
  existing technique already has a switchable reference implementation.
- Do not enforce timing thresholds in ordinary CI on shared runners.
- Do not treat a historical parent checkout as equivalent to a current-head
  reference route.

## Design

### Mechanism Identity and Inventory

The optimization catalog assigns each current mechanism a stable category ID
independent of numbered measurement records and Issue IDs. IDs use a letter
and a three-digit number: `E` for value elision, `M` for
matching and calls, `R` for reuse and memo/Document state, `A` for allocation
and retention, and `G` for generation and compilation. IDs are not reused. A mechanism page
describes the current implementation and links to code, design records and
numbered evidence pages; the numbered records remain the measurement history.
The catalog matrix maps each mechanism to its applicable backends, default
state and reference-control availability.
Initial allocations leave gaps in steps of ten. Related mechanisms can use
unused numbers in those gaps; existing IDs are never renumbered or reused.

Each mechanism page should state, in compact form:

- the mechanism and its current source owner;
- support for the closure runtime, recursive VM, iterative VM, generated Go
  Node parsing and recognition, typed Go, and TypeScript; mark default
  applicability separately from reference-control availability;
- required mechanisms and known interactions that could change results;
- whether a reference control exists, how it is selected, and what it omits;
- semantic-equivalence tests and reproducible benchmark workloads;
- links to relevant numbered records and the latest applicable evidence.

The inventory should include routine mechanisms even when they were not the
subject of a numbered performance change. It must not imply that a reference
route is implemented when the code has no such route. A mechanism whose
optimized and reference forms cannot yet be isolated should be marked
uncontrolled with the reason.

### Selecting Reference and Optimized Routes

Choose the earliest selection point that can remove the alternative from the
matching loop:

1. **Generation or compilation time.** A private generator/compiler option
   selects which body or instruction form is emitted. The generated parser or
   compiled program contains only the selected form.
2. **Build time.** Where a shared engine implementation cannot be selected
   during generation, build constraints or an equivalent static build
   setting produce separate optimized and reference binaries. The selection
   must not add a per-rule or per-input branch to the normal parser.
3. **Benchmark-only harness.** If neither approach is practical, isolate the
   alternative in a benchmark/test harness. Document the limitation and do
   not present it as a maintained runtime control.

Accept a control only when route selection has no cost in the normal matching
path. Tests must inspect the selected emitted source or compiled path and
verify that no runtime option check remains there. When a control adds code
or setup outside that path, compare the optimized default with an otherwise
identical build that has no control support; the absence of the option's name
from generated text alone is not sufficient evidence.

Controls remain internal unless a separate public API design demonstrates a
user need. They must preserve the optimized default and must not be confused
with per-parse resource options such as the proposed `ParseOption` API for
depth limits. In particular, a public parser option is not an acceptable
shortcut when it would check a mechanism flag in a hot call path.

The [ordinary typed-cut route](../optimizations/078-local-cuts-in-typed-direct-rules.md#78-local-cuts-in-typed-direct-rules)
is a concrete example of the first form: the latest generator emits general
typed-rule bodies for the reference binary and direct bodies for the optimized
binary. It is an internal control, not a stable public option or a promise
that each direct-rule mechanism can be disabled independently.

### Dependencies and Interactions

Represent dependencies and known interactions explicitly. Some optimizations
are only valid when another analysis or lowering pass has run; others affect
the same source region and cannot be toggled independently. A comparison
must state which mechanisms are held fixed, which combinations are tested,
and whether the reference route removes only the target mechanism or a larger
group.

For mechanisms with interacting controls, measure the relevant combinations
on the same source revision. Record order effects when applying one change
alters the benefit of another. Do not sum individual speedups to predict a
combined result. Where the number of combinations is too large, select and
justify a bounded set of representative combinations.

### Equivalence and Performance Evidence

Fast tests should compare reference and optimized routes on the same grammars,
inputs, options and position units. Compare values or trees, errors and
expected sets where observable, recognition results, and rollback behavior
affected by the mechanism. Include generated output or serialized artifacts
when those are part of the route. Fuzz or corpus checks should use a fixed,
reported seed when applicable. The default path must also retain its existing
test coverage.

Measure the optimized and reference variants from the same commit, with the
same toolchain, fixtures and setup. Alternate run order, report paired ratios
and ranges, and keep setup outside the timed region when setup is not the
target. Report time and allocations, and live memory when it is relevant.
State which workload paths and backends were actually measured. Store routine
raw logs locally; publish concise commands, source inputs, toolchain, sample
method and summarized results.

Keep three comparisons separate:

- **Same-revision mechanism comparison:** optimized versus reference control
  at one source revision; this attributes the measured difference to the
  controlled route, subject to its stated scope and interactions.
- **Historical before/after comparison:** a change commit versus its parent;
  this records the total change in those revisions and may include the target
  mechanism plus necessary implementation changes.
- **Full benchmark checkpoint:** the standard benchmark suite at a pinned
  clean revision; this records current overall performance and is not an
  ablation of individual mechanisms.

Do not claim attribution across different baselines or silently substitute a
full-suite snapshot for a paired mechanism measurement.

### CI and Maintenance

Ordinary CI should run semantic equivalence and control-construction tests,
not noisy timing thresholds. Pinned, repeated benchmark runs provide
performance evidence; periodic or manually requested full checkpoints show
the aggregate state. Update a mechanism's inventory entry and its optimization
evidence in the same implementation commit. Keep mechanism controls tested so
that the reference path does not silently drift from the current surrounding
compiler or runtime.

Introduce controls incrementally when changing a mechanism. Do not add a
control to an existing mechanism without a concrete need and a useful
current-head comparison. Keep the applicability and control inventory current
as implementations change.

## Alternatives Considered

### Compare Only Parent and Candidate Commits

This is simple and valuable for historical change records. It cannot isolate
one mechanism from correctness fixes or other changes accumulated since the
parent. Keep it as historical evidence, not as the sole current attribution
method.

### Add a Public Runtime Option for Every Mechanism

This would make variants easy to select but expands the public API and can
add flag checks to matching loops. Most users need stable parser behavior,
not implementation controls. Keep experimental selection private and choose
it before parsing.

### Maintain Separate Long-Lived Reference Branches

Branches can preserve an old implementation but drift from the current
compiler, tests and runtime. Same-revision controls make the differences
explicit and can be tested together. Historical branches may still be useful
for reproducing old measurements, but they are not the current reference
contract.

### Add All Controls Before Further Optimization Work

A full control framework would delay correctness and performance work, and
some mechanisms have no useful alternative to select. Add each control
alongside the change it can validate, then keep the control-coverage inventory
current as mechanisms change.

## Implementation Sequence

1. Keep numbered evidence records and measurement pages as the historical
   evidence layer. Maintain a separate current-mechanism catalog with stable
   IDs and pages that link to implementation, design and evidence.
2. For each new optimization, add a private latest-revision reference route
   when a meaningful alternative can be built without matching-path overhead;
   add parity tests and paired benchmark instructions with the change.
3. Keep the applicability and control inventory current as implementations
   change. Add controls only where a concrete attribution question justifies
   their maintenance cost.

## Limitations and Open Questions

Some engine mechanisms may not admit a clean reference route without
maintaining a second implementation; those may need a build-only variant or
remain uncontrolled. Generation-time controls can also increase generator
and test complexity, so each must justify its maintenance cost with a real
comparison.

The catalog does not claim that all current optimizations are independently
switchable. Mechanism identity and the current-mechanism index are implemented;
backend applicability is documented in the catalog, while control coverage
remains incremental. Existing mechanism pages do not imply that every route has a
same-source reference implementation.
