# 000. Design Record Template

- **Status**: Template
- **Author**: @ornew
- **Date**: 2026-10-09

Use this as a starting point for `NNN-topic.md`, following records 012, 015, 016, 017 and 018. Replace the title,
status and date when copying it. Start an unaccepted design as **Proposed**; use **Implemented** only after the
document describes landed behavior. Keep the author as `@ornew`. Do not add an issue-tracking metadata field: the
record must explain its decisions without relying on mutable issue discussions. Remove these instructions and all prompts
from the finished record. Keep only sections useful to the change; existing records vary with their scope.

## Summary

Describe the problem and the proposed or implemented behavior in a few sentences. Show the user's concrete entry
point or a short input/output example when it clarifies the contract. Label illustrative APIs as proposals.

## Motivation

Explain the use case, current behavior and the gap. Cite the relevant current code, specification, measurements or
external primary sources. Distinguish reproduced failures and measured costs from hypotheses. Pin revisions and
toolchains when evidence depends on them.

## Goals

List the results the design must achieve and the constraints that determine its shape. State measurable acceptance
criteria where possible, including correctness, portability and resource use.

## Non-goals

State related capabilities deliberately outside the design. Explain meaningful scope boundaries without using them
to omit necessary correctness or compatibility work.

## Design

### Public behavior and contracts

Define the API or grammar syntax, inputs, outputs, defaults, errors and edge cases. Specify position units,
ownership/mutation, concurrency and cancellation where relevant. Make examples concrete and keep proposed syntax
distinct from syntax the specification already accepts.

### Implementation and invariants

Describe the components and state that carry the behavior, why they preserve the contract, and how failure,
backtracking, memoization, left recursion or recovery interact when relevant. Focus on decisions and invariants;
the code will provide routine implementation detail.

### Compatibility and backend support

State effects on existing callers/grammars, serialized formats and generated output. Identify closure, recursive VM,
iterative VM, generated Go/typed Go and TypeScript support separately where it differs. Specify migration or version
gates for breaking changes and the documentation/specification that must change with the code.

### Implementation sequence

For a large change, identify dependencies and reviewable stages, with acceptance gates. Do not mark a proposed stage
complete or promise a release date without evidence. A broad roadmap does not replace concrete feature API designs.

## Alternatives considered

For each credible alternative, explain its benefit, cost and reason for choosing or deferring it. Include the current
behavior as a baseline when useful. Record measured rejection reasons, preserve open choices as open, and avoid
presenting an untested performance hypothesis as a decision supported by data.

## Testing

Describe regressions, differential/reference checks and failure cases that establish the contract. Cover affected
backends/units and generated consumers. Include interruption/rollback, malformed input, Unicode, boundaries and
resource retention when applicable. Distinguish completed checks from the proposed validation plan.

## Performance and results

For resource/performance effects, define reproducible workloads, baseline/candidate revisions, tools, sampling and
raw-result locations. Measure the affected operation separately from adjacent work and check representative normal
paths for regressions. Report time, allocations and live memory as appropriate; state uncertainty and unmeasured
paths. Record actual effects in `docs/performance.md` with the code commit and refresh the generated benchmark report
at optimization milestones. Omit this section if the design has no meaningful performance question.

## Limitations and open questions

State remaining limitations, unresolved contracts, decisions requiring maintainer judgment and evidence needed to resolve
them. Do not describe planned behavior as implemented. Update the record, development status, guides and specification
together when stages land or the final design changes.
