# 030. Generate Typed TypeScript Values from Parse Trees

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Allow `pego gen -lang ts -types` and `gen.TypeScript{Types: true}`
to generate interfaces, aliases and unions corresponding to grammar types (a grammar join type becomes a TypeScript union),
plus typed parse-result entry points. The first implementation runs
the existing Node parser once and converts its completed result into typed
values. Existing `parse`, `parseRule`, recognition and Node serialization stay
unchanged. Direct typed matching is a separate future optimization requiring
its own correctness and measurement gates.

## Motivation

`internal/gen/tsgen/generator.go` previously rejected `GenOptions.Types`. Generated TS
returns `Node` values whose fields require runtime name lookup and consumer
casts. Generated Go already maps grammar types to public values through
`internal/gen/gogen/types.go`; its metadata and converter semantics provide the reference
for the new TS mapping. The feature acceptance criterion requires actual
consumer use, value equivalence and declaration exports, not merely compiling
a generated module.

The implementation is opt-in. Generated Go conversion remains the semantic
reference for typed values; the focused and full-suite validation and measured
tradeoffs are recorded below.

## Goals

- Export every declared grammar type and alias with its own name from
  `types.ts`, including Unicode names.
- Give the configured start rule a static result type with accurate nullable
  fields, aliases and recovery values.
- Preserve the Node parser's whole-input, action, predicate, cut, recovery,
  LR/Operators Rule, error, Unicode, integer and position-unit contracts.
- Convert returned values after parsing, without changing the representations
  actions and field-reading predicates observe while parsing.
- Keep the generated module dependency-free, ES2020-compatible, and usable by
  runtimes that erase TS syntax.

The Unicode-name goal records the lexical contract at the time of this design.
Grammar identifiers are now ASCII-only; Unicode input and string values remain
supported.

## Non-Goals

Scalar top-level rules remain rejected by the existing node-producing rule
contract. Scalar fields and aliases remain supported. This proposal does not
change type inference, grammar/saved formats, Go parsing, streaming, ready
parsers, 32-bit portability, or recognition's result type. Named typed dispatch
is a separate feature outside this initial API. This proposal does not promise
faster typed parsing or introduce a public arbitrary-Node conversion API.

## Design

### Public API

Mirror the current TS position-unit and options-aware pairs:

```ts
export interface TypedParseResult<T> {
  readonly value: T | null;
  readonly error: Error | null;
}

export function parseAST(
  input: string | Uint8Array, unit?: Unit,
): TypedParseResult<StartValue>;
export function parseASTWithOptions(
  input: string | Uint8Array, options?: ParseOptions,
): TypedParseResult<StartValue>;
```

`StartValue` above denotes the inferred generated type, not a mandatory new
export. The initial entry points select the configured start rule only.
Existing generic named-rule parsing remains available through `parseRule`.

The options-aware entry point reads and validates options once before parsing,
including property getters. Invoke the internal resolved `run` path, rather
than resolving options and calling another options-aware public function.
Use the generated depth default, zero reset, safe-integer validation and units
already defined by the TS runtime. The unit entry point retains the existing
unit normalization.

### Value Mapping

Prefer plain data objects and discriminated interfaces, with lowercase
metadata matching existing TS Node conventions:

```ts
export interface Span { readonly start: number; readonly end: number }
export interface Match extends Span {
  readonly type: "Match";
  readonly text: string;
}
export interface Pair extends Span {
  readonly type: "Pair";
  readonly Key: Match | null;
  readonly Value: Match | null;
}
```

Grammar struct fields currently must begin with an uppercase letter
(`check.go`), so lowercase `type`, `start`, `end`, `text` and `message` metadata
do not collide with valid fields. Preserve original grammar field spelling.
Terminal interfaces use their original type-name discriminator and `text`.
A recovered Error converts to a helper such as `RecoveredError`, containing
`type:"Error"`, span, text and message. Keep diagnostic `Error` and
`SyntaxErrors` classes separate from this value interface.

Mapping details:

| Grammar value | TS representation |
| --- | --- |
| struct or terminal | generated interface; nullable where Go conversion can produce a nil pointer |
| `int` field/alias | `number | bigint`; absent nonoptional field is `0` |
| `string`, `bool` field/alias | `string`, `boolean`; absent nonoptional fields are `""`, `false` |
| `[]T` | readonly array of mapped elements; null for absent/nil list, distinct from an empty array |
| optional value | mapped value or `null`, including optional scalars |
| declared alias | exported alias of the complete target representation, preserving nullability |
| canonical declared node join | TypeScript union of converted members plus `RecoveredError`; nullable in zero/optional positions |
| CST or record | existing `Node`, preserving its Node representation |
| mixed/open or undeclared joins, `node`, `terminal`, `_` | conservative TypedValue, following Go any-conversion and including null; inferred nil/never use this same recipe because recovery can produce Error values |

Generate these open-value unions from the known grammar declarations plus
Match, RecoveredError and CST Node. The `_` mapping also includes
number|bigint, string and boolean. Conversion of a dynamically typed List stays
Node where Go's anyValue does; statically known list conversion returns an
array. Use `unknown` only if metadata cannot safely describe a permitted value.

A CST member in a join retains the Node class. Consumers first use
`instanceof Node`, then discriminate converted members by `type`; Node's broad
string `type` must not be advertised as providing complete literal narrowing.

Expose all declared aliases, including normalized duplicate joins, chained,
scalar, optional, list and CST aliases. TS structural types do not need Go's
union marker methods. Use the existing inferred metadata and normalization.

Mixed scalar/node joins require the existing Go any-conversion semantics,
not a pure-node join selector. For example, `Word | int` preserves canonical
number/bigint values, converts a Word Node, converts a recovered Error into
RecoveredError even though it is not a written member, and preserves CST Nodes.
Missing values become null. Inferred nil/never use the same conservative
TypedValue mapping: a nil-producing or nonproductive rule can recover to an
Error value. Other permitted dynamic values arriving through
`_` must follow the same conversion, rather than be filtered or rejected.
Export such mixed/open aliases conservatively as the complete TypedValue
representation, documenting their written members; a narrower TS union would
promise filtering that the existing Go converter does not perform. Test
recovery and dynamic values explicitly. Canonical declared pure node joins
retain their member mapping plus RecoveredError. Nonmatching nodes become null unless the join has CST members,
in which case Go preserves them as Node. Joins without a canonical declared
node-join recipe use any-conversion, as Go does. No checker or inference rule changes are required.

The declarations of the grammar live in `types.ts`, a module of its own, and
`parser.ts` refers to them as `Types.<name>`. The names of the runtime and of the
generated parser (Parser, Actx, Rule, OperatorsRuleLine, the R0/Q0 families, Node,
Value) therefore cannot collide with a grammar type, and a grammar type keeps its
own name. The few names that `types.ts` declares itself (`Span`, `TNode`,
`StringNode`, `RecoveredError`, `TypedParseResult`, `TypedValue` and the import
`RuntimeNode`) are reserved: a grammar type with one of them fails generation with
an error that lists them, rather than being renamed. Preserve original grammar names
in discriminators. Account for legal TS binding names and quote property keys where
needed; never normalize Unicode spelling.

### Results, Failure and Recovery

A successful parse returns the mapped value and null error. A successful nil
result returns the selected representation's zero value (normally null).
Ordinary parse, action or depth failure returns null with the original error;
invalid option errors do likewise. Top-level rules cannot produce scalar
results, so numeric zero is a field concern rather than a failed scalar rule
result. After recovery, convert the value and preserve the same `SyntaxErrors`
object and diagnostics. An Error where a struct/terminal is expected becomes
null; an Error in a join remains `RecoveredError`; list conversion follows
Go's existing Error/empty-list and element-null rules. Do not infer success
from `value !== null`: nil-valued rules can succeed.

No existing entry point changes its return shape. `parseAST` performs one Node
parse followed by conversion.

### Ownership and Conversion

Converters receive only the completed internal Node result. Allocate fresh
plain objects and arrays, copy spans and scalar values, and never mutate the
source Nodes or action-visible fields. Retain existing public CST Nodes where
mapping requires them. Returned objects are invocation-owned and cannot be
cleared by parser reuse or another parse. `readonly` is a TS checking contract,
not a promise of runtime freezing. Internal aliasing/object identity is not a
cross-backend equality promise; semantic values and spans are.

Use an explicit conversion work stack for deep structures and nested lists,
and an invocation-local cache keyed by both Node identity and conversion
representation. Iteration alone does not prevent exponential duplication of a
shared DAG: a fold or LR action can store the same capture into two fields at
each growth step. Cache each `(Node, representation)` once, with representation
IDs derived from canonical converter recipes. Distinguish struct/terminal,
join/`_` and list element conversions: the same Error Node can become null in
one position and RecoveredError in another. Cache null using an explicit
presence check, not a null-as-missing sentinel.

Register allocated object/array shells before scheduling their children, fill
all pending work before publishing the result, and reuse cached shells on
repeated edges. Equivalent converter recipes may share representation IDs;
otherwise their output caches stay separate. Work and storage follow the
number of distinct Node/representation pairs and visited edges, not the number
of expanded root-to-leaf paths. The internal parse graph must remain unchanged.
Repeated references may share a converted readonly object within one result;
callers cannot assume every field holds a distinct object. Cross-backend
pointer identity is not an equality contract.

An LR/fold result may be much deeper than the parser's call stack. Conversion
must not introduce a new unclassified JS stack overflow or reinterpret maxDepth
as a value-tree depth limit. Work stack and cache belong to one invocation only,
including reentrant calls. No cross-invocation converter/cache pooling. Cache
bookkeeping adds memory and lookup costs for tree-shaped inputs, and both the
Node graph and converted graph can remain live during conversion; measure this
cost as well as the avoided DAG expansion.

### Unicode, Positions and Integers

Copy Node text and coordinates unchanged. CodePoints positions count decoded
Unicode scalars; Bytes positions count UTF-8 bytes, never JavaScript UTF-16
indices. String input replaces lone surrogates with U+FFFD as the current
runtime does. Invalid byte input matches as U+FFFD per offending byte, while
Bytes-mode terminal/action text preserves raw invalid byte `b` as U+DC00+b
(U+DC80–U+DCFF). Conversion must preserve these surrogate escapes rather than
repairing them again. Valid encoded U+FFFD and invalid bytes remain
observable as different text when the current unit requires it.

Keep TS's current signed 64-bit, wrapping integer arithmetic and canonical
representation: safe integers are numbers, larger magnitudes are bigint.
Copy existing integer field values without Number coercion; do not force all
ints to bigint or inherit the executing Go host's native-int width. Positions
remain numbers under the existing input-position contract.

Typed plain objects containing bigint are not automatically JSON-compatible:
`JSON.stringify` can throw, and Bytes-mode escaped text needs explicit handling.
The existing Node `marshal` remains a Node API. First-stage documentation must
show deliberate exact-integer/text normalization for transport and must not
claim byte-for-byte typed JSON parity. An exact typed serializer can be a
separately scoped API rather than an implicit lossy `toJSON` hook.

### Compatibility and Implementation Sequence

1. Implement the public shape and export policy above, deriving a TS mapping
   from existing type metadata, with deterministic exports and consumer tests.
2. Enable the existing generation flag and Go generation option for TS; emit
   post-parse converters and typed entry points. Keep untyped output and API
   behavior unchanged. Update CLI validation, guides and support tables.
3. Complete differential/ownership/depth gates and measure source/build and
   conversion costs before claiming readiness.
4. Only later consider direct typed matching. Require a TS analogue of Go's
   full `runtimeOK` guard, including every struct field and field-reading
   action/predicate. Unsupported layouts must retain the converter path.
   Preserve action semantics, cuts, memoization, LR growth, Operators Rule attempts,
   recovery and public ownership. Compare direct output with the converter
   reference using a static generation control; do not promise eligibility or
   a speedup before evidence.

Closure/recursive VM/iterative VM and generated Go remain unchanged. Generated
TS gains opt-in typed output. No format migration is needed; regeneration is
required to receive new exports, and reserved-name cascades can require
consumer import/type spelling changes.

## Alternatives Considered

- **Interfaces over existing Nodes only.** Cheap emission but leaves runtime
  field lookup/casts, and does not produce the Go-style converted values.
- **Direct typed matching first.** May avoid Node allocations but combines a
  new public contract with a second value runtime and field-read hazards.
  Defer until the converter path is an executable reference.
- **Classes for all grammar types.** Enables instanceof checks but adds runtime
  declarations, constructors and inheritance policy. Prefer erasable
  interfaces and plain discriminated values.
- **Replace `parse` with typed results under `-types`.** Breaks Node callers.
  Keep explicit additive typed entry points.
- **Force integers to number or bigint.** Number loses precision; all-bigint
  changes the established canonical representation. Preserve number|bigint.

## Testing

The test suite covers the cases below. Focused checks and full integration
validation passed; the results below quantify the opt-in converter costs:

- Strict consumer compilation and execution: exported interfaces, every alias,
  union narrowing and nullable results. Include grammar types named like the
  runtime and generated declarations (Parser, Actx, Rule, OperatorsRuleLine, Node,
  R0), and assert that the reserved names fail generation. Assert imports and
  declaration exports of the real `runtime.ts`, `types.ts` and `parser.ts`, not just
  generated-file compilation.
- Differential conversion versus existing Node results and Go typed mappings
  on common representable values. Normalize backend shape/64-bit integer
  transport explicitly; do not confuse serialization equality with value
  equality. Cover empty/nil lists, optionals, Error in all positions, CST/open
  values and mixed scalar/node joins with recovered/dynamic values.
- Whole-input failures, action errors, recovery, cuts, LR, Operators Rule, field-reading
  actions, omitted fields, option getters applied once before parsing, generated
  and overridden depth limits. Retain results/diagnostics across repeated and
  reentrant parses and verify source Nodes are unchanged.
- Both units; string and Uint8Array inputs; astral characters, combining text,
  lone surrogates, encoded U+FFFD, malformed and EOF-truncated UTF-8. Verify
  terminal text and exact spans, including predicates comparing raw byte text.
- Integer boundaries around ±(2^53−1) and signed 64-bit extrema, overflow,
  negative division/remainder, optional ints and invalid non-integer values.
- Deep LR/fold/list results that stress conversion independently of parse
  nesting. Add a fold and an LR growth action that store the same accumulator
  or capture in two fields repeatedly. Compare small controls with the ordinary
  converter oracle, then verify large DAG controls visit/allocate according to
  distinct Node/representation counts without expanding paths exponentially.
  Assert shared results and representation-specific Error conversion, and
  compare warmed retained heap and process RSS for shared-DAG and ordinary-tree
  controls. Isolated instantaneous conversion peak remains unmeasured.
- Test ES2020 tsc consumers and real Node runtime first; run the project's
  available browser/other-runtime checks before claiming those environments
  were verified.

The focused test suite and full integration validation passed on Node.js
22.18.0 and TypeScript 5.9.3, Darwin/arm64. The full suite covered the
checks listed above, including strict consumers, both position units,
recovery, retained ownership, integer and Unicode boundaries, and shared DAGs.
The generated Go parsers and TypeScript runtime were unchanged by regeneration.

The typed replay compared 168 results across 23 grammars against generated
Go's Node-conversion path, in both units, with explicit integer, text and CST
normalization. Strict consumers verified imports and declaration exports.
`PEGO_TS_TYPED_DIR` exported matching typed/untyped sources, strict consumers,
declarations and small/large inputs for field-heavy, capture-free, recovery
and shared-DAG controls. The checked-in `bench/ts-typed.mjs` harness has SHA-256
`ed66f403f3de4e2c5ccddb8605dee8e175002bed1f5c8a356b6edf3a854e44da`.

Full root tests, vet, all ten parser-module suites, parser freshness,
regeneration and focused source-review checks passed. Strict TypeScript
consumers and real Node 22 runtime checks passed. Browser, Deno and Bun were
not tested.

## Performance and Results

The typed API converts the ordinary Node result after parsing. It is an
opt-in compatibility feature, not a runtime optimization: conversion can cost
substantially more than returning Nodes, and both representations may coexist.

Measurements used the implementation in this commit on base `1cf6840`
(runtime source `73f7c01`), Go 1.27.1, Node 22.18.0 and TypeScript 5.9.3 on
Apple M3 Max, Darwin/arm64. Large inputs contain 4,000 field-heavy items,
24,000 capture-free code points, 2,000 good/bad recovery pairs, or 6,000 shared
fold/LR levels. Small inputs are the first exported fixture inputs.
The timing comparisons used five alternating fresh-process pairs. For ordinary
large inputs, each process used three warmups and twelve timed calls; the shared
DAG used one warmup and three timed calls. Small inputs used 1,000 warmups and
10,000 calls. Heap measurements used 20 warmups, one result per process, three
explicit GC cycles per stage, and three pairs. They report post-GC retained
heap for bounded result/helper frames, not peak memory. Process RSS includes
imports, verification and warmup, so it is not a conversion peak. Build timing
used five pairs with OS cache warm and includes TypeScript process startup,
checking, declaration emission and output writes; it is not pure compiler time.

| Large input | Unit | Typed conversion / Node time | Post-GC retained heap, Node → typed |
| --- | --- | ---: | ---: |
| Fields | Code points | 2.98× | 2.95 → 1.40 MB |
| Fields | Bytes | 2.04× | 2.96 → 1.41 MB |
| Capture-free | Code points | 1.02× | approximately unchanged |
| Capture-free | Bytes | 0.98× | approximately unchanged |
| Recovery | Code points | 1.11× | 3.38 → 5.29 MB |
| Recovery | Bytes | 1.09× | 3.43 → 5.33 MB |
| Fold | Code points | 3.53× | 2.86 → 0.50 MB |
| Fold | Bytes | 2.30× | 2.87 → 0.50 MB |
| Left recursion | Code points | 2.25× | 2.25 → 0.51 MB |
| Left recursion | Bytes | 1.97× | 2.25 → 0.52 MB |

These are paired medians, not universal multipliers. The capture-free timing
ranges included parity; the recovery, fold and left-recursion examples show
conversion cost. Retained heap depends on graph shape: fields and recovery can
retain more or less; typed fold and left-recursion results use less warmed
heap here. Both Node and typed DAG outputs preserve shared edges. The negative or near-zero capture-free
heap deltas are measurement noise, not meaningful savings. Low-warmup DAG
heap samples varied substantially; the retained-heap table uses the matched
20-warmup protocol and includes runtime bookkeeping, rather than exact object
footprints. After release, deltas from the warmed baseline ranged from about
−33 KB to +18 KB. This does not establish a leak or its absence in other uses.

For small inputs, typed conversion took 1.24–1.27× the Node path in the
field-heavy CodePoints/Bytes and capture-free CodePoints cases. In an untyped
Node control, all four measured timing ranges included parity with the
untyped baseline. This separates conversion cost from changes to ordinary
Node parsing; it does not establish zero overhead for every grammar.

Generated typed source added 4,987–5,871 bytes and declarations added
1,104–1,682 bytes across the fixtures; parent and current untyped output was
byte-identical. Fresh TypeScript build-time ratios were 1.013–1.033×, with all
paired ranges including parity. Fresh-process first-import ratios were 1.009–1.033×, with every paired range
including parity. They measure native TS loading, erasure and module evaluation
inside an already started Node process; OS file caches are uncontrolled. They
do not establish process startup, cold-filesystem or universal import costs.

The results describe typed conversion and its retained graph for these
workloads. They do not establish lower peak memory, a general speedup, or
behavior in untested JavaScript runtimes. Direct typed matching remains a
separate design with its own eligibility, ownership and differential gates.

### Reproduction

Export the checked fixtures with Node 22.18+ and TypeScript 5.9 on `PATH`:

```bash
typed_fixtures="$(mktemp -d)"
PEGO_TS_TYPED_DIR="$typed_fixtures" go test ./internal/engine \
  -run '^TestGeneratedTSTyped(Workloads|SharedDAG)$' -count=1

# Field-heavy large CodePoints: five alternating fresh-process pairs.
for round in 1 2 3 4 5; do
  if (( round % 2 )); then modes=(node ast); else modes=(ast node); fi
  for mode in "${modes[@]}"; do
    env -u NODE_OPTIONS node --expose-gc bench/ts-typed.mjs \
      --fixtures "$typed_fixtures" --module "$typed_fixtures/fields/parser.ts" \
      --fixture fields --size large --unit codepoints --mode "$mode" \
      --variant "${round}-${mode}" --phase time --warmup 3 --count 12 \
      --retain 1 --input-kind string --entry-mode direct --revision current
  done
done
```

The harness prints bounded JSON summaries, source/input fingerprints, elapsed
time and process RSS. Select `bytes`, `capture-free`, `recovery`, `fold` or `lr`
with the matching module path. Use one warmup and three timed calls for large
fold/LR, and 1,000 warmups/10,000 calls for small cases. For memory, use
`--phase memory --warmup 20 --retain 1`, and three alternating pairs. Result
creation, observation and release finish in helper frames before GC sampling.
To check inactive typed output, compare Node mode from `untyped/parser.ts` and
`parser.ts`, using identical inputs, units and call counts.

Source/declaration checks cover all five fixture modules. Build comparisons
stage typed and untyped modules under the same `parser.ts` filename, with an
identical bounded Node-only adapter using `parse`, `parseWithOptions` and
`recognize`; the strict flags match `runTypedTSFixture` and add
`--declaration --emitDeclarationOnly`. Each observation uses a fresh compiler
process and output directory; five alternating pairs cover fields,
capture-free and fold. Typed-only API declaration checks are separate from
these matched build controls. Keep raw process records local and summarize
paired medians, ranges and uncertainty.

## Limitations and Follow-Up Scope

The implemented API is configured-start parsing through parseAST and
parseASTWithOptions, following existing TS call conventions. Interfaces are
plain discriminated values with readonly grammar fields and lowercase metadata.

Mixed/open aliases are conservative because existing any-conversion semantics
can retain recovered and dynamically typed values beyond their written members.
CST Node unions require instanceof narrowing. Exact typed serialization, typed
named dispatch and direct typed matching are separately scoped follow-ups.
The integer/Unicode representation and scalar-rule rejection remain established
contracts. Browser, Deno and Bun behavior remains unverified, and the measurements
above apply only to the stated workloads and runtime.
