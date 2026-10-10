# Optimization Catalog

This catalog contains PEGO's numbered optimization history. Each entry preserves its original change number, implementation details, measurements, tradeoffs, and reproduction commands. The [performance document](../performance.md) keeps the current benchmark analysis and measurement method.

## Where each optimization applies

The runtimes are separate code: the closure backend (`compile.go`, `runtime.go`), the recursive and iterative bytecode
VMs (`vm.go`, `ivm.go`), the generated parsers' runtime (`genrt/runtime.go`, behind `Parse` and `Recognize`) and the
typed runtime of generated parsers (`genrt/typed.go`, behind `ParseAST`). An optimization made in one of them is not
automatically in the others. This table records, for every change in the log below, where it is in effect.

✓ applied · Partial applied to the subset in the note · ✗ not applied (see the note) · – not applicable (the backend has no such code path or feature)

| # | Optimization | Closure | VM | Iter. VM | Generated | Typed | Notes |
|--:|:--|:-:|:-:|:-:|:-:|:-:|:--|
| [1](001-value-free-rule-bodies-value-free-twins-transient-rules-0b2c903.md#1-value-free-rule-bodies-value-free-twins-transient-rules-0b2c903)| Value-free bodies, twins, transient rules | ✓ | ✓ | ✓ | ✓ | ✓ | generated since 11 |
| [2](002-frame-pooling-in-the-iterative-vm-8fab402.md#2-frame-pooling-in-the-iterative-vm-8fab402)| Pooled frames of the iterative VM | – | – | ✓ | – | – | |
| [3](003-node-fields-as-a-slice-54a5774.md#3-node-fields-as-a-slice-54a5774)| Node fields as a slice | ✓ | ✓ | ✓ | ✓ | – | the typed runtime builds no nodes for its results |
| [4](004-expected-sets-as-interned-ids-on-a-shared-stack-11a6ce3.md#4-expected-sets-as-interned-ids-on-a-shared-stack-11a6ce3)| Expectations as interned IDs | ✓ | ✓ | ✓ | ✓ | ✓ | generated since 11 |
| [5](005-memo-table-with-per-position-chains-cf77e4f.md#5-memo-table-with-per-position-chains-cf77e4f)| Memo table with per-position chains | ✓ | ✓ | ✓ | ✓ | ✓ | |
| [6](006-slab-allocation-for-nodes-child-lists-and-frames-5d03bf2.md#6-slab-allocation-for-nodes-child-lists-and-frames-5d03bf2)| Slab allocation (nodes, lists, frames) | ✓ | ✓ | ✓ | ✓ | ✓ | typed: pooled arenas (48) |
| [7](007-recognition-mode-3f7e9bf.md#7-recognition-mode-3f7e9bf)| Recognition mode | ✓ | ✓ | ✓ | ✓ | – | generated: `-recognize` (47) |
| [8](008-fast-path-for-unmemoized-calls-scan-nesting-limit-d2db87d.md#8-fast-path-for-unmemoized-calls-scan-nesting-limit-d2db87d)| Unmemoized fast path, scan loops, nesting limit | ✓ | ✓ | ✓ | ✓ | ✓ | VMs: `SCAN` |
| [9](009-value-free-pratt-lines-discarding-trivia-in-example-grammars-936857c.md#9-value-free-pratt-lines-discarding-trivia-in-example-grammars-936857c)| Value-free Pratt lines | ✓ | ✓ | ✓ | ✓ | ✓ | |
| [10](010-zero-copy-token-text-reusable-evaluation-context.md#10-zero-copy-token-text-reusable-evaluation-context)| Zero-copy token text, reused evaluation context | ✓ | ✓ | ✓ | ✓ | ✓ | VMs: text only (their evaluator has its own stack, 14) |
| [12](012-memoizing-rules-that-read-variables.md#12-memoizing-rules-that-read-variables)| Memoizing rules that read variables | ✓ | ✓ | ✓ | ✓ | ✓ | |
| [13](013-allocation-free-pratt-attempts-and-pooled-pratt-frames-in-the-engine-and-vms.md#13-allocation-free-pratt-attempts-and-pooled-pratt-frames-in-the-engine-and-vms)| Pratt attempts by value | ✓ | ✓ | ✓ | ✓ | ✓ | iterative VM: pooled Pratt frames |
| [14](014-one-operand-stack-for-vm-expression-code.md#14-one-operand-stack-for-vm-expression-code), [27](027-struct-and-call-operands-on-the-expression-stack-in-the-closure-backend.md#27-struct-and-call-operands-on-the-expression-stack-in-the-closure-backend)| Operands on one expression stack | ✓ | ✓ | ✓ | – | – | generated code evaluates actions as Go expressions |
| [15](015-allocation-free-action-plumbing-in-all-backends.md#15-allocation-free-action-plumbing-in-all-backends), [28](028-field-lists-and-list-built-ins-without-allocation-in-the-generated-parsers.md#28-field-lists-and-list-built-ins-without-allocation-in-the-generated-parsers)| Allocation-free action plumbing (field chunks, list built-ins on a stack) | ✓ | ✓ | ✓ | ✓ | ✓ | |
| [16](016-start-positions-and-lambdas-from-chunks-in-the-vms.md#16-start-positions-and-lambdas-from-chunks-in-the-vms)| Start positions and lambdas from chunks | – | ✓ | ✓ | – | – | |
| [17](017-ascii-bitmaps-for-character-classes-in-the-vms.md#17-ascii-bitmaps-for-character-classes-in-the-vms), [31](031-specialized-character-class-tests-in-the-closure-backend.md#31-specialized-character-class-tests-in-the-closure-backend)| Fast character-class tests | ✓ | ✓ | ✓ | ✓ | ✓ | closure: specialized tests (31); VMs: ASCII bitmaps (17); generated: inline comparisons (11) |
| [18](018-memoizing-from-the-second-call-at-a-position.md#18-memoizing-from-the-second-call-at-a-position)| Memoizing from the second call at a position | ✓ | ✓ | ✓ | ✓ | ✓ | not for `Document` and streams, which keep every entry; generated since 21 |
| [19](019-a-plain-call-path-for-unmemoized-rules-without-captures.md#19-a-plain-call-path-for-unmemoized-rules-without-captures)| Plain call path (`invokePlain`) | ✓ | ✓ | ✓ | ✓ | ✓ | iterative VM since 24 (`plainCallFrame`, 34) |
| [20](020-splicing-the-memo-table-on-document-edits.md#20-splicing-the-memo-table-on-document-edits), [23](023-splicing-the-text-of-a-document-in-place.md#23-splicing-the-text-of-a-document-in-place), [30](030-building-the-code-point-offset-table-on-demand.md#30-building-the-code-point-offset-table-on-demand), [45](045-moving-reused-trees-in-place-after-document-edits.md#45-moving-reused-trees-in-place-after-document-edits), [56](056-applying-a-document-edit-to-memo-entries-when-they-are-looked-up.md#56-applying-a-document-edit-to-memo-entries-when-they-are-looked-up)| `Document` edits: memo and text spliced, offsets on demand, trees moved in place, memo entries brought up to date lazily | ✓ | ✓ | ✓ | – | – | generated parsers have no `Document` |
| [22](022-bounded-memory-and-less-copying-in-stream-parsing.md#22-bounded-memory-and-less-copying-in-stream-parsing)| Bounded memory in streams | ✓ | ✓ | ✓ | – | – | generated parsers have no streams |
| [26](026-lambda-calls-without-allocation-in-the-closure-backend.md#26-lambda-calls-without-allocation-in-the-closure-backend)| Lambda calls without allocation | ✓ | ✓ | ✓ | – | – | VMs: lambdas from chunks (16); generated lambdas are Go function literals |
| [32](032-matching-literals-against-loaded-input-in-one-loop.md#32-matching-literals-against-loaded-input-in-one-loop)| Literals compared in one loop | ✓ | ✓ | ✓ | ✓ | ✓ | generated: its own loop |
| [33](033-smaller-vm-entries.md#33-smaller-vm-entries)| Smaller VM entries | – | ✓ | ✓ | – | – | |
| [35](035-left-recursion-growth-without-allocation.md#35-left-recursion-growth-without-allocation)| Left-recursion growth without allocation | ✓ | ✓ | ✓ | ✓ | ✓ | |
| [36](036-comparing-texts-without-boxing-them.md#36-comparing-texts-without-boxing-them), [37](037-no-intermediate-lists-in-concat.md#37-no-intermediate-lists-in-concat)| Unboxed text comparison, no intermediate lists in `concat` | ✓ | ✓ | ✓ | ✓ | ✓ | VMs since 44 (instruction set 2) |
| [38](038-calling-plain-rules-directly-from-closure-call-sites.md#38-calling-plain-rules-directly-from-closure-call-sites), [42](042-generated-calls-of-plain-rules.md#42-generated-calls-of-plain-rules), [43](043-direct-calls-of-plain-rules-in-the-vms.md#43-direct-calls-of-plain-rules-in-the-vms), [46](046-direct-calls-of-plain-rules-in-generated-parsers.md#46-direct-calls-of-plain-rules-in-generated-parsers)| Direct calls of plain rules | ✓ | ✓ | ✓ | ✓ | ✓ | generated: a method per rule (46); typed: a method per rule for every rule (48) |
| [39](039-a-line-table-for-error-positions.md#39-a-line-table-for-error-positions)| Line table for error positions | ✓ | ✓ | ✓ | ✓ | ✓ | streams still scan from the committed position |
| [40](040-one-pass-to-prepare-code-point-input-in-generated-parsers.md#40-one-pass-to-prepare-code-point-input-in-generated-parsers), [41](041-building-the-offset-table-while-decoding-for-full-parses.md#41-building-the-offset-table-while-decoding-for-full-parses)| Offset table built while decoding | ✓ | ✓ | ✓ | ✓ | ✓ | engine: full parses only; recognition builds it on demand (30) |
| [48](048-a-typed-runtime-for-parseast.md#48-a-typed-runtime-for-parseast)| Struct constructors per type, frames reused, scratch pooled across parses | ✗ | ✗ | ✗ | ✗ | ✓ | see below |
| [52](052-projected-repetitions-in-generated-parse.md#52-projected-repetitions-in-generated-parse), [53](053-projected-repetitions-in-the-engine-and-the-vms.md#53-projected-repetitions-in-the-engine-and-the-vms)| Projected repetitions (`map($rest, (r) => $r.f)`) | ✓ | ✓ | ✓ | ✓ | ✓ | VMs: `NEXT` mode 3 (instruction set 3) |
| [49](049-first-character-dispatch-in-generated-choices.md#49-first-character-dispatch-in-generated-choices), [50](050-first-character-dispatch-in-the-closure-backend.md#50-first-character-dispatch-in-the-closure-backend), [51](051-first-character-dispatch-in-the-vms-instruction-set-3.md#51-first-character-dispatch-in-the-vms-instruction-set-3) | First-character dispatch in choices | ✓ | ✓ | ✓ | ✓ | ✓ | VMs: `GUARD` (instruction set 3) |
| [54](054-resuming-long-repetitions-in-a-document.md#54-resuming-long-repetitions-in-a-document), [59](059-resuming-long-repetitions-in-the-vms.md#59-resuming-long-repetitions-in-the-vms)| `Document`: resuming long repetitions | ✓ | ✓ | ✓ | – | – | VMs since 59 (sites found in the bytecode); generated parsers have no `Document` |
| [55](055-tracing-hook-on-the-call-path.md#55-tracing-hook-on-the-call-path)| Tracing hook (cost only) | ✓ | ✓ | ✓ | – | – | generated parsers have no tracing |
| [57](057-pooling-the-scratch-memory-of-whole-input-parses.md#57-pooling-the-scratch-memory-of-whole-input-parses), [58](058-pooling-the-scratch-memory-of-generated-parse-and-recognize.md#58-pooling-the-scratch-memory-of-generated-parse-and-recognize)| Scratch memory pooled across whole-input parses (input, offsets, memo, value stack) | ✓ | ✓ | ✓ | ✓ | ✓ | typed: since 48; generated `Parse` and `Recognize`: 58 |
| [63](063-a-smaller-node.md#63-a-smaller-node), [64](064-a-smaller-node-in-generated-parsers.md#64-a-smaller-node-in-generated-parsers)| Smaller `Node` (88 bytes: `int32` positions, interned type and rule names) | ✓ | ✓ | ✓ | ✓ | – | generated `Parse` since 64; the typed runtime builds no nodes |
| [60](060-direct-rules-in-the-typed-runtime.md#60-direct-rules-in-the-typed-runtime)| Direct rules: a rule's body inlined into its call method, captures in Go variables, the action in place | – | – | – | Partial | ✓ | generated supports eligible value-free and Node bodies, with cuts still general; typed supports ordinary local cuts; `#recover`, Pratt and LR leaders remain general |
| [61](061-reading-code-points-directly-in-direct-rules.md#61-reading-code-points-directly-in-direct-rules)| Character tests read code points without calling `peek` | – | – | – | Partial | ✓ | direct rules only; generated supported Node and value-free rules since 76/77; Bytes retains decoding fallback |
| [66](066-no-memo-key-allocated-for-variables-where-none-is-defined.md#66-no-memo-key-allocated-for-variables-where-none-is-defined)| No memo key allocated for variables where none is defined | ✓ | ✓ | ✓ | ✓ | ✓ | |
| [67](067-retire-memo-entries-when-their-lookup-slots-are-removed.md#67-retire-memo-entries-when-their-lookup-slots-are-removed)| Retire/reuse memo entries on replacement, pruning and edit invalidation | ✓ | ✓ | ✓ | ✗ | ✗ | streams and Documents are engine-only; generated batch memo recycling has not been measured |
| [68](068-release-construction-tracking-when-an-action-returns-nil.md#68-release-construction-tracking-when-an-action-returns-nil)| Release action/predicate construction tracking, including nil results | ✓ | ✓ | ✓ | ✓ | ✓ | direct typed nested constructors included; TS also truncates on nil/errors; no generated streaming API |
| [69](069-bound-persistent-variable-binding-histories.md#69-bound-persistent-variable-binding-histories)| One current variable binding per name, persistent replacement and equal-value reuse | ✓ | ✓ | ✓ | ✓ | ✓ | TS also uses unique bindings; lookup scales with names, not assignment history; changed non-head values copy a prefix |
| [70](070-keep-repetition-records-across-unchanged-document-parses.md#70-keep-repetition-records-across-unchanged-document-parses)| Keep repetition records across unchanged Document parses | ✓ | ✓ | ✓ | – | – | retain the same-generation map, including root memo hits; one-edit eligibility and abort/reset invalidation remain |
| [71](071-dependency-propagation-for-sample-constructor-analysis.md#71-dependency-propagation-for-sample-constructor-analysis)| Dependency propagation for sample constructor analysis | ✓ | ✓ | ✓ | – | – | `sample.New` requires a grammar AST; shared constructor analysis, no matching-runtime change; contiguous reverse edges and bounded worklist |
| [72](072-reuse-structurally-equal-duplicate-capture-types.md#72-reuse-structurally-equal-duplicate-capture-types)| Reuse equal capture types without formatting | ✓ | ✓ | ✓ | ✓ | ✓ | shared type checking before compilation/generation; no matching-runtime change; top-level optional/union normalization retained |
| [73](073-dependency-component-propagation-of-variable-reads.md#73-dependency-component-propagation-of-variable-reads)| Propagate variable reads once per dependency component | ✓ | ✓ | ✓ | ✓ | ✓ | shared compiler metadata; no-read fast path; callee-first SCCs and immutable sorted peer keys; no matching-runtime change |
| [74](074-smaller-first-chunks-for-generated-typed-values.md#74-smaller-first-chunks-for-generated-typed-values)| Smaller first chunks with bounded growth for typed values | – | – | – | – | ✓ | generated `ParseAST`, both direct construction and Node conversion; fresh per-call owners, no returned-chunk reuse |
| [75](075-exclude-absent-expectations-before-scanning.md#75-exclude-absent-expectations-before-scanning)| Fixed-size absence filter for ordered expectations | ✓ | ✓ | ✓ | ✓ | ✓ | Go only; Node, recognition, typed direct/conversion; collisions retain exact scans, TypeScript unchanged |
| [76](076-inline-value-free-plain-generated-go-rules.md#76-inline-value-free-plain-generated-go-rules)| Inline value-free plain rule bodies | – | – | – | ✓ | – | Go recognition and Node skip twins; typed direct rules already use this emitter; see 77 for supported Node/value-building paths; TypeScript unchanged |
| [77](077-inline-node-expressions-in-generated-go-parsers.md#77-inline-value-building-and-memoized-node-expressions-in-generated-go-parsers)| Inline value-building and memoized Node expressions | – | – | – | Partial | – | Generated Go Node only; unsupported cut/recovery/Pratt/LR leaders and unresolved captures fall back; typed and TypeScript unchanged |
| [78](078-local-cuts-in-typed-direct-rules.md#78-local-cuts-in-typed-direct-rules)| Local cuts in typed direct rules | – | – | – | – | ✓ | ordinary cut-bearing typed Go rules; `#recover`, Pratt and LR leaders remain general |
| [62](062-comparing-short-literals-in-place-in-direct-rules.md#62-comparing-short-literals-in-place-in-direct-rules)| Short literals compared in place | – | – | – | Partial | ✓ | direct rules, up to 4 code points; generated value-free and supported Node rules since 76/77; CodePoints only, Bytes retains the literal matcher |

Not applied, and why:

- **Reusing frames when their rule returns (48):** in the Node runtimes, capture frames share their chunks with the
  child lists of the result, so they cannot be freed or pooled without separating them; freeing frames in the engine
  was measured slower (see the experiments table). The scratch memory that results do not share is pooled (57, 58).
- **General per-rule call methods for value-building generated `Parse`** (the typed runtime's `typedCall`) measured within noise (48).
- Experiments that did not pay off in some backends are in the experiments table at the end.

## Optimization history

## Changes 1–25
- [001. Value-free rule bodies, value-free twins, transient rules (0b2c903)](001-value-free-rule-bodies-value-free-twins-transient-rules-0b2c903.md)
- [002. Frame pooling in the iterative VM (8fab402)](002-frame-pooling-in-the-iterative-vm-8fab402.md)
- [003. Node fields as a slice (54a5774)](003-node-fields-as-a-slice-54a5774.md)
- [004. Expected sets as interned IDs on a shared stack (11a6ce3)](004-expected-sets-as-interned-ids-on-a-shared-stack-11a6ce3.md)
- [005. Memo table with per-position chains (cf77e4f)](005-memo-table-with-per-position-chains-cf77e4f.md)
- [006. Slab allocation for nodes, child lists and frames (5d03bf2)](006-slab-allocation-for-nodes-child-lists-and-frames-5d03bf2.md)
- [007. Recognition mode (3f7e9bf)](007-recognition-mode-3f7e9bf.md)
- [008. Fast path for unmemoized calls, `SCAN`, nesting limit (d2db87d)](008-fast-path-for-unmemoized-calls-scan-nesting-limit-d2db87d.md)
- [009. Value-free Pratt lines; discarding trivia in example grammars (936857c)](009-value-free-pratt-lines-discarding-trivia-in-example-grammars-936857c.md)
- [010. Zero-copy token text; reusable evaluation context](010-zero-copy-token-text-reusable-evaluation-context.md)
- [011. Generated parsers catch up with changes 4–10](011-generated-parsers-catch-up-with-changes-410.md)
- [012. Memoizing rules that read variables](012-memoizing-rules-that-read-variables.md)
- [013. Allocation-free Pratt attempts and pooled Pratt frames in the engine and VMs](013-allocation-free-pratt-attempts-and-pooled-pratt-frames-in-the-engine-and-vms.md)
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
## Changes 26–50
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
## Changes 51–78
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

## Experiments that did not pay off

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
| Estimating a smaller `Node` (120 → about 88 bytes with `int32` positions and interned type and rule names) by the opposite change: 32 bytes of padding | Closure: JSON +1%, XML +4%, Arith_Pratt +1%, Minilang and the long `Document` within noise, about +12% bytes; so shrinking would gain a few percent at most | Adopted later as change 63, accepting the API change for the memory it saves |
| Inlining small plain rules into direct typed rules (after change 62; a body without calls of up to 16 expressions, also tried transitively) | `ParseAST` JSON −1% to −7% depending on the run, the other benchmarks within ±3% | Not adopted: no consistent gain for more generated code |
| Setting the fields of the reused action context in place in direct typed rules, instead of copying a whole `tctx` | `ParseAST` JSON −5%, Outline +5%, the others within ±2% | Not adopted (noise) |
| Memoizing every rule (classic packrat) | 2–3× slower than the transient policy on all workloads; memo entries were never reused for leaf and single-reference rules | Replaced by the transient policy (change 1) |

## Grammar authoring guidelines for performance

- Inside a captured expression, discard parts the action does not need with `-x` (typically whitespace and
  punctuation): `rest:(-ws -"," -ws v:value)*`. The engine cannot drop them automatically, because a captured value
  exposes its full CST.
- A predicate that reads a capture makes the capture's rules build values even in recognition mode, and with them
  every rule they call: in `parsers/cue`, one predicate reading a capture that contained an expression made nearly
  the whole grammar build values in `Recognize`. Keep captures that predicates read small (a name, a token).
- Prefer terminal types (`type Name terminal`) or `@(...)` for tokens: their bodies are matched without building
  values.
- Write actions that use captures rather than `$n` where possible; a body whose action does not use `$n` is matched
  without building its CST.
