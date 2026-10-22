# 062. Backend-Independent Optimization IR

- **Status**: Accepted
- **Author**: @ornew
- **Date**: 2026-10-22

## Summary

Every backend of PEGO lowers the grammar AST (`grammar.Expr`) by itself, so every optimization is written
once per lowering and generated Go is repaired after it has been printed. This design puts two internal
representations between the checked grammar and the backends:

- **Core IR**: typed parsing expressions whose observable effects are explicit, built once from the
  grammar and the existing analyses. Optimizations are rewrites of Core IR with stated preconditions.
- **Flow IR**: one function per rule variant, with explicit state (position, value slots, saved state),
  structured blocks and exits, built from Core IR for the host-language emitters. State and code-shape
  optimizations happen here.

Each target declares the Core nodes it can execute, and the Core pass pipeline runs per target. Generated
Go and TypeScript are printed from Flow IR. Bytecode and the closure backend are built from Core IR: the
bytecode instruction set is a choice-point machine that matches Core IR, not Flow IR. Closures and
bytecode compile each program in two families, each built on first use: an inlined family for parses
without per-call events or Document memoization, and a call-preserving family for traced, profiled and
Document parses. Each existing lowering is removed in the stage that replaces it.

```text
                                                   ┌─ closure pipeline ──▶ closures (Closure backend)
grammar.Grammar ──check, analyze──▶ Core IR ───────┼─ bytecode pipeline ─▶ bytecode module (RVM, IVM, .pegoc)
                                                   └─ generated pipeline ▶ Flow IR ──▶ Go
                                                                                   └──▶ TypeScript
```

## Motivation

The evidence below was measured at commit `4e2f3763` with Go 1.27.1 on an Apple M3 Max (darwin/arm64).

### Six Lowerings of One AST

| Path | Lowering of parser expressions | Approximate lines |
|:--|:--|--:|
| Closure | `compiler.expr`/`exprMatcher` and helpers in `compile.go`, attributes in `attrs.go` | 720 |
| Bytecode, shared by the recursive and iterative VMs | `bcompiler.match`/`elems`/`leaf`/`void` and helpers in `bcompile.go` | 490 |
| Generated Go Node and recognizer | `generator.expr` and helpers in `gen.go`, one Go function per expression | 455 |
| Generated typed Go | `dgen` in `gen_direct.go` and `gen_direct_node.go`, falling back to `generator.expr` | 1,770 |
| Terminal programs | `terminalProgram` in `gen_terminal.go` | 260 |
| Generated TypeScript | `tsGen.expr` and helpers in `gen_ts.go` | 400 |

Line counts are the lengths of the functions named in each row. Actions and predicates are lowered four
more times: the closure evaluator in `eval.go`, bytecode expression code in `bcompile.go`, Go terms in
`gen.go` and `gen_types*.go`, and TypeScript terms in `gen_ts.go`. In the non-test files of
`internal/engine`, 123 function declarations in 23 files have a parameter of type `grammar.Expr` or
`[]grammar.Expr` (counted with `go/ast`). Each lowering re-derives what an expression means for values
(`build`, `isElems`, `valuedCount`), state (`mark`, `capSets`, `bindsVars`) and attributes (`#recover`,
`@error` and `@diagnostic` are interpreted from attribute lists in every lowering).

### Optimizations Written per Path

- Eight lowering decisions apply in every backend column of the
  [mechanism index](../optimizations/README.md#mechanism-index) (C, RVM, IVM, Go Node/Recognize, Typed
  Go, TS) and are implemented in each lowering behind those columns: value-free bodies (E010), value-free
  twins (E020), Operators Rule line value elision (E030), projected repetitions (E050), first-character
  dispatch (M010), scan loops (M040), plain rule calls (M050) and unboxed text comparisons (M120).
  First-character dispatch needed three records:
  [049](../optimizations/049-first-character-dispatch-in-generated-choices.md),
  [050](../optimizations/050-first-character-dispatch-in-the-closure-backend.md) and
  [051](../optimizations/051-first-character-dispatch-in-the-vms-instruction-set-3.md).
- The first terminal of an expression is defined by three recursive functions (`firstTerminal` in
  `compile.go` with a cancellable copy in `class_context.go`, `generator.first` in `gen.go`,
  `terminalProgram.first` in `gen_terminal.go`), which a change to guard eligibility must keep in step.
  Guards are emitted in six places.
- The typed Go path is a partial inliner whose subset grew by one mechanism at a time: G140 to G200 add cuts,
  left-recursion bodies, Operators Rule lines, framed cuts, frame layouts, recovery bodies and local
  layouts, each with a private `disable…` switch; code generation has 17 such switches.
- Work-control specialization exists three times: the closure backend compiles a second, metered closure
  graph (`controlledDispatch`); generated Go duplicates each function through `go/ast`
  (`specializeOrdinaryMethods`, `rewriteOrdinaryControl`, `foldOrdinaryControl`, `compactOrdinaryPeeks`,
  `stripNoControlWork`); TypeScript rewrites printed conditions with regular expressions (`tsWorkCond`).
- Code is shared as strings. TypeScript reuses the Go conditions of `generator.first` and
  `controlledClassReject` after rewriting `==` to `===` and rebalancing `&&`/`||` by parenthesis counting
  (`tsCond`, `tsBalancedCondition`). Helper sharing
  ([record 090](../optimizations/090-share-identical-generated-helpers.md)) compares printed function
  bodies.
- 36 function declarations whose names end in `Controlled` duplicate compile-time walkers only to poll
  compile cancellation.

### Generated Go Repaired After Printing

`dgen` writes Go text directly. Because Go rejects a `goto` that jumps over a variable declaration, it
declares every variable of a rule at the top of the function in a `var` block, typed `any` for values. It
tracks reads to avoid unused-variable errors, tracks reachability to satisfy `go vet`, and writes `_ = x`
where a declared variable ends up unread. Actions become immediately invoked function literals
(`func() any { c.P.StepWork(); return … }()`). The resulting text then passes through `go/format` and four
`go/ast` passes: ordinary-method specialization, charge stripping, typed-entry pruning (`gen_prune.go`) and
runtime-name qualification (`qualifyRuntime`). Removing the charges leaves `func() any { ; return … }()`
behind, and blank lines, which the specialization deletes by replacing `"\n\n"` in the printed text.

Counts in the checked-in parsers (`parsers/*/parser.go`):

| Parser | Bytes | `_ = x` lines | `{ ; return` | `func() any {` | `var (` blocks | Locals typed `any` |
|:--|--:|--:|--:|--:|--:|--:|
| `cel` | 136,982 | 9 | 32 | 137 | 61 | 274 |
| `python` | 785,557 | 89 | 86 | 2,370 | 363 | 1,969 |
| `typescript` | 1,183,066 | 130 | 118 | 1,904 | 528 | 2,641 |
| `duckdb` | 1,978,082 | 134 | 235 | 2,983 | 1,190 | 5,970 |

An excerpt of the start rule of `cel`, as generated today:

```go
func i0(p *pegort.TParser) (any, bool) {
	var (
		start int
		v     any
		c     *pegort.TActCtx
		ok    bool
		k1    any
		x2    int
		v3    any
	)
	start = p.Pos
	p.Enter()
	_ = x2
	// …
	v = func() any { return k1 }()
```

The post-passes also dominate generation time. `GenerateGo` with `WithTypes()` on the DuckDB grammar took
16.6 s per run (three runs); of the CPU samples under `generate`, `qualifyRuntime` had 51.6 %,
`specializeOrdinaryMethods` 24.5 %, `pruneTypedEntries` 8.6 %, `stripNoControlWork` 7.5 % and the final
`go/format.Source` 5.4 %; compilation and emission had less than 2 %. `qualifyRuntime` inserts each
qualifier by copying the rest of the source, which is quadratic and can be fixed independently of this
design. `pego gen -types -typed-only` on the same grammar took 4.0 s; with Node output as well it took 14.9
s and 412 MB of resident memory for a 6.9 MB `parser.go`.

## Goals

- One lowering of parser expressions and actions from the checked grammar: the Core IR builder. Each
  backend lowers from Core IR or Flow IR only.
- Every optimization is a pass with a precondition stated on Core IR or Flow IR attributes, a private
  same-source switch ([design 024](024-optimization-mechanism-controls.md)), and a negative-control test.
- Results, errors and expected sets, positions in both position units, recovery, memoization and left
  recursion behavior, and trace and profile events stay as the specification requires on every backend,
  as checked by the existing differential tests and the event-stream comparison in [Testing](#testing).
- Generated Go has no `go/ast` post-pass other than formatting, no `_ = x` placeholder, no function literal
  around an action that does not need one, and no local declared `any` where the checker gives a concrete
  type.
- No stage increases generated size, generation time, parse time, allocations or `Compile` cost beyond
  measurement noise; the first-use build of the call-preserving family and the size of saved programs
  are measured and reported separately. The measurement plan is in
  [Performance and Results](#performance-and-results).

## Non-Goals

- Runtime algorithms and data structures: memo tables, deferred memoization, left-recursion growth, the
  Operators Rule loop, Document splicing, stream buffers and slabs stay runtime services that the IR calls.
- Changes to the grammar language or the public API. The bytecode instruction set gains only the scope
  instructions of `Nest`; other new instructions need a design of their own (see [Decisions](#decisions)).
- A serialized or public IR. Core IR and Flow IR are internal to `internal/engine`.
- `#missing` support in backends other than Closure; Core IR represents it, and the other backends keep
  rejecting it.

## Design

### The Observable Contract

A rewrite is correct when the following are unchanged for every input and every option; everything else,
such as node identity and the number of times an action runs, is free
([Memoization](../../spec/parsing.md#memoization)).

| Observable | Where it lives in Core IR |
|:--|:--|
| Success, end position, value (up to node identity) | Node semantics, value attributes, rule finish |
| Farthest failure position, expected items, `@error` messages | Expectations carried by primitives; silent scopes (lookahead, Operators Rule skip); label and memo isolation scopes |
| Recovered errors and their order | Recover nodes; the recovered-errors part of saved state; rule-call scopes |
| Whether an action or conversion error, a nesting-limit error or a missing-token-limit error occurs, and which first | The raise effect, ordered by evaluation order |
| Positions in both units | Positions are opaque; primitives advance by characters, never by a unit count fixed at build time |
| Stream elements already delivered | The commit effect: a delivered element stays delivered. Which prefix is delivered before an interruption depends on work counts, which are implementation-specific |
| Engine trace and profile events: an enter and an exit event for every rule call, with `Failure`, `Recovered`, `Text` and `Examined`, and the `Profile` counts ([Engine Tracing](../../spec/observability.md#engine-tracing)) | `Call` nodes; in closures and the VMs these parses run the call-preserving family, which keeps every call (see [Rule Calls in Each Target](#rule-calls-in-each-target)) |
| Generated trace and profile events: the rule calls of the generated route and `TraceSkipped` ([Generated Go Batch Tracing](../../spec/observability.md#generated-go-batch-tracing)); in Documents, the known examined frontier ([Generated Go Document Tracing and Profiling](../../spec/observability.md#generated-go-document-tracing-and-profiling)) | `Call` nodes and the trace points of `Nest` in observable builds; guard peeks and `Nest` bodies in Document variants read input through recording primitives |
| Document results equal those of a parse from scratch; statistics are implementation-specific ([Reuse](../../spec/documents.md#reuse)) | Input reads happen only in primitives, which record what they examined |
| Work interruption | Fatal, never absorbed; counts are implementation-specific ([Cancellation and Work Limits](../../spec/parsing.md#cancellation-and-work-limits)) |

Some existing optimizations are not equivalences but specializations that the contract allows only in a
mode: recognition drops actions whose values predicates do not need, and with them their errors
([Modes](../../spec/parsing.md#modes)); a parse without work controls charges nothing; a parse that neither
reuses results nor reports examined input, such as a generated batch parse (which reports `ExaminedKnown`
false), needs no examined-range recording. Core IR keeps the two apart: **rewrites** preserve every
observable above in every mode, and **mode specializations** produce a rule variant for one mode and may
change only what that mode's contract leaves open. Generated parsers fix every mode when they are
generated; closures and bytecode select a variant when a parse starts, and engine tracing and Documents
select the call-preserving family (see [Rule Calls in Each Target](#rule-calls-in-each-target)).

### Core IR

A Core module holds the start rules, the interned expectation strings and character classes, the checked
types, the declared variables, and the rules. A rule holds its name, its value type, its capture scope
(slots with checked types), its finish (Value Action, capture attachment and named-node conversion, as in
[Rule Completion](../../spec/bytecode.md#rule-completion)), its Operators Rule table if it has one, and its call
policy: memoized, transient, plain, left-recursion leader, splice, the variables it reads, and its variant
key (value or value-free, recognition, work-control and Document modes, observability, and for closures
and bytecode the family). These are the
attributes that `rule` in `compile.go` holds today; Core IR makes them the input of every backend instead of
flags that each backend interprets.

Expression nodes:

| Node | Meaning | From |
|:--|:--|:--|
| `Lit(text)`, `Class(id)`, `Any`, `Anchor(kind)` | Match one primitive, recording its expectation on failure | `Literal`, `CharClass`, `Any`, anchors |
| `Seq`, `Choice`, `Opt`, `Repeat(min, max)` | PEG control; a `Choice` alternative may carry a `Guard` | `Seq`, `Group`, `Choice`, `Optional`, `Repeat` |
| `Look(neg)` | Lookahead; a silent scope | `And`, `Not` |
| `Cut`, `Succeed`, `Fail` | `#cut`, `_`, `_\|_` | `Cut`, `Top`, `Bottom` |
| `Call(rule, level, variant)` | Rule call through the runtime (memo, left recursion, depth, trace) | `Ref` |
| `Nest(rule, body)` | An inlined call with the scope of a rule call (below), without memoization | Inlining |
| `OperatorsRule(table)` | The runtime Operators Rule loop over Core line bodies | `OperatorsRule` |
| `Text(e)`, `Drop(e)`, `Capture(slot, e)` | `#atomic`, `-`, `name:` | |
| `Pred(term)`, `Bind(var, term)` | Predicate Action, variable binding | `Predicate` |
| `Label(e, message, mode)`, `Recover(e, skip)`, `Missing(lit)` | `@error`/`@diagnostic`, `#recover`, `#missing` | `Attributed`, `Recovery` |
| `Stream(repeat)` | The `#stream` repetition of a start rule | `Stream` |
| `Scan(class, min, max)`, `ClassRun(classes)`, `Terminal(program)` | Value-free terminal forms | Rewrites |

`Nest(rule, body)` is a scope node with every effect of a rule call except memoization
([Rule Calls](../../spec/bytecode.md#rule-calls), [Body Evaluation](../../spec/bytecode.md#body-evaluation)):

- it is a cut barrier: a cut in the body commits only choices inside it, as a cut never extends beyond its
  rule ([Cut](../../spec/parser-expressions.md#cut));
- it opens a variable scope, so bindings made in the body are not visible after it, on success or failure
  ([Variables](../../spec/predicates.md#variables));
- the body has its own capture slots, separate from the caller's;
- on failure it removes the errors recovered inside it, resets the position and, while `#missing` insertion
  is active, resets the missing-token state;
- it counts one nesting level, checked against the nesting limit on entry, and in observable builds it
  reports enter and exit events.

Terms (actions, predicate expressions, bindings) are a typed sublanguage of Core IR: names are resolved to
slots, items, lambda parameters and variables; built-ins are resolved; each term carries its checked type
and whether it can raise. Projected repetitions, `concat` fusion, literal-text dispatch and unboxed text
comparisons become term rewrites.

Every node carries attributes computed once and cached: **want** (whether its value is used), its value
shape (none, one value, or elements spliced into the enclosing tuple, which is what `isElems` and
`valuedCount` compute today), whether it is nullable, its first terminal, and an **effect summary**. Terms,
capture slots and rule values carry their checked types; the value types of nodes are not attributes, and
are added to them before Flow IR declares typed value slots.

| Effect | Meaning |
|:--|:--|
| consume | May advance the position |
| fail | May fail |
| expect | May record expectations (absent inside silent scopes) |
| capture(slots) | May write these capture slots |
| bind | May bind a variable |
| recover | May append recovered errors |
| cut | May set the cut flag of the enclosing choice, option or repetition; `Nest` and `Call` stop it |
| raise | May end the parse with an error: actions, conversions, nesting limit, missing-token limit |
| call | Calls rules or enters a `Nest` (depth, memo, trace) |
| commit | May deliver stream elements |

Expectations are not separate nodes: a primitive and the expectation it records on failure stay one node,
so no rewrite can separate them. Explicit scope nodes (`Look`, `Label`, `Recover`, `Nest`, the memo
isolation of `Call`) carry the rules for how recorded expectations combine.

Core IR invariants, checked by a verifier after every pass in tests:

- The bodies of `Look(neg)`, `Text` and `Drop` have want none and write no capture. The body of a positive
  `Look` has want value exactly where it captures: the captures inside build their values, as in every
  backend.
- `OperatorsRule` is the whole body of its rule; `Stream` occurs only at the top of a start rule.
- A `Guard` on alternative i holds only if the alternative's first terminal is the guard's class, reached
  through `Seq` heads, `Capture`, `Text`, `Drop`, `Nest` and calls of rules that are neither leaders nor
  Operators Rules, and the guard's depth is the number of such calls and `Nest` nodes, each counting one
  level.
- Every slot read by a term is in the scope of its rule, `Nest`, Operators Rule line or repetition
  element, and no `Cut` reaches outside the `Nest` or rule that contains it.
- A `Nest` of rule r never occurs inside the body of r or of a rule in the same call-graph cycle as r, nor
  inside a `Nest` of r or of such a rule.
- A node with want none builds no value; a node with want value has a value type.
- A target's pipeline produces only the node kinds that the target declares.

### Rewrites and Their Obligations

| Rewrite | Precondition and obligation |
|:--|:--|
| First-character guards | As the invariant above. The guard records the alternative's expectation at the current position and skips only if the depth after the calls on the way stays within the nesting limit, which is checked at run time; it is off while `#missing` insertion is active. Adjacent guards share one peek and may become a dispatch table. |
| Rule inlining (`Call` to `Nest`) | Only in pipelines that declare `Nest`: generated parsers and the inlined family of closures and bytecode. The callee is not a leader, has no Operators Rule, is never memoized in the variant's mode (plain, or transient outside Documents), and is not in a call-graph cycle with the caller; a size budget per function bounds the growth. `Nest` keeps the scope of a call (above), so cuts, variables, captures, recovered errors, nesting-limit errors and the events of observable builds are those of the call. In a Document variant, rules that Documents memoize are not inlined: inlining them is allowed by the contract but gives up reuse. |
| Value elision (want none) | Allowed only where building the value cannot raise: a conversion that the checker proves, such as a rule result converted to a named node type, has no raise effect, while an action may raise and keeps its evaluation. This covers value-free bodies, twins, Operators Rule lines and projections. The value-needed analysis decides want for the recognition specialization, which may also drop raising actions. |
| Terminal lowering | Value-free regions of literals, classes, sequences, choices, options and negative lookaheads become `Scan`, `ClassRun` or `Terminal`, which consume the same input and record the same expectations at the same positions ([record 111](../optimizations/111-terminal-programs.md)). The emitter chooses a table or straight-line code by size. |
| Case-pair class folding | A run of classes each holding one letter in both cases, such as `[kK] [eE] [yY]`, becomes a `ClassRun` with a case-folding fast path; on failure it records the class of the failing position, not a literal. |
| Depth-check merging | An outer and an inner `Nest` check merge into one check of both levels only if, from the outer entry, the inner `Nest` is reached on every path and nothing before it can fail, raise or commit, and, in observable builds, no event lies between them. Otherwise a parse that fails normally before the inner call would abort with a nesting-limit error, which is fatal ([Limits](../../spec/parsing.md#limits)). |

A rewrite is justified in a comment by these obligations and in tests by differential runs against the
pipeline without it (see [Testing](#testing)).

### Rule Calls in Each Target

A traced or profiled parse (`WithTrace`, `WithProfile`) reports an enter and an exit event for every rule
call, and a Document parse memoizes transient rules ([Engine Tracing](../../spec/observability.md#engine-tracing),
[Rule Calls](../../spec/bytecode.md#rule-calls)). Both are options of a parse. The closure backend and the
VMs share one program between these parses and all others and decide at every call whether to take the
plain path (`r.plain && !p.noPlain` in `compile.go`, `vm.go` and `ivm.go`; `setTrace` in `trace.go` and
every Document parse in `document.go` set `noPlain`). Recognition and, in the closure backend, work
controls already select separate programs, built on first use under a `sync.Once` of the shared
`Program` (`Program.recognizer` in `parse.go`, `prepareControlledProgram` in `work_setup.go`); a program
saved in recognize mode stores the recognizer's module.

Closures and bytecode therefore compile each program in two **families**:

| Family | Serves | Rule calls |
|:--|:--|:--|
| Inlined | Parses without tracing, profiling or a Document: batch parses, streams, recognition and work-controlled parses | Inlining produces `Nest`; a plain rule that is not inlined is called on the plain path, without the memo and trace checks of a call |
| Call-preserving | Traced and profiled parses, and Document parses | Every rule reference stays a full `Call` |

A parse selects its family from its options when it starts, so no rule call checks the mode, and
`noPlain` disappears. `plain` is derived for each family when its program is built or loaded and is not
stored in modules: false for every rule in the call-preserving family, and computed from the rule table
as today in the inlined family. `Compile` checks the grammar and builds Core IR; each family's program is
built on first use under its own `sync.Once`, as the recognizer is, because a `Program` is shared across
goroutines. A user that only parses Documents, such as the language server, never builds the inlined
family. Every compile error, such as `undefined rule`, which the closure lowering reports today
(`compiler.errorf` in `compile.go`), is reported by the checker or the Core IR builder inside `Compile`, so
building a family cannot fail on a grammar that compiled; an inconsistency found while building one is an
internal error, not a user error. A first-use build runs inside the parse that needs it, which checks its
context and work control before and after the build, outside the `sync.Once`, as
`prepareControlledProgram` does today. The build itself is not interrupted, so a cancelled parse never
leaves an interrupted build cached in the shared `Program`; its duration is part of the first-use cost
that [Performance and Results](#performance-and-results) measures. In the work-controlled program, entering a `Nest` charges at least one step, as a call does, so the
progress invariants of [Flow IR](#flow-ir) hold.

The family is independent of the other variants, which gives this matrix:

| Target | Variants | Count |
|:--|:--|--:|
| Closures | family × work control × recognition | up to 8 closure graphs, each built on first use |
| VMs | family × recognition (work controls do not select a separate program) | up to 4 modules |
| Saved programs | one mode per file: parse (inlined family), recognize (recognition in the inlined family) or document (call-preserving family) | 1 module per file |

[Performance and Results](#performance-and-results) measures the first-use cost of each family and the
size of saved programs.

Engine Documents memoize every rule that has a memo mark, transient or not (`memoAll` in `document.go`),
and the compiler marks every rule outside a call-graph cycle unless memoization is disabled
(`compile.go`). The only rules a Document parse does not memoize are non-leader rules on a cycle (and, while
`#missing` insertion is active, rules that can insert), and only a call from outside the cycle could
inline them. A third family, inlined for Documents, would add
`Compile` work, a saved mode and a variant to keep in step for that single case, so
Documents use the call-preserving family. Generated Document variants differ because their pipeline
knows the variant when it is generated; they inline only rules that their Documents never memoize.

Two other ways keep one program for all parses: keep the run-time choice and never inline in closures and
the VMs, which gives up inlining there; or compile a `Nest` that checks the mode and falls back to a full
call, which keeps two copies of every inlined body and a check per call.

Generated parsers fix observability and Documents when they are generated (`-observability`,
`-document`), so their pipeline knows the variant: in an observable build `Nest` emits the enter and exit
events of the call.

### Target Pipelines

Each target declares the node kinds its code and runtime support, and its pipeline runs only the passes
that produce them.

| Target | Declares | Passes |
|:--|:--|:--|
| Closures | Every node except `ClassRun` and `Terminal`; the call-preserving family also excludes `Nest` | Guards, scans, twins and value elision, projections, recognition; inlining in the inlined family |
| Bytecode | Nodes with an instruction: every node except `ClassRun`, `Terminal` and `Missing`; the call-preserving family also excludes `Nest` | The same passes; the call-preserving pipeline reproduces today's modules |
| Generated Go | Every node except `Missing` | All passes |
| Generated TypeScript | Every node except `Missing`, `ClassRun` and `Terminal`, which have no TypeScript runtime support | All passes that produce what it declares |

A pass enabled for a target later is measured on that target. Closure support for `ClassRun` and
`Terminal` is possible but unmeasured.

### Flow IR

Flow IR is built per rule variant from Core IR optimized by the generated pipeline, for the host-language
emitters. A function has:

- registers: the position, the saved state of each backtracking point (position, recovered-error count,
  environment, cut flag, capture values or trail length), and the silent depth;
- typed value slots: captures, intermediate values and the rule value, with Go and TypeScript types derived
  from the checked types;
- structured blocks with an exit label; `exit L` leaves block `L` (success or failure continuation), `loop`
  and `if` are the only other control; primitive operations (peek, literal and class tests, expect, call,
  build, evaluate term, charge work, trace) mirror runtime helpers.

Lowering threads a failure exit through each Core node: a `Choice` is a block per alternative whose failure
exit restores and falls to the next, a `Repeat` is a loop whose element failure restores and leaves the
loop, and a `Nest` is a block that saves and restores the caller's environment, cut flag and recovered
count, and has its own slots.

Flow IR passes:

- **Save/restore elision**: a backtracking point saves only what its body may change according to the Core
  effect summary (no environment without bind, no recovered count without recover, only the capture slots
  in capture). This generalizes what `dgen.mark` and
  [record 110](../optimizations/110-no-recovered-rollback-without-recovery.md) do case by case.
- **Slot allocation**: liveness over the block tree; slots whose live ranges do not overlap share a
  variable when their types are equal; slots never read are not declared.
- **Block merging and jump threading**: an exit to a block that only exits again goes to the outer label;
  empty blocks and unused labels disappear.
- **Charge placement**: charges coalesce within straight-line code, under the invariants that each loop
  iteration and each call charges at least one step, that input work is charged before the input is
  inspected, and that no charge moves across a commit. Without work controls, charges are not lowered at
  all.
- **Mode lowering**: the ordinary and the controlled variants of a function, batch and Document input
  access, and position-unit fast paths (such as reading decoded code points in place) are separate Flow IR
  functions selected by the caller, replacing the source-level specialization.

Flow IR invariants: every slot is declared in the innermost block that contains all its uses and is
assigned before it is read; an exit names an enclosing block; every loop iteration consumes input, exits,
or charges; in a Document variant every input read goes through a recording primitive.

### Emission

**Go.** A block becomes a Go block; `exit L` becomes `goto L` where the label is placed directly after the
block. A jump out of a block skips only declarations inside it, so slots use `:=` with concrete types.
Go 1.27.1 accepts this and rejects a `goto` that jumps over a declaration in the same block
(`goto fail jumps over declaration of n`), which is why today's code hoists every variable. Labels are
printed only when used. Pure terms print as Go expressions; a term that needs statements (a charge, a
loop) prints them before the expression instead of a function literal. Runtime names are qualified while
printing, and the source passes through `go/format` once. Values cross rule boundaries as `any`, because
`TRule.Body`, `TParser.Call` and the typed memo entries hold `any` (see [Decisions](#decisions)).

**TypeScript.** A block becomes a labeled block (`L: { … break L; }`), which TypeScript supports; loops
become `for (;;)`. Conditions are Flow IR expressions printed by the TypeScript printer, not rewritten Go
text.

**Bytecode.** Bytecode is emitted from Core IR optimized by the bytecode pipeline. The instruction set is a
choice-point machine: `CHOICE` saves the whole state (position, value-stack and entry-stack heights,
repetition states, capture trail, environment, recovered errors, frame) as one entry, and values live on a
stack ([VM State](../../spec/bytecode.md#vm-state), [Failure](../../spec/bytecode.md#failure)). Flow IR's
selective saves, registers and slots have no instructions to lower to, while every node that the bytecode
pipeline declares has one: `Choice` to `CHOICE`/`COMMIT`, `Guard` to `GUARD`, `Repeat` to
`REPEAT`/`ITER`/`NEXT`/`ENDREPEAT`, projections to `NEXT` mode 3, `Scan` to `SCAN`, `Look` to
`LOOK`/`ENDLOOK`, `Label` to `LABEL`, `Recover` to `RECOVER`/`ENDRECOVER`/`ENDSKIP`, `Call` to `CALL`,
and `Nest` to two scope instructions:

| Instruction | Operands | Semantics |
|:--|:--|:--|
| `NEST` | A rule | Begin an inlined call of rule A. Charge one work step; increment the call depth and end the parse with the nesting-limit error if it exceeds the limit; save the state and the start position; push a nest entry; create a new frame sized for rule A's scope. |
| `ENDNEST` | A rule, B keep | The body succeeded. If rule A's body is valued (not a value-free twin or value-free body; see [Rule Table](../../spec/bytecode.md#rule-table)), pop the body value; otherwise the body value is nil. Remove the nest entry, keep the nest's frame for completion, and restore the caller's frame and the capture trail as [Body Evaluation](../../spec/bytecode.md#body-evaluation) does; perform [Rule Completion](../../spec/bytecode.md#rule-completion) of rule A with the body value and the nest's frame from the saved start position; restore the environment, decrement the depth, and push the value if B is 1. |

On failure, a nest entry restores the state, which resets the position and removes the errors recovered
inside it, decrements the depth and continues failing. `CUT` stops at a nest entry as it stops at the base
of a rule body.

Load-time validation checks nest regions structurally, as it checks recovery regions; as for all code,
the loader does not verify stack use ([Load-Time Validation](../../spec/bytecode.md#load-time-validation)):

- every `NEST` begins a contiguous region that ends at a unique `ENDNEST` naming the same rule;
- every jump target and every target recorded by an instruction inside a region (`CHOICE`, `COMMIT`,
  `GUARD`, `LOOK`, `RECOVER`, `ENDRECOVER`, `ITER`, `NEXT`, `JUMP`) lies inside that region, and no
  instruction outside it targets an instruction strictly inside it, so a failure inside never leaves a
  nest entry behind;
- regions nest properly with each other and with recovery regions, and every repetition (`REPEAT` to
  `ENDREPEAT`), lookahead (`LOOK` to `ENDLOOK`) and label (`LABEL` to `ENDLABEL`) opened inside a region
  closes inside it;
- a region contains no `RETURN`, no `END` and no stream-mode `NEXT` (mode 2): a `#stream` repetition
  streams only in the start rule's body at call depth 1 ([Match Instructions](../../spec/bytecode.md#match-instructions)),
  and an inlined body never runs there, so the compiler emits mode 1;
- slot operands inside a region are in rule A's scope; rule A is not a leader, has no Operators Rule and
  is not memoized in the module's family; a call-preserving module contains no `NEST`.

The recursive and iterative VMs share `step` and the call stages
([Execution Models](../../spec/bytecode.md#execution-models)), so both get the instructions from one
implementation, and the recognizer's modules, which a recognize-mode file saves, are the bytecode of the
recognition specialization in each family.

**Closures.** The closure backend compiles Core IR optimized by the closure pipeline, in both families. It
stays the default backend for grammars compiled from source and the only `#missing` backend. Once it is built from
Core IR, the same compiler run on Core IR without optimizing passes is the reference for rewrites, so no
separate interpreter is maintained.

### Mapping of Existing Analyses

| Analysis today | In this design |
|:--|:--|
| Type checking, type resolution (`value_types.go`, `type_resolve.go`) | Runs on the grammar before Core IR; annotates nodes, slots and terms |
| Output schema (`output_schema.go`) | Unchanged; reads checker results |
| Call graph, nullability, left calls, leaders, SCCs (`analysis.go`) | Computed once by the builder; rule policy and node attributes |
| Transient, plain, memoized rules, variables read; the run-time `noPlain` choice | Rule call policy; the family of the program |
| First terminals (three implementations) | One Core attribute; `Guard` |
| Character classes, class tables, VM ASCII masks | Interned Core classes; each emitter chooses its representation |
| Value-needed rules, twins, lean bodies, projections (`project.go`) | Want and value elision; twin and recognition variants |
| Terminal programs (`gen_terminal.go`) | `Terminal` nodes; the emitter keeps the runtime table format |
| Recognizer program and recognize-mode modules | Recognition specialization of Core IR in each family; bytecode emitted from it |
| Helper sharing by printed body ([record 090](../optimizations/090-share-identical-generated-helpers.md)) | Structural sharing of identical Flow IR functions before printing |
| `pruneTypedEntries` | Reachability from start rules on Core IR |
| Compile-cancellation copies (`…Controlled`) | The pass driver polls the compile control between and within passes |

### Compatibility and Backend Support

The public API and the grammar language do not change. The bytecode instruction set gains `NEST` and
`ENDNEST`, and the instruction-set version of every module goes from 26 to 27. The compiled-file container
gets a new version. A saved file holds one module of one mode: a parse-mode file the inlined family's, a
document-mode file the call-preserving family's, and a recognize-mode file the recognition specialization
of the inlined family. Loaders reject earlier versions, which are compiled again; the format keeps no compatibility before the first
release. `spec/bytecode.md` specifies the two instructions, their failure handling and cut base, the
families and which parses use each, the derivation of `plain`, the file layout and the load-time checks.
Generated Go and TypeScript change in internal names and shape, not in API. Work counts may change, which
the specification allows.

### Implementation Sequence

All five stages precede the first release, in this order, so that a release can be cut after any
completed stage. Each stage ends by deleting the lowerings it replaces, including their term lowering.
During a stage, the new path sits behind a private generator or compile switch for comparison, and the
switch is removed when the default flips. From the first stage on, new optimizations are written only as
Core IR or Flow IR passes; the AST lowerings that remain receive fixes only. Each Core pass lands in the
stage of its first consumer, so its differential tests and negative controls run from the start.

1. **Core IR and typed Go.** The builder, verifier, printer and term sublanguage; Flow IR and the Go
   emitter for the typed rules of every `-types` output (typed-only, work-controlled, Document and
   observable), covering every construct the typed path supports, including those that fall back today
   (Operators Rule lines, left-recursion bodies, recovery, cuts); the Core and Flow IR passes that typed
   Go uses. The Node rules that such outputs keep and the recognizer's rules stay on their current
   lowering until stage 2. Gate: identical results on the generated-parser tests,
   `parsers/test.sh`, the typed conformance tests and the engine differential tests; identical generated
   trace and profile event streams, apart from differences the specification allows; the comparisons in
   [Performance and Results](#performance-and-results). Then remove `dgen`, the typed parts of
   `generator.expr` and of the Go term lowering, `gen_prune.go` and the typed uses of the ordinary-method
   passes.
2. **Go Node, recognizer, Document and stream output**, with the recognition specialization. The terms of
   the recognizer's rules are compared with the generator's from this stage, once the generator's
   projections for those rules match Core IR. Gate as stage 1 plus the Document and stream differential tests and the Node, recognition, Document and stream
   benchmarks. Then remove `generator.expr`, the rest of the Go term lowering and the remaining `go/ast`
   passes.
3. **TypeScript.** Gate: the TypeScript parity tests in `bench` and `internal/engine`, TypeScript event
   streams, and the TypeScript benchmarks (`bench/ts-*.mjs`). Then remove `tsGen.expr`, the TypeScript term
   lowering and the condition rewriting.
4. **Bytecode**, in both families, with `NEST`, `ENDNEST`, the new instruction-set and container versions
   and their specification. Gate: the call-preserving modules, main and recognition, have the module
   layout bytes of today's modules, excluding the instruction-set version field, for all repository
   grammars and the specification test data; the inlined family is verified by the
   differential tests and by event-free equality with the call-preserving family (equal results, errors,
   expected sets, recovered errors and positions on every parse without events) in both VMs; saved
   programs of each mode load and run in their family; the VM benchmarks, `Compile` cost and
   saved-file size. Then remove the match and expression-code lowering of `bcompile.go` and the VMs'
   `noPlain` checks.
5. **Closures**, in both families. Gate: unchanged results and engine event streams; event-free equality
   of the two families; `Compile` plus the first-use build of the inlined family within noise of today's
   `Compile` in time and allocations, and the first-use build of the call-preserving family measured. Then remove `exprMatcher`, its helpers, the term compilation of
   `eval.go` and `noPlain`. If `Compile` time
   regresses, the stage does not flip; the remedies to measure are building Core IR per rule on first use
   and skipping optimizing passes and closure construction for compilations that only report diagnostics,
   such as the language server's `CompileContext`.

New optimizations (wider inlining, case-pair folding, slot sharing across alternatives) come after the
stage that gives them a consumer and follow the same gates.

## Alternatives Considered

**Keep the per-backend lowerings and share more helpers.** No migration risk, but every optimization stays
one implementation per lowering, the string-level coupling between Go and TypeScript remains, and the
`go/ast` repairs and their generation cost remain.

**One IR level.** Emitting Go and TypeScript directly from Core IR keeps one representation but puts slot
allocation, save/restore elision and charge placement back into each emitter, which is how `dgen` grew.

**Three levels, as in GHC (Core, STG, Cmm).** STG exists to make lazy evaluation and closure allocation
explicit; a PEG parser has neither. The work between Core IR and host code is state and control, which
Flow IR covers. A third level would add a translation and a verifier without a pass of its own.

**One optimized Core IR for every target.** Simpler, but targets differ in what they can execute (the
VMs have no instruction for `ClassRun` or `Terminal`) and in when modes are fixed, and it would break the
layout-identical gate of the bytecode stage.

**Bytecode as the IR.** The bytecode is specified and validated, but it has no types and no structure:
host-language emission would have to recover blocks and value types from a flat instruction stream.

**Bytecode from Flow IR, with a register instruction set.** It would give the VMs selective saves and
slots, but it redesigns the instruction set, both VMs, the specification and load-time validation, and
it is unmeasured whether finer instructions repay their extra dispatches in an interpreter.

**An IR interpreter replacing the closure backend.** A single interpreter is less code, but the closure
backend resolves dispatch when it compiles, and the interpreter's speed is unmeasured. The closure
compiler over Core IR without optimizing passes already serves as the reference.

**A test-only Core IR evaluator from the first stage.** It would let Core passes be tested before any
backend consumes them, but it is one more executor to keep in step with the specification. Landing each
pass with its first consumer gives the same testing without it.

**Building Go with `go/ast` instead of text.** It removes formatting repairs, but not the per-path lowering,
the hoisted `any` variables or the missing analyses behind them.

## Testing

- **Verifier.** Core IR and Flow IR invariants are checked after every pass in tests.
- **Rewrite differential.** For each pass, its private switch runs the corpora with the pass on and off on
  every target that consumes it; results, errors, expected sets, recovered errors and positions in both
  units must match. Corpora: the repository grammars with their golden files, the specification test data,
  grammar fixture suites ([design 059](059-grammar-fixture-runner.md)), inputs from `sample/`, and mutated
  inputs for error paths.
- **Backend parity.** The existing differential tests between engine backends, generated Go, Documents,
  streams and TypeScript stay the acceptance tests of every stage.
- **Event streams.** For each stage, engine and generated trace event streams and profiles before and
  after the stage are compared on the corpora; a difference is accepted only where the specification
  allows it, such as calls skipped by first-character dispatch.
- **Families.** The corpora run in both families of closures and bytecode: the inlined family must equal
  the call-preserving family on every parse without events, and the call-preserving family keeps the event
  streams, profiles and Document statistics of the stage before.
- **Independent expectations.** One Core IR builder feeds every backend, so a builder error would no longer
  show as a disagreement between backends. Golden files and fixture expectations, which are not derived
  from any backend, guard against it, and the AST closure backend remains the reference until its stage.
- **Negative controls.** Each rewrite has a deliberately broken variant in tests that the differential
  tests must detect: a guard with a wrong expectation; a `Nest` without its cut barrier, which the grammar
  `rule a = b / "yq"` with `rule b = "y" #cut "z"` exposes on the input `yq`; a `Nest` that keeps the
  callee's bindings; a save that omits the environment; a merged depth check with a failing node between
  the two checks, run at the nesting limit; an elided value with a raising conversion; a `NEST` without
  its cut base; and a Document family that inlines a transient rule that Documents memoize, which the
  Document reuse statistics and the rule events of a traced Document parse must expose.
- **Work controls.** The charge invariants are tested directly: limits stop every looping and recursive
  grammar in the work tests, and interruptions stay fatal in every mode.

## Performance and Results

No implementation exists yet. The baseline for each stage is the commit before it, measured on the same
machine:

- **Generation:** wall time, CPU and allocations of `pego gen` for each repository parser with its
  `parsers/generate.go` options, plus DuckDB and Python with Node and typed output. The `qualifyRuntime`
  fix is measured on its own, so that its gain is not attributed to this design.
- **Generated code:** `parser.go` bytes, lines and functions; `go build` time and binary size of each
  generated package.
- **Parsing:** for stages 1 and 2, the typed, Node, recognition, Document and stream benchmarks in `bench`
  in both position units, with and without work controls; for stage 3, the TypeScript benchmarks; time and
  allocations with at least six samples compared by `benchstat`.
- **Closure and bytecode:** `Compile` time and allocations, which the closure backend pays at run time and
  the language server pays on edits; the time and allocations of building each family on its first use,
  which the first parse of each kind pays, and of the variants in the matrix of
  [Rule Calls in Each Target](#rule-calls-in-each-target); the size of saved programs in each mode; and the full-suite benchmarks of each backend in each family.

Each enabled pass records its measured effect in the [optimization catalog](../optimizations/README.md).

## Decisions

1. **Types at rule boundaries in typed Go.** Values cross rule calls as `any`, and bodies use concrete
   types. Typed entries and memo slots per rule, through generics or generated tables, change the runtime
   and are adopted only if measurements show that they cost less than the type assertions they save.
2. **Bytecode source.** Bytecode is emitted from Core IR with the current instruction set and the scope
   instructions of `Nest`. Flow IR with a
   register instruction set would redesign the instruction set, both VMs and load-time validation, and
   keeping the AST lowering would keep a parallel path (see [Emission](#emission)).
3. **Inlining in closures and the VMs.** Closures and the VMs inline at compile time in the inlined
   family and keep every call in the call-preserving family; each family is built on first use, a parse
   selects its family when it starts, and no rule call checks the mode. Documents use the
   call-preserving family, because they memoize nearly every rule
   ([Rule Calls in Each Target](#rule-calls-in-each-target)). The VMs execute `Nest` with `NEST` and
   `ENDNEST` ([Emission](#emission)). Instructions for `ClassRun` and `Terminal` are a measured follow-up
   with a design of their own if VM profiles show that terminal and class runs dominate.
4. **Saved programs.** A saved file holds one module of one mode: parse stores the inlined family,
   document the call-preserving family and recognize the recognition specialization, and the file serves
   only that mode's parses. The container and instruction-set
   versions change, and the format keeps no compatibility before the first release.
5. **Closure backend.** Closures are compiled from Core IR. An IR interpreter is unmeasured and would give
   up the dispatch that closures resolve when they compile; the closure compiler over unoptimized Core IR
   serves as the rewrite reference without new code. Closures compile the same two families as bytecode.
6. **Closures last.** The closure backend migrates in the last stage, so the AST closure backend remains a
   reference independent of the Core IR builder through the other stages, and the families and `NEST` are
   proven on bytecode, whose call-preserving modules have a layout-identical gate, before the default backend
   adopts them. The AST lowerings that remain receive fixes only.
7. **Stages before the first release.** All five stages precede the first release, so that no AST
   lowering ships and every backend has the Core passes. The stages are ordered so that a release can be
   cut after any completed stage.
8. **Work counts.** Charge placement may change the work counts of generated artifacts under the progress
   invariants of [Flow IR](#flow-ir), as the specification makes counts implementation-specific; tests that
   pin counts are updated.
9. **Overlap within a stage.** The old and the new path run side by side behind a private switch only
   within one stage, because comparison on the same source is the main correctness evidence; the switch is
   removed when the default flips.
10. **Terms in Core IR.** Actions and predicates are lowered in Core IR from the first stage, because the
   function literals and `any` locals in generated Go come from term lowering.

## Limitations

The following are predictions to be measured by the gates in
[Performance and Results](#performance-and-results), not results:

- the cost of building Core IR and running the passes, in generation and in `Compile`;
- the closure backend's `Compile` time and allocations when compiled from Core IR, and the first-use cost
  of each family;
- the parse-time gain of inlining in closures and the VMs, and the size of saved programs in each
  mode;
- the speed and size of generated Go and TypeScript printed from Flow IR, and the cost of `any` at rule
  boundaries.
