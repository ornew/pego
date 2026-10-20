# Optimization Catalog

This catalog describes optimization mechanisms, their backend scope and
measured evidence. The mechanism index below assigns stable IDs to current
mechanisms and maps each one to its applicable backends and control state.
Numbered records preserve implementation details, tradeoffs and
reproduction commands for individual changes; commits provide chronology. The
[performance document](../performance.md) keeps the measurement method and
latest full-suite analysis.

## Mechanism Index

Each stable ID uses a category letter and a three-digit number: `E` (value elision), `M` (matching and calls), `R` (memo and Document reuse), `A` (allocation and retention), and `G` (generation and compilation). Initial allocations use steps of ten, leaving gaps for related mechanisms. New mechanisms may use those unused numbers; existing IDs are never renumbered or reused. IDs are independent of numbered evidence records and backlog IDs.

Backend cells describe default applicability: `✓` applies; `Partial` applies only to the stated eligible subset; `Private off` marks a private/default-off implementation; `No` means the backend could use the mechanism but does not implement it; and `—` means the mechanism has no corresponding path. The separate state and control columns distinguish default selection from same-source comparison support. Shared compilation, setup and editor mechanisms identify their scope in the Path scope column; they are not runtime matching claims.

Backend headers use C for Closure, RVM for recursive VM, IVM for iterative VM, Go Node / Recognize for generated Go Node parsing and recognition, Typed Go for generated `ParseAST`, and TS for generated TypeScript. Path scope abbreviations GN and TG refer to those generated Go paths.

Each linked mechanism page describes its source, eligibility, interactions, control/reference scope, and evidence. Numbered records remain historical measurement reports and may cover multiple mechanisms. Their results apply only to the workloads, backends, and baselines stated in each record.

### E — Value Elision

| ID | Mechanism | Path scope | C | RVM | IVM | Go Node / Recognize | Typed Go | TS | Default state | Same-source control |
|:--|:--|:--|:-:|:-:|:-:|:-:|:-:|:-:|:--|:--|
| [E010](mechanisms/E010-value-free-rule-bodies.md) | Value-free body lowering | C/RVM/IVM/GN/TG/TS; eligibility-based; no universal same-source control | Partial | Partial | Partial | Partial | Partial | Partial | Default | None maintained |
| [E020](mechanisms/E020-value-free-rule-twins.md) | Value-free CST twins | All six; distinct memo identities, excludes LR cycles | Partial | Partial | Partial | Partial | Partial | Partial | Default | None maintained |
| [E030](mechanisms/E030-operators-line-value-elision.md) | Operators Rule line value elision | All six; line-specific eligibility | Partial | Partial | Partial | Partial | Partial | Partial | Default | None maintained |
| [E040](mechanisms/E040-recognition-lowering.md) | Recognition value/capture/action lowering | Core and generated recognition | Partial | Partial | Partial | Partial | — | Partial | Default | None maintained |
| [E050](mechanisms/E050-projected-repetition-values.md) | Projected repetition results | All six; eligible subset, including TS projection | Partial | Partial | Partial | Partial | Partial | Partial | Default | Compile Options.noProjections |
| [E060](mechanisms/E060-concat-input-fusion.md) | Fuse eligible concat inputs | Eligible action inputs | Partial | Partial | Partial | Partial | Partial | Partial | Default | None maintained |

### M — Matching and Calls

| ID | Mechanism | Path scope | C | RVM | IVM | Go Node / Recognize | Typed Go | TS | Default state | Same-source control |
|:--|:--|:--|:-:|:-:|:-:|:-:|:-:|:-:|:--|:--|
| [M010](mechanisms/M010-first-character-dispatch.md) | First-character guards for ordered choices | C/RVM/IVM/GN/TG/TS; eligibility-based | Partial | Partial | Partial | Partial | Partial | Partial | Default | None maintained |
| [M020](mechanisms/M020-character-class-tests.md) | Backend-specific character-class tests/representations | C/RVM/IVM/GN/TG/TS; distinct implementations by backend | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | Default | None maintained |
| [M030](mechanisms/M030-literal-matching.md) | Loaded-input literal matching | All six; input paths differ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | Default | None maintained |
| [M040](mechanisms/M040-character-scan-loops.md) | Value-free character repetition scan loops | Value-free character repetitions | Partial | Partial | Partial | Partial | Partial | Partial | Default | None maintained |
| [M050](mechanisms/M050-plain-rule-calls.md) | Calls to eligible uncaptured/unmemoized rules | C/RVM/IVM/GN/TG/TS; each backend has separate call route | Partial | Partial | Partial | Partial | Partial | Partial | Default | None maintained |
| [M060](mechanisms/M060-left-recursion-growth.md) | Reuse state during left-recursion growth | Go seed-growth storage | ✓ | ✓ | ✓ | ✓ | ✓ | — | Default | None maintained |
| [M070](mechanisms/M070-vm-predecoded-match-data.md) | Predecoded VM literals and ASCII class masks | VM-program setup; predecoded literal/class data is used on RVM/IVM matching paths | — | ✓ | ✓ | — | — | — | Default | None maintained |
| [M080](mechanisms/M080-direct-code-point-access.md) | Read decoded code points in direct Go bodies | GN/TG direct eligible bodies; CodePoints fast path | — | — | — | Partial | Partial | — | Default | None maintained |
| [M090](mechanisms/M090-diagnostic-line-index.md) | Lazy line-start table for diagnostics | All six; streams retain committed-position scan | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | Default | None maintained |
| [M100](mechanisms/M100-absent-expectation-filter.md) | Fixed-size absent expectation filter | C/RVM/IVM/GN/TG Go only; TS unchanged | Partial | Partial | Partial | Partial | Partial | — | Default | None maintained |
| [M110](mechanisms/M110-compact-vm-backtracking-state.md) | Compact VM entries and separate label state | RVM/IVM only | — | ✓ | ✓ | — | — | — | Default | None maintained |
| [M120](mechanisms/M120-unboxed-text-comparisons.md) | Native text equality without boxing | All six eligible comparison paths | Partial | Partial | Partial | Partial | Partial | Partial | Default | None maintained |
| [M130](mechanisms/M130-short-literal-comparisons.md) | In-place short literal comparison | GN/TG direct Go CodePoints only | — | — | — | Partial | Partial | — | Default | None maintained |

### R — Memo and Document Reuse

| ID | Mechanism | Path scope | C | RVM | IVM | Go Node / Recognize | Typed Go | TS | Default state | Same-source control |
|:--|:--|:--|:-:|:-:|:-:|:-:|:-:|:-:|:--|:--|
| [R010](mechanisms/R010-per-position-memo-chains.md) | Per-position memo chains and key storage | C/RVM/IVM/GN/TG/TS; exact per-backend route | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | Default | None maintained |
| [R020](mechanisms/R020-deferred-memoization.md) | Defer memo allocation until a second same-position call | C/RVM/IVM/GN/TG/TS; no universal control | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | Default | None maintained |
| [R030](mechanisms/R030-sparse-seen-position-storage.md) | Sparse deferred-memo seen positions with dense small-input path | C/RVM/IVM/GN/TG/TS; core build-tag and generation controls differ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | Default | pego_reference_sparse_memo; GenOptions.disableSparseMemo (TypeScript) |
| [R040](mechanisms/R040-memo-entry-retirement.md) | Retire obsolete memo entries | C/RVM/IVM only; active seed ownership preserved | ✓ | ✓ | ✓ | — | — | — | Default | None maintained |
| [R050](mechanisms/R050-bounded-variable-bindings.md) | Bound variable binding histories | All six default; persistent prefix replacement | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | Default | None maintained |
| [R060](mechanisms/R060-chunked-document-input.md) | Chunked Document text storage | C/RVM/IVM Document only | ✓ | ✓ | ✓ | — | — | — | Default | pego_reference_document_pieces |
| [R070](mechanisms/R070-document-memo-edits.md) | Gap-buffer memo splice and lazy advancement | C/RVM/IVM Document only | ✓ | ✓ | ✓ | — | — | — | Default | None maintained |
| [R080](mechanisms/R080-lazy-code-point-offsets.md) | On-demand code-point offsets | Core recognition and Document offset tables | ✓ | ✓ | ✓ | — | — | — | Default | None maintained |
| [R090](mechanisms/R090-moving-document-trees.md) | Move reused Document results | C/RVM/IVM Document only | ✓ | ✓ | ✓ | — | — | — | Default | None maintained |
| [R100](mechanisms/R100-document-repetition-resumption.md) | Resume saved long repetition runs | C/RVM/IVM Document only | ✓ | ✓ | ✓ | — | — | — | Default | None maintained |
| [R110](mechanisms/R110-document-run-record-retention.md) | Retain unchanged-generation repetition records | C/RVM/IVM Document only | ✓ | ✓ | ✓ | — | — | — | Default | None maintained |
| [R120](mechanisms/R120-transient-rule-memo-policy.md) | Avoid ordinary memoization for transient rules | All six; Document remains eager | Partial | Partial | Partial | Partial | Partial | Partial | Default | None maintained |
| [R130](mechanisms/R130-document-snapshot-record-sharing.md) | Share exact immutable records in Document snapshots | C/RVM/IVM Document snapshots; candidate/reference build tag | ✓ | ✓ | ✓ | — | — | — | Default | pego_snapshot_sharing_reference |
| [R140](mechanisms/R140-share-document-snapshot-child-runs.md) | Share immutable child-run backing between exact Document snapshot records | Document snapshot child-run backing; immutable records only | ✓ | ✓ | ✓ | — | — | — | Default | pego_snapshot_child_chunks_reference |
| [R150](mechanisms/R150-cache-snapshot-child-run-descriptors.md) | Cache immutable child-run descriptors in SnapshotNode records | Document snapshot record access; caches immutable child-run descriptors | ✓ | ✓ | ✓ | — | — | — | Default | No cache-off control; reference retains this cache |
| [R160](mechanisms/R160-relative-snapshot-position-overlays.md) | Store per-version relative positions over shared snapshot records | Private Document snapshot comparison; no matching-path change | Private off | Private off | Private off | — | — | — | Private off | `pego_snapshot_position_overlays` |

### A — Allocation and Retention

| ID | Mechanism | Path scope | C | RVM | IVM | Go Node / Recognize | Typed Go | TS | Default state | Same-source control |
|:--|:--|:--|:-:|:-:|:-:|:-:|:-:|:-:|:--|:--|
| [A010](mechanisms/A010-compact-node-representation.md) | Compact Node fields and interned kinds | C/RVM/IVM/GN; TG/TS do not use these Node layouts | ✓ | ✓ | ✓ | ✓ | — | — | Default | None maintained |
| [A020](mechanisms/A020-node-and-list-slabs.md) | Slab storage for Nodes and child lists | Go Node storage; typed Go uses typed arenas | ✓ | ✓ | ✓ | ✓ | — | — | Default | None maintained |
| [A030](mechanisms/A030-parser-frame-reuse.md) | Pooled iterative VM frames and plain-call frame | IVM; distinguish Operators Rule attempts below | — | — | ✓ | — | — | — | Default | None maintained |
| [A040](mechanisms/A040-action-operand-scratch.md) | Reusable action operands and plumbing | All six by route; no TS cross-call pool | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | Default | None maintained |
| [A050](mechanisms/A050-streaming-state-reuse.md) | Bounded stream input and memo storage | C/RVM/IVM streaming only | ✓ | ✓ | ✓ | — | — | — | Default | None maintained |
| [A060](mechanisms/A060-direct-typed-output-construction.md) | Construct typed output directly | TG eligible direct path; conversion remains fallback | — | — | — | — | Partial | — | Default | None maintained |
| [A070](mechanisms/A070-typed-output-bootstrap.md) | Independent initial typed allocations before slab growth | TG only; default applied, control `disableTypedOutputBootstrap` | — | — | — | — | ✓ | — | Default | GenOptions.disableTypedOutputBootstrap |
| [A080](mechanisms/A080-generated-node-capture-scratch.md) | Reusable generated Node capture scratch | Generated Go Node capture frames | — | — | — | ✓ | — | — | Default | — |
| [A090](mechanisms/A090-typed-output-liveness-cleanup.md) | Clear unreachable generated typed output slots | TG measured; not adopted or implemented | — | — | — | — | — | — | — | — |
| [A100](mechanisms/A100-document-vm-value-stack.md) | Reuse bounded VM value stack for Document parses | RVM/IVM Document paths only | — | ✓ | ✓ | — | — | — | Default | pego_reference_document_vm_values |
| [A110](mechanisms/A110-expected-set-ids.md) | Interned expectation IDs and shared windows | All six; backend storage differs | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | Default | None maintained |
| [A120](mechanisms/A120-source-token-text.md) | Source substring token text | All six subject to input representation; stream separately | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | Default | None maintained |
| [A130](mechanisms/A130-closure-lambda-arenas.md) | LIFO lambda call/context arenas | C only; VM allocation separate | ✓ | — | — | — | — | — | Default | None maintained |
| [A140](mechanisms/A140-construction-tracking-release.md) | Release completed construction tracking | All six default; TS timing unmeasured | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | Default | None maintained |
| [A150](mechanisms/A150-vm-position-and-lambda-chunks.md) | VM boxed positions and lambda chunks | RVM/IVM only | — | ✓ | ✓ | — | — | — | Default | None maintained |
| [A160](mechanisms/A160-whole-input-scratch-pooling.md) | Pool whole-input scratch across calls | C/RVM/IVM/GN/TG; TS N/A | ✓ | ✓ | ✓ | ✓ | ✓ | — | Default | None maintained |
| [A170](mechanisms/A170-operators-attempt-scratch.md) | Reusable Operators Rule candidate storage | C/RVM/IVM/GN/TG by route; TS does not use Go by-value attempt | Partial | Partial | Partial | Partial | Partial | — | Default | None maintained |
| [A180](mechanisms/A180-typed-frame-reuse.md) | Release/reuse typed runtime capture frames | TG only | — | — | — | — | ✓ | — | Default | None maintained |
| [A190](mechanisms/A190-evaluation-context-reuse.md) | Reusable action evaluation context | All six per parser; no TS cross-call pool | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | Default | None maintained |
| [A200](mechanisms/A200-stream-capture-frame-reuse.md) | Reuse completed stream capture frames | C/RVM/IVM; pego_reference_stream_frames | ✓ | ✓ | ✓ | — | — | — | Default | pego_reference_stream_frames |
| [A210](mechanisms/A210-bounded-stream-text-snapshots.md) | Share bounded immutable stream text | C/RVM/IVM; pego_reference_stream_text | ✓ | ✓ | ✓ | — | — | — | Default | pego_reference_stream_text |
| [A220](mechanisms/A220-stream-prefix-isolation.md) | Isolate live stream prefix ownership | C/RVM/IVM; pego_reference_stream_prefix | ✓ | ✓ | ✓ | — | — | — | Default | pego_reference_stream_prefix |
| [A230](mechanisms/A230-stream-undo-cleanup.md) | Clear inactive stream undo backing | C/RVM/IVM; pego_reference_stream_undo | ✓ | ✓ | ✓ | — | — | — | Default | pego_reference_stream_undo |
| [A240](mechanisms/A240-stream-vm-stack-cleanup.md) | Clear inactive stream VM value stack | RVM/IVM; pego_reference_stream_stack | — | ✓ | ✓ | — | — | — | Default | pego_reference_stream_stack |
| [A250](mechanisms/A250-typed-object-slabs.md) | Typed object/terminal slab growth | TG; disableTypedObjectSlabs | — | — | — | — | ✓ | — | Default | GenOptions.disableTypedObjectSlabs |
| [A260](mechanisms/A260-typed-list-slabs.md) | Typed list slab growth | TG; disableTypedListSlabs | — | — | — | — | ✓ | — | Default | GenOptions.disableTypedListSlabs |
| [A270](mechanisms/A270-shared-empty-capture-frame.md) | Share immutable capture-free frame | All six; capture-free scopes only | Partial | Partial | Partial | Partial | Partial | Partial | Default | None maintained |

### G — Generation and Compilation

| ID | Mechanism | Path scope | C | RVM | IVM | Go Node / Recognize | Typed Go | TS | Default state | Same-source control |
|:--|:--|:--|:-:|:-:|:-:|:-:|:-:|:-:|:--|:--|
| [G010](mechanisms/G010-generated-helper-sharing.md) | Share exact generated helper bodies | GN/TG/TS default; literals independent | — | — | — | ✓ | ✓ | ✓ | Default | GenOptions.disableMethodSharing |
| [G020](mechanisms/G020-lazy-backend-preparation.md) | Lazy one-time setup of optional engine recognizer/VM backends | First-use engine setup; closure recognition only, ordinary closure graph is eager | Partial | Partial | Partial | — | — | — | Default on demand | None maintained |
| [G030](mechanisms/G030-constructor-dependency-analysis.md) | Sample constructor dependency propagation | Shared sampling analysis, runtime cells N/A | — | — | — | — | — | — | Default setup path | None maintained |
| [G040](mechanisms/G040-copy-on-change-attribute-normalization.md) | Reuse unchanged AST paths during attribute normalization | Shared compile/generation preprocessing only, not runtime matching | — | — | — | — | — | — | Default setup path | Compile Options.eagerAttributeCopies |
| [G050](mechanisms/G050-generated-work-control-specialization.md) | Specialize generated Go ordinary methods and helpers while retaining checks | GN/TG only; adopted; effect scope from record 096 | — | — | — | ✓ | ✓ | — | Default | GenOptions.disableOrdinaryWorkSpecialization |
| [G060](mechanisms/G060-ordinary-typed-direct-rules.md) | Ordinary typed direct bodies | TG eligible default; later extensions separately keyed | — | — | — | — | Partial | — | Default | None maintained |
| [G070](mechanisms/G070-literal-text-predicates.md) | Pure literal-text disjunction lowering | GN/TG/TS eligible default | — | — | — | Partial | Partial | Partial | Default | GenOptions.disableLiteralTextDispatch |
| [G080](mechanisms/G080-equal-capture-type-reuse.md) | Reuse structurally equal capture types | Shared type analysis, runtime cells N/A | — | — | — | — | — | — | Default setup path | None maintained |
| [G085](mechanisms/G085-action-free-positional-items.md) | Skip positional item typing for action-free rules | Shared type analysis, runtime cells N/A | — | — | — | — | — | — | Default setup path | None maintained |
| [G090](mechanisms/G090-variable-read-components.md) | Variable reads by dependency component | Shared compiler metadata, runtime cells N/A | — | — | — | — | — | — | Default setup path | None maintained |
| [G100](mechanisms/G100-generated-node-body-inlining.md) | Inline eligible Go Node expressions | GN partial default; separate value-free/node eligibility | — | — | — | Partial | — | — | Default | None maintained |
| [G110](mechanisms/G110-one-pass-code-point-preparation.md) | Decode/validate/build offsets in one pass | CodePoints input preparation; GN/TG eager, core recognition lazy, TypeScript uses its own decoder | Partial | Partial | Partial | Partial | Partial | Partial | Default by input path | None maintained |
| [G120](mechanisms/G120-lsp-position-checkpoints.md) | LSP UTF-16/code-point checkpoints | Editor preprocessing only; backend cells N/A | — | — | — | — | — | — | Default editor path | None maintained |
| [G125](mechanisms/G125-bounded-diagnostic-prefixes.md) | Bound displayed lint expression prefixes | CLI/LSP lint diagnostics only; runtime backend cells N/A | — | — | — | — | — | — | Default lint path | None maintained |
| [G130](mechanisms/G130-generated-literal-sharing.md) | Share generated literal tables | GN/TG default; TS experimental/private off | — | — | — | ✓ | ✓ | Private off | Go default; TS off | GenOptions.disableLiteralSharing; enableTSLiteralSharing (off) |
| [G140](mechanisms/G140-typed-local-cuts.md) | Local cuts in ordinary direct typed rules | TG default eligible; disableTypedCuts | — | — | — | — | Partial | — | Default | GenOptions.disableTypedCuts |
| [G150](mechanisms/G150-typed-left-recursion-bodies.md) | Inline typed LR leader bodies | TG default eligible; disableTypedLRBodies | — | — | — | — | Partial | — | Default | GenOptions.disableTypedLRBodies |
| [G160](mechanisms/G160-typed-operators-line-bodies.md) | Inline typed Operators Rule line bodies | TG default eligible; disableTypedOperatorsRuleBodies | — | — | — | — | Partial | — | Default | GenOptions.disableTypedOperatorsRuleBodies |
| [G170](mechanisms/G170-typed-framed-cuts.md) | Scoped cuts in unfinished typed bodies | TG default eligible; disableTypedFramedCuts | — | — | — | — | Partial | — | Default | GenOptions.disableTypedFramedCuts |
| [G180](mechanisms/G180-typed-frame-layouts.md) | Match direct framed capture layouts | TG default eligible; disableTypedFrameLayouts | — | — | — | — | Partial | — | Default | GenOptions.disableTypedFrameLayouts |
| [G190](mechanisms/G190-typed-recovery-bodies.md) | Inline eligible typed recovery bodies | TG default eligible; disableTypedRecoveryBodies | — | — | — | — | Partial | — | Default | GenOptions.disableTypedRecoveryBodies |
| [G200](mechanisms/G200-typed-local-capture-layouts.md) | Complete typed Go-local capture layouts | TG default eligible; disableTypedLocalLayouts | — | — | — | — | Partial | — | Default | GenOptions.disableTypedLocalLayouts |

Records [025](025-discarding-separators-in-the-example-grammars.md) and [055](055-tracing-hook-on-the-call-path.md) remain in the evidence archive but are not optimization mechanisms: the first covers grammar-author output choices and the second measures tracing-instrumentation cost.

## Optimization History

## Records 1–25
- [001. Value-free rule bodies, value-free twins, transient rules (0b2c903)](001-value-free-rule-bodies-value-free-twins-transient-rules-0b2c903.md)
- [002. Frame pooling in the iterative VM (8fab402)](002-frame-pooling-in-the-iterative-vm-8fab402.md)
- [003. Node fields as a slice (54a5774)](003-node-fields-as-a-slice-54a5774.md)
- [004. Expected sets as interned IDs on a shared stack (11a6ce3)](004-expected-sets-as-interned-ids-on-a-shared-stack-11a6ce3.md)
- [005. Memo table with per-position chains (cf77e4f)](005-memo-table-with-per-position-chains-cf77e4f.md)
- [006. Slab allocation for nodes, child lists and frames (5d03bf2)](006-slab-allocation-for-nodes-child-lists-and-frames-5d03bf2.md)
- [007. Recognition mode (3f7e9bf)](007-recognition-mode-3f7e9bf.md)
- [008. Fast path for unmemoized calls, `SCAN`, nesting limit (d2db87d)](008-fast-path-for-unmemoized-calls-scan-nesting-limit-d2db87d.md)
- [009. Value-free Operators Rule lines; discarding trivia in example grammars (936857c)](009-value-free-operators-rule-lines-discarding-trivia-in-example-grammars-936857c.md)
- [010. Zero-copy token text; reusable evaluation context](010-zero-copy-token-text-reusable-evaluation-context.md)
- [011. Generated parsers catch up with changes 4–10](011-generated-parsers-catch-up-with-changes-410.md)
- [012. Memoizing rules that read variables](012-memoizing-rules-that-read-variables.md)
- [013. Allocation-free Operators Rule attempts and pooled Operators Rule frames in the engine and VMs](013-allocation-free-operators-rule-attempts-and-pooled-operators-rule-frames-in-the-engine-and-vms.md)
- [014. One operand stack for VM expression code](014-one-operand-stack-for-vm-expression-code.md)
- [015. Allocation-free action plumbing in all backends](015-allocation-free-action-plumbing-in-all-backends.md)
- [016. Start positions and lambdas from chunks in the VMs](016-start-positions-and-lambdas-from-chunks-in-the-vms.md)
- [017. ASCII bitmaps for character classes in the VMs](017-ascii-bitmaps-for-character-classes-in-the-vms.md)
- [018. Memoizing from the second call at a position](018-memoizing-from-the-second-call-at-a-position.md)
- [019. A plain call path for unmemoized rules without captures](019-a-plain-call-path-for-unmemoized-rules-without-captures.md)
- [020. Splicing the memo table on Document edits](020-splicing-the-memo-table-on-document-edits.md)
- [021. Changes 18 and 19 in the generated parsers](021-changes-18-and-19-in-the-generated-parsers.md)
- [022. Bounded memory and less copying in stream parsing](022-bounded-memory-and-less-copying-in-stream-parsing.md)
- [023. Splicing the text of a Document in place](023-splicing-the-text-of-a-document-in-place.md)
- [024. The plain call path in the iterative VM](024-the-plain-call-path-in-the-iterative-vm.md)
- [025. Discarding separators in the example grammars](025-discarding-separators-in-the-example-grammars.md)
## Records 26–50
- [026. Lambda calls without allocation in the closure backend](026-lambda-calls-without-allocation-in-the-closure-backend.md)
- [027. Struct and call operands on the expression stack in the closure backend](027-struct-and-call-operands-on-the-expression-stack-in-the-closure-backend.md)
- [028. Field lists and list built-ins without allocation in the generated parsers](028-field-lists-and-list-built-ins-without-allocation-in-the-generated-parsers.md)
- [029. Shifting reused subtrees into the parser's chunks](029-shifting-reused-subtrees-into-the-parsers-chunks.md)
- [030. Building the code-point offset table on demand](030-building-the-code-point-offset-table-on-demand.md)
- [031. Specialized character-class tests in the closure backend](031-specialized-character-class-tests-in-the-closure-backend.md)
- [032. Matching literals against loaded input in one loop](032-matching-literals-against-loaded-input-in-one-loop.md)
- [033. Smaller VM entries](033-smaller-vm-entries.md)
- [034. One frame per plain call in the iterative VM](034-one-frame-per-plain-call-in-the-iterative-vm.md)
- [035. Left-recursion growth without allocation](035-left-recursion-growth-without-allocation.md)
- [036. Comparing texts without boxing them](036-comparing-texts-without-boxing-them.md)
- [037. No intermediate lists in `concat`](037-no-intermediate-lists-in-concat.md)
- [038. Calling plain rules directly from closure call sites](038-calling-plain-rules-directly-from-closure-call-sites.md)
- [039. A line table for error positions](039-a-line-table-for-error-positions.md)
- [040. One pass to prepare code-point input in generated parsers](040-one-pass-to-prepare-code-point-input-in-generated-parsers.md)
- [041. Building the offset table while decoding, for full parses](041-building-the-offset-table-while-decoding-for-full-parses.md)
- [042. Generated calls of plain rules](042-generated-calls-of-plain-rules.md)
- [043. Direct calls of plain rules in the VMs](043-direct-calls-of-plain-rules-in-the-vms.md)
- [044. Changes 36 and 37 in the VMs (instruction set 2)](044-changes-36-and-37-in-the-vms-instruction-set-2.md)
- [045. Moving reused trees in place after document edits](045-moving-reused-trees-in-place-after-document-edits.md)
- [046. Direct calls of plain rules in generated parsers](046-direct-calls-of-plain-rules-in-generated-parsers.md)
- [047. Recognition in generated parsers](047-recognition-in-generated-parsers.md)
- [048. A typed runtime for ParseAST](048-a-typed-runtime-for-parseast.md)
- [049. First-character dispatch in generated choices](049-first-character-dispatch-in-generated-choices.md)
- [050. First-character dispatch in the closure backend](050-first-character-dispatch-in-the-closure-backend.md)
## Records 51–98
- [051. First-character dispatch in the VMs (instruction set 3)](051-first-character-dispatch-in-the-vms-instruction-set-3.md)
- [052. Projected repetitions in generated `Parse`](052-projected-repetitions-in-generated-parse.md)
- [053. Projected repetitions in the engine and the VMs](053-projected-repetitions-in-the-engine-and-the-vms.md)
- [054. Resuming long repetitions in a `Document`](054-resuming-long-repetitions-in-a-document.md)
- [055. Tracing hook on the call path](055-tracing-hook-on-the-call-path.md)
- [056. Applying a `Document` edit to memo entries when they are looked up](056-applying-a-document-edit-to-memo-entries-when-they-are-looked-up.md)
- [057. Pooling the scratch memory of whole-input parses](057-pooling-the-scratch-memory-of-whole-input-parses.md)
- [058. Pooling the scratch memory of generated `Parse` and `Recognize`](058-pooling-the-scratch-memory-of-generated-parse-and-recognize.md)
- [059. Resuming long repetitions in the VMs](059-resuming-long-repetitions-in-the-vms.md)
- [060. Direct rules in the typed runtime](060-direct-rules-in-the-typed-runtime.md)
- [061. Reading code points directly in direct rules](061-reading-code-points-directly-in-direct-rules.md)
- [062. Comparing short literals in place in direct rules](062-comparing-short-literals-in-place-in-direct-rules.md)
- [063. A smaller `Node`](063-a-smaller-node.md)
- [064. A smaller `Node` in generated parsers](064-a-smaller-node-in-generated-parsers.md)
- [065. Position conversion in the language server](065-position-conversion-in-the-language-server.md)
- [066. No memo key allocated for variables where none is defined](066-no-memo-key-allocated-for-variables-where-none-is-defined.md)
- [067. Retire memo entries when their lookup slots are removed](067-retire-memo-entries-when-their-lookup-slots-are-removed.md)
- [068. Release construction tracking when an action returns nil](068-release-construction-tracking-when-an-action-returns-nil.md)
- [069. Bound persistent variable binding histories](069-bound-persistent-variable-binding-histories.md)
- [070. Keep repetition records across unchanged Document parses](070-keep-repetition-records-across-unchanged-document-parses.md)
- [071. Dependency propagation for sample constructor analysis](071-dependency-propagation-for-sample-constructor-analysis.md)
- [072. Reuse structurally equal duplicate capture types](072-reuse-structurally-equal-duplicate-capture-types.md)
- [073. Dependency-component propagation of variable reads](073-dependency-component-propagation-of-variable-reads.md)
- [074. Smaller first chunks for generated typed values](074-smaller-first-chunks-for-generated-typed-values.md)
- [075. Exclude absent expectations before scanning](075-exclude-absent-expectations-before-scanning.md)
- [076. Inline value-free plain generated Go rules](076-inline-value-free-plain-generated-go-rules.md)
- [077. Inline value-building and memoized Node expressions in generated Go parsers](077-inline-node-expressions-in-generated-go-parsers.md)
- [078. Local cuts in typed direct rules](078-local-cuts-in-typed-direct-rules.md)
- [079. Inline eligible typed left-recursion bodies](079-inline-eligible-typed-left-recursion-bodies.md)
- [080. Inline eligible typed Operators Rule line matchers](080-inline-eligible-typed-operators-rule-lines.md)
- [081. Scoped cuts in unfinished typed Go bodies](081-scoped-cuts-in-unfinished-typed-go-bodies.md)
- [082. Match typed direct bodies to general frame layouts](082-match-typed-direct-frame-layouts.md)
- [083. Inline eligible typed recovery bodies](083-inline-typed-recovery-bodies.md)
- [084. Complete typed Go-local capture layouts](084-complete-typed-local-capture-layouts.md)
- [085. Reuse stream capture frames at element commit](085-reuse-stream-capture-frames.md)
- [086. Reuse the Document VM value stack](086-reuse-document-vm-values.md)
- [087. Store deferred-memo seen positions sparsely](087-sparse-deferred-memo-seen-pages.md)
- [088. Share bounded stream text snapshots](088-share-bounded-stream-text-snapshots.md)
- [089. Isolate stream prefixes and batch scratch cleanup](089-isolate-stream-prefixes-and-batch-scratch-cleanup.md)
- [090. Share identical generated helpers and literal tables](090-share-identical-generated-helpers.md)
- [091. Specialize pure literal-text disjunctions](091-specialize-pure-literal-text-disjunctions.md)
- [092. Reuse generated Node capture scratch](092-reuse-generated-node-capture-scratch.md)
- [093. Chunked input storage for Document edits](093-chunked-document-input.md)
- [094. Generation-only typed-output liveness cleanup](094-generated-typed-output-liveness.md)
- [095. Copy-on-change attribute normalization](095-attribute-copy-on-change.md)
- [096. Specialize generated Go work-control methods](096-specialize-generated-go-method-helpers.md)
- [097. Exact Document Snapshot record sharing](097-exact-document-snapshot-record-sharing.md)
- [098. Bootstrap generated typed output allocations](098-bootstrap-generated-typed-output-allocations.md)

## Records 99 Onward

- [099. Share Document snapshot child runs](099-share-document-snapshot-child-runs.md)
- [100. Relative Snapshot Position Overlays](100-relative-snapshot-position-overlays.md)
- [101. Skip positional items for action-free rules](101-skip-positional-items-for-action-free-rules.md)
- [102. Read text struct fields without a struct lookup](102-read-text-struct-fields-without-a-struct-lookup.md)

## Experiments That Did Not Pay Off

| Experiment | Result | Decision |
|:--|:--|:--|
| Delay the variable-name hint until a second name occurs in the current environment | A snapshot with a different single name can be saved before the hint exists; restoring it later produces a false negative and retains duplicate bindings | Rejected for correctness. Record names from the first assignment, including branches that are later abandoned |
| Go arenas (`GOEXPERIMENT=arenas`) for per-parse scratch memory (memo entries, memo slots, expectation chunks), freed at the end of `Parse` | No measurable change in time or allocation: memo entries were already slab-allocated, and the tree must stay on the heap because it is returned | Not adopted. Requires an experimental build flag for no gain |
| Option to skip recording expected sets (report only the failure position) | After change 4: about −10% time on minilang, no change on JSON and CSV | Not adopted. Not worth an API knob; the full information is cheap enough |
| ASCII bitmap for character classes in the closure engine (instead of scanning the ranges) | No measurable change when measured against the unmodified code in alternating runs (JSON, CSV); classes in practice have one to three ranges, which scan as fast as a bitmap lookup | Not adopted then; change 31 specializes small classes instead, which does pay |
| Freeing capture frames when their rule invocation returns (LIFO arenas for frames and their slots, marked in `invokeBegin` and freed in `invokeEnd`) | −16 to −23% bytes per parse, but 1–6% slower on every workload and backend (recognition included), even with a fast path that skips freeing when nothing was allocated: the per-call bookkeeping and the clearing of freed slots cost more than the garbage collector saved | Not adopted |
| Storing all-ASCII input in code points as bytes (positions are the same in both units), with `len` of strings still counting code points | −9% bytes per parse on ASCII versions of the JSON, CSV and XML inputs (no rune array or offset table), but 0–4% slower: matching bytes checks for multi-byte characters at every step, which indexing code points does not | Not adopted |
| Iterative VM: holding body frames by value in the VM stack (a tagged entry instead of a pooled frame behind an interface) | 5–19% slower: each entry is about 150 bytes, and copying and clearing it on every push and pop costs more than the interface call and the pool it saves | Not adopted |
| Iterative VM: calling body frames directly (a type assertion before the interface call) and returning them to the pool without the type switch | Within ±4% (noise) | Not adopted |
| Estimating a smaller `Node` (120 → about 88 bytes with `int32` positions and interned type and rule names) by the opposite change: 32 bytes of padding | Closure: JSON +1%, XML +4%, Arith_OperatorsRule +1%, Minilang and the long `Document` within noise, about +12% bytes; so shrinking would gain a few percent at most | Adopted later as change 63, accepting the API change for the memory it saves |
| Inlining small plain rules into direct typed rules (after change 62; a body without calls of up to 16 expressions, also tried transitively) | `ParseAST` JSON −1% to −7% depending on the run, the other benchmarks within ±3% | Not adopted: no consistent gain for more generated code |
| Setting the fields of the reused action context in place in direct typed rules, instead of copying a whole `tctx` | `ParseAST` JSON −5%, Outline +5%, the others within ±2% | Not adopted (noise) |
| Memoizing every rule (classic packrat) | 2–3× slower than the transient policy on all workloads; memo entries were never reused for leaf and single-reference rules | Replaced by the transient policy (change 1) |

## Grammar Authoring Guidelines for Performance

- Inside a captured expression, discard parts the action does not need with `-x` (typically whitespace and
  punctuation): `rest:(-ws -"," -ws v:value)*`. The engine cannot drop them automatically, because a captured value
  exposes its full CST.
- A predicate that reads a capture makes the capture's rules build values even in recognition mode, and with them
  every rule they call: in `parsers/cue`, one predicate reading a capture that contained an expression made nearly
  the whole grammar build values in `Recognize`. Keep captures that predicates read small (a name, a token).
- Prefer named node types (`rule name Name = (...) #atomic` with `type Name = node[string]`) or `(...) #atomic` for
  tokens: their bodies are matched without building values.
- Write actions that use captures rather than `$n` where possible; a body whose action does not use `$n` is matched
  without building its CST.
