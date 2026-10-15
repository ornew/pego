# 059. Grammar Fixture Runner

- **Status**: Accepted
- **Author**: @ornew
- **Date**: 2026-10-14

## Summary

Define a versioned JSON suite for repeatable grammar examples and a `pego test`
command/API to run them. A fixture can assert accepted, recovered, or rejected
outcomes, canonical node JSON, source spans, and syntax-error locations.
Optional backend equivalence is a separate result, never a substitute for
expected values. Golden updates refresh only observed node and location fields
for one explicitly selected backend, recording explicit clears when a selected
backend has no inherited value.

## Motivation

Grammar-specific tests can assert parser behavior, but PEGO has no common fixture
contract for parse outcomes, values, locations, and backend
comparisons. A shared format lets grammar authors reuse examples across
packages without treating backend agreement as the correctness oracle.

## Goals

- Define a versioned, strict fixture format for accepted, recovered, and
  rejected examples.
- Assert canonical node JSON and explicit node and syntax-error locations.
- Run an explicit, ordered selection of PEGO execution backends.
- Keep each backend's fixture result independent of cross-backend equivalence.
- Produce deterministic JSON reports and readable expected/actual differences.
- Update only node/location goldens, without changing inputs or outcome
  expectations.

## Non-Goals

- Generate inputs, mutate documents, minimize failures, or perform
  coverage-guided fuzzing.
- Compare PEGO with an external parser or treat reference deviations as correct.
- Run generated Go or TypeScript parsers; this design covers PEGO's own
  execution backends.
- Update fixture inputs or outcome expectations automatically.

## Design

### Fixture Format

Version 1 is a JSON object. The following is a proposed complete example; fields
whose defaults are omitted are described below.

```json
{
  "version": 1,
  "grammar": "grammar.pego",
  "start": "main",
  "unit": "codepoints",
  "backends": ["closure", "bytecode", "bytecode-iterative"],
  "equivalence": true,
  "cases": [
    {
      "name": "accepts-a",
      "input": "a",
      "expect": {"outcome": "accepted", "start": 0, "end": 1}
    },
    {
      "name": "rejects-b",
      "input": "b",
      "expect": {
        "outcome": "rejected",
        "errors": [{"pos": 0, "line": 1, "column": 1}]
      }
    }
  ]
}
```

`version` is required and unsupported versions fail to load. `grammar` is
required and names a PEGO source (`.pego`) or grammar JSON (`.json`) file,
resolved relative to the suite file unless absolute. The proposed v1 accepts
source and JSON grammars; compiled `.pegoc` support is deferred pending a scope
decision. `start` defaults to `main`. `unit` defaults to `codepoints`; `bytes`
selects UTF-8 byte positions. `backends` defaults to `default`. Supported names
are `default`, `closure`, `bytecode`, and `bytecode-iterative`; duplicates are
removed while preserving order. Unknown fields, invalid values, trailing JSON
values, duplicate case names, and an empty `cases` array are errors.
Typed top-level fields reject explicit `null`, including optional fields such as
`unit` and `backends`; omit an optional field to select its default.

Each case has a unique nonempty `name`, an explicitly present `input` string,
and a required `expect` object, even when `byBackend` supplies an outcome. An
empty input is valid and differs from a missing `input` field; the loader must
validate field presence rather than relying on
the decoded string's zero value. `expect.outcome` is required in the shared
expectation or a backend override. Optional fields are:

- `node`: expected PEGO node JSON, compared after canonicalizing JSON object
  key order. Array order and JSON scalar types remain significant.
- `start` and `end`: root node span in the suite's selected unit. Values are
  nonnegative, and `end` cannot precede `start` when both are supplied.
- `errors`: an ordered list of syntax-error `{pos, line, column}` records. Lines
  and columns are required and one-based; `pos` is required and nonnegative.
  Omit `errors` to make no assertion; use an empty array to assert that no
  syntax errors were reported. Updates preserve the distinction between an
  omitted field and an explicitly empty array such as `errors: []`.

`byBackend` maps backend names to partial expectation overrides. For each field,
an override replaces the shared expectation only when that field is present;
other fields inherit. This permits a known backend-specific result without
weakening the common assertions. The override does not alter backend runtime
selection. `byBackend` and each override must be objects; explicit `null` is
invalid. Explicit `null` is also invalid for typed expectation fields
(`outcome`, `start`, `end`, and `errors`). `node: null` remains valid and asserts
a JSON null node value. Load validates each effective expectation after merging
the shared values with each selected or explicitly declared backend's override,
including constraints that depend on multiple fields such as `start <= end`.
An override may use `clear` to suppress inheritance for optional fields:
`clear: ["node", "start", "end", "errors"]`. A cleared field cannot also
appear in that override. Unknown or repeated clear names are invalid. During
`-update`, absent observed node/span values are recorded as clears when they
would otherwise inherit a shared golden in a backend-specific override. No
observed syntax errors clear an inherited nonempty `errors` list; an explicit
`errors: []` remains an empty assertion. An explicit empty `byBackend: {}`
targets backend-specific updates without changing shared values.

### Acceptance and Recovery

`outcome` is `accepted` for a clean whole-input match, `recovered` for a
whole-input match completed after `#recover`, and `rejected` for a syntax
failure. Runtime errors are reported separately. A nil root node does not
change the outcome; a successful or recovered parse with no node is represented
with `node: null`. Syntax-error locations and node presence are independent
observations.

### Runtime Errors and Reports

The report contains one result per case/backend pair, with the observed parse
outcome, optional node/span, syntax-error locations, and mismatches. A
top-level `passed` value is true only when every fixture expectation and every
requested equivalence result passes. Text output identifies the case and
backend and prints expected and actual mismatch values plus runtime diagnostics.
JSON output uses ordered case and equivalence arrays and stable report fields.

Parser runtime errors are distinct from syntax errors and are recorded on the
case/backend result without assigning a parse outcome. The runner continues
with later isolated cases; invocation-wide cancellation stops the run. Error
text is diagnostic only and is not compared as a golden because wording may
change. Whether fixtures can assert an error category is unresolved.

Backend equivalence is opt-in and requires at least two distinct resolved
backends. `default` and `closure` resolve to the same backend and cannot satisfy
this requirement by themselves. Exact duplicate names are removed while
preserving order; aliases that resolve to one runtime backend are rejected.
For each case,
equivalence compares parse outcome, canonical node JSON (including null), root
span, and ordered syntax-error locations. It reports differing fields
separately from fixture mismatches. Matching backends cannot turn a wrong
golden into a pass, and a fixture expectation may pass while equivalence fails.
How runtime-error categories participate in equivalence remains unresolved.

### CLI and Go API

The proposed command is:

```text
pego test -f suite.json [-format text|json]
            [-backend closure,bytecode] [-equivalence] [-update]
```

`-backend` overrides the suite selection while preserving order. `-equivalence`
requests the separate comparison. `-update` requires one explicit backend and
cannot be combined with equivalence. The command exits nonzero for fixture or
equivalence failures; invalid suite/grammar input and compilation errors are
command errors.

The proposed Go API is `fixture.Load(path)`, `fixture.Run(suite,
fixture.RunOptions)`, and `fixture.UpdateGoldens(suite, report, backend)`. `Run`
returns the same structured report used by the CLI. File loading and execution
options are sufficient for the initial capability; a programmatic in-memory
suite builder is not required for v1. `RunOptions` carries a context and work
limits. Context cancellation stops the invocation, while a work-limit error is
recorded for its case/backend and later cases continue.

### Golden Updates

Golden update requires exactly one explicitly selected backend. It updates node
JSON and locations only; it never changes `input` or outcome expectations. It
must refuse to write if the observed parse outcome disagrees with the expected
outcome, while still reporting the mismatch. It validates all selected outcomes
before changing in-memory expectations or writing, so a later mismatch cannot
partially update earlier cases. Backend-specific checks use the effective
outcome after applying the shared expectation. When `byBackend` is present, only
the selected backend's override is updated and shared expectations remain
untouched, including when `byBackend` is an explicitly empty object. Updates
record clears for inherited values absent from the observed backend and preserve
explicit `errors: []`. The operation writes a temporary file in the suite
directory and renames it into place. It preserves the byte
spelling of untargeted raw node JSON, while targeted goldens may be reformatted.
An update is refused when the selected result has a runtime error. The outcome
remains an explicit assertion that a reviewer edits separately after a
successful outcome check.

### Implementation and Invariants

The fixture package owns strict manifest loading, grammar loading, validation,
expectation comparison, equivalence comparison, and golden updates. Compile one
parser per distinct resolved backend, then execute cases and backends in stable
order.
Every selected backend result is compared with the expectation that applies to
that backend. Backend equivalence is calculated independently.

JSON node comparison canonicalizes object key order without changing array
order. Error locations remain ordered. Writes are atomic and leave fixture
inputs and outcome expectations unchanged. Suite or grammar setup errors stop
execution; runtime errors are recorded per case/backend and isolated cases
continue, while invocation-wide cancellation stops the run. Golden updates
must preserve explicit field presence, including empty arrays, and support
clearing a backend override without changing shared expectations.

## Compatibility and Backend Support

This is additive and does not change grammar syntax, parser semantics, node
serialization, or existing parser APIs. The proposed execution backends are
closure, recursive bytecode, and iterative bytecode. `default` follows PEGO's
public default. Backend-specific expected values document observed differences
without changing runtime selection.

Version 1 should reject unknown fields and unsupported versions, rather than
silently ignore them. Grammar JSON is included in the proposed v1. Compiled
grammar support is open. No generated parser output changes are required.

## Alternatives Considered

- **Unversioned JSON:** smaller initially, but incompatible changes become
  ambiguous. Require an integer version.
- **Treat backend agreement as the assertion:** agreement may preserve a shared
  bug. Keep explicit fixture expectations authoritative and equivalence
  separate.
- **Update every field:** could silently turn a regression into a new
  expectation. Restrict updates to node/location goldens and keep acceptance
  manual.
- **Run every backend unconditionally:** increases routine work and hides the
  comparison scope. Use an ordered explicit selection and make equivalence
  opt-in.
- **Runtime-error handling:** record the case/backend error and continue with
  independent cases. Invocation-wide cancellation still stops the run; setup
  failures remain suite-level errors.
- **Use external reference outputs as implicit goldens:** conflates comparison
  with correctness. Keep expected values explicit.

## Testing

Validate malformed JSON, unsupported versions, unknown fields/backends,
duplicate names, missing outcome, missing `input` versus empty `input`, and
invalid grammar paths. Cover successful parses with both non-nil and nil nodes,
rejected parses, and recovered parses; node values; spans; ordered syntax-error
locations; codepoint and byte positions on multibyte input; backend-specific
overrides and explicit clears. Verify equivalence requires distinct resolved
backends (`default` plus `closure` is insufficient) and independently of
expectation pass/fail, including both passing equivalence with a failed golden
and failing equivalence with
passing individual expectations. Verify deterministic output, update scope,
atomic writes, refusal to write on outcome mismatch or runtime error, preserved
input/outcome, backend-specific updates, explicit empty `errors: []`, and raw
untargeted golden preservation. Exercise syntax errors separately from runtime
errors and test the chosen runtime-error reporting/continuation policy. Test CLI
dispatch/help as well as the package API.

Acceptance requires the fixture package API and integrated CLI command to pass
these cases across the supported backends.

## Performance and Results

There are no performance measurements. Suite loading, grammar compilation, and
parsing should be timed separately before making claims about routine cost.
Running multiple backends increases test time; the default should remain the
single `default` backend, with equivalence explicitly requested.

## Limitations and Open Questions

- Decide whether fixtures may assert a runtime error category. Runtime error
  text remains diagnostic and is not golden data.
- Confirm whether `unit` and backend defaults should remain implicit;
  recommendation: codepoints and `default`, respectively.
- Decide whether backend-specific expectations belong in version 1; they allow
  intentional deviations without weakening common expectations.
- Confirm whether update creates missing node/location goldens. It must refuse
  to write when the observed parse outcome disagrees with the expected outcome.
- Decide whether compiled `.pegoc` suites are required in the initial release.
- Decide whether equivalence should compare runtime-error categories; do not
  compare unstable error text.
- Backend equivalence does not explain why a known deviation exists. If fixture
  authors need local rationale, define a comment/description convention in a
  later compatible version.
