# 041. Opt-in Missing Literal Recovery

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Propose explicit, bounded insertion of a missing literal for editor parses. A missing token has an empty range and
distinct value and diagnostic; it does not pretend that source text exists. Ordinary parsing remains strict unless
the caller enables insertion. Existing consuming `#recover(skip=...)` retains its current behavior.

The syntax and APIs below are proposals, not accepted language or exported API contracts:

```pego
def main = name ";" #missing $$
def name = (?a-z)+
```

```go
node, err := parser.Parse("name", pego.WithMissingTokens(16))
```

Here the missing semicolon is represented at the actual EOF position and accompanies a recovered diagnostic.
Without the option, this input fails as it does today.

## Motivation and scope

[Current recovery](../../spec/attributes.md#recover) rewinds a failed expression and consumes a nonempty skip.
Both [design 006](006-error-reporting-and-recovery.md) and `recoverAttr` in
[genrt/runtime.go](../../internal/engine/genrt/runtime.go) deliberately reject an empty skip. Removing that check
would neither identify an expected token nor give repetitions, memoization or backtracking an insertion contract.

The first stage accepts `#missing` with no arguments on a nonempty literal only. It inserts that entire literal,
not a guessed prefix after partial matching. Classes, arbitrary expressions, synthesized identifiers, automatic
repair searches and changes to ordered choice are outside this stage. An annotation permits insertion wherever
its literal fails; grammar authors can constrain that point with explicit lookahead. It does not implicitly prefer
a later strict alternative over an earlier recovering alternative.

## Design

### Invocation, values and errors

Propose `WithMissingTokens(n)` as a Go parse option on the engine and generated Go. Zero, including omission,
disables insertion; a positive value bounds synthesis attempts during one invocation; a negative effective value
is a configuration error before execution. Options resolve once, in order, with the last value winning. Generated
TypeScript proposes `ParseOptions.missingTokens`, a nonnegative safe integer, on its existing options-aware APIs.
There is no mutable global switch or generation-time change to the strict default.

An annotated literal first attempts an ordinary match. After failure it restores input, captures, variables and
recovery state to the literal's start, then may insert. No input is consumed. Propose reserved node type `Missing`
with `Start == End`, empty `Text`, and string fields `expected` (the literal) and `message` (the diagnostic).
The error uses the insertion position, line and column in the invocation's position unit. Literal contents remain
the original string; an empty Text is never replaced with those contents. Anchors and positional actions observe
the real position. A surrounding `#error` supplies its label to the insertion diagnostic by the same explicit
label-scope rules used for failures; composition with outer `#recover` must not duplicate a diagnostic on success.

Successful whole-input parsing returns a tree and the existing `SyntaxErrors` collection, in committed encounter
order, including insertion diagnostics. If the whole parse fails, return only the final ordinary syntax error as
with current recovery. Exceeding the attempt budget is instead a distinct fatal `MissingTokenLimitError`, exposing
limit and position: return no batch tree and never turn resource exhaustion into successful recovery.
Recognition can use insertion when explicitly enabled and returns the same diagnostics with no public tree.
Recognition still evaluates actions needed to supply predicate values; it skips unrelated actions.

Add `Missing` to the reserved type list and inference as a union of the literal's ordinary Match and Missing.
Propose the existing Error-style assignability rule for node destinations. Go exports a typed `Missing` value
with Span, Text, Expected and Message; TS exports its corresponding `type:"Missing"` interface. Explicit node
unions and open/any conversions preserve it; a destination concrete struct/terminal that cannot hold Missing
receives its existing nil/null recovery representation, with the diagnostic retained. A known list converts its
elements with these same rules, rather than dropping or inventing source tokens. Aliases use their canonical
representation. Document this distinction so consumers needing missing-token identity declare a union or retain
the Node tree. Top-level scalar/action restrictions remain unchanged. Reserving the new name requires a reported
grammar migration for a preexisting user type named Missing and deterministic generated-helper collision tests.

### Progress, rollback and caches

Maintain a rollback-aware ledger keyed by insertion site identity and absolute position on the current candidate
path. A second insertion at the same site and position fails ordinarily. Marks, choice resets and Pratt candidate
restoration undo ledger additions and diagnostics together with captures; cut retains its existing choice scope.
Lookahead always disables insertion, including callees, so recovery cannot make a negative lookahead unexpectedly
fail or leak a Missing value into a capture made by a positive lookahead.

Keep a separate monotonic attempt counter which rollback cannot replenish. Each new synthesis and each memo
replay that installs an insertion event on the path consumes one attempt. Revisiting an event already installed
on that path installs nothing. Budget costs can depend on attempted alternatives, including abandoned ones; this
is a resource limit, not a guarantee that a tree with at most n missing nodes will be accepted. Diagnostics and
ledger entries from abandoned paths remain invisible. Budget exhaustion aborts even on an abandoned alternative.

Retain the language's repetition rule: the first successful iteration without input progress counts once and
then stops, including generated, projected, resumed and streamed repetitions. An insertion never supplies input
progress. LR may establish an initial successful empty seed; further growth must advance beyond the already
successful seed. Pratt prefix/postfix parts must consume input, but an infix part may be empty under the existing
longest-candidate rules. A successful infix application with a nullable RHS stops after its first zero-progress
application. Insertion supplies no consumed input and must preserve these distinctions; do not tighten existing
nullable grammar acceptance or let a Missing value alone justify another growth round.

Initially disable completed memo reuse for insertion sites and their transitive callers when insertion is enabled.
Keep mandatory LR growing seeds, carrying the selected seed's insertion events with its errors; replay validates
the ledger and charges new installations before returning the seed. Each growth attempt starts with the caller's
saved ledger and keeps only the winning seed's events. Never publish provisional seed state. This conservative
policy avoids a completed memo answer depending on a different insertion history or remaining budget. Strict
mode retains normal memo behavior. Any later contextual memo optimization needs its own differential gate.

Document reparses start with a fresh ledger and counter. Insertion-dependent cached rules are not reused in
enabled mode; other entries still obey examined-range, positional-value and recovered-error invalidation rules.
Abort or cancellation discards provisional LR/memo/resumption state before retry. Returned Missing nodes obey
ordinary Node alias ownership: mutable Document reuse may move earlier trees; Clone preserves retained versions.

### EOF, streams and backend gates

Actual EOF permits insertion; a stream temporarily lacking bytes does not. Wait for enough input to establish
literal failure or for actual EOF. After insertion inside a stream element, ordinary commit rules apply; no later
repair can retract a delivered element. Recovery wrapping a streaming repetition remains forbidden. Release
discarded input only after captures, diagnostic text and inserted values are detached from parser scratch.

| Backend | Proposed gate |
| --- | --- |
| Closure, recursive VM, iterative VM | Both CodePoints and Bytes; one semantic reference matrix |
| Generated Go Node and typed direct/conversion | Same option, ledger, progress and conversion contracts |
| Generated TS Node and typed conversion | Both units, string/Uint8Array inputs, exact option validation |
| Current engine Document and streams | Fresh per-run insertion state; reader EOF and commit tests |
| Generated Document/streams | Supported only when their separately proposed APIs land |

Add syntax/JSON AST validation, formatting, typing and lint checks together. Saved AST records need a compatible
new file-format version for the new construct. VM support needs a new instruction-set version with a specified
insertion instruction/site operand and structural validation; do not reinterpret RECOVER or permit its empty skip.
Old files retain their behavior; old loaders reject new versions. Compiled recognition modules must carry the same
sites and option semantics. The numeric versions are allocated against the then-current specification.

## Implementation sequence and validation

1. Add syntax, typed Missing mapping and closure implementation behind the opt-in option; establish strict-mode
   output equivalence and grammar migration diagnostics.
2. Specify/version VM encoding, add both VMs, saved/load round trips and malformed site/operand rejection.
3. Add generated Node/typed Go and TS parity, then Document and stream integration. Do not advertise a backend
   until its full matrix passes; unsupported enabled insertion is an explicit error.

Proposed tests cover absent/zero/positive/negative limits, callback options once, EOF and partial literals,
multibyte/invalid UTF-8, partial reader prefixes, chained insertions, nested repetitions and minima, choices/cuts,
lookahead, labels, consuming recovery, LR cycles and Pratt longest-match ties. Check budget exhaustion after
backtracking and during seed replay; compare diagnostics and values across backends and fresh versus edited
Document parses. Exercise union/any/CST/list/alias/concrete nil mappings, retained results, concurrent/reentrant
calls, callback stop, cancellation and subsequent pooled retry. Property tests must establish termination and
that strict mode has exactly the old acceptance set.

## Alternatives and performance gates

Keep consuming recovery only as the baseline. Treating an empty skip as recovery lacks a site identity, expected
token and budget; reject that shortcut. A search for minimum-cost edits can offer better repairs but changes PEG
selection and requires a separate design. Fabricating a typed terminal with expected text would simplify callers
but falsely reports source text; choose an explicit Missing value. Always enabling annotated insertion removes the
strict baseline; prefer the invocation option.

No measurements are claimed. Compare exact parent, strict candidate and a static generation/build control with
insertion accounting omitted, then enabled valid/editor-invalid cases. Measure time, allocations, live heap,
attempt/committed counts, source size and Document edit/reparse separately. Include broad backtracking, LR/Pratt,
ordinary JSON-like input, EOF streams and both units. Extra dormant parser state can change GC costs; a zero option
alone is not an adequate cost control. Run paired serialized samples with pinned inputs/toolchains and retain raw
results outside the repository. Acceptance requires bounded attempts and no unapproved strict-path regression.
