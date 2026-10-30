# Project Strategy

This page covers what PEGO aims to be, where it earns its first proof, what must become true on the way, and how
progress is measured. It is written for contributors who are choosing what to work on and judging the result; the
summary for readers is [Why PEGO](why-pego.md). Facts about PEGO refer to this repository. Benchmark figures come from
[Benchmark Results](benchmarks.md) (commit `13b3d186`, Go 1.27.1, Apple M3 Max, darwin/arm64, median of three runs,
positions in code points). Comparisons checked on 2026-10-29.

## Vision and Positioning

**Mission.** Make a grammar the only parser code a program needs, whatever language the program is written in and
whatever it parses for, as fast as the fastest tools for its platform.

**Identity.** PEGO aims to be one grammar for every target, engine and use: a grammar compiler and parsing platform
that turns one grammar into parsers for any target language, engine and optimization profile, and for every use
(batch trees, typed values, incremental documents, streaming, recognition and, as a goal, editors). The aim is the
best measured speed in each, against the fastest tools for each platform, with every loss published. The breadth is
in targets and uses, not in grammar class: grammars are PEG-based, with ordered choice (see the non-goals). Each part
of the aim is stated with its status today:

| Aim | Today | Goal |
|:--|:--|:--|
| One compilation path | The Core IR and Flow IR feed the Go and TypeScript generators; the engine backends compile the grammar by their own routes | Every backend on the Core IR ([design record 062](design/records/062-backend-independent-optimization-ir.md), accepted, not implemented) |
| Any target language | Two: generated Go and generated TypeScript | More target languages ([Targets and engines](#wedge-and-sequencing) track) |
| Any engine | Three engine backends in Go (closure, recursive VM, iterative VM); a bytecode with a published specification, implemented only by the Go VMs and unstable until it is declared stable and versioned, which the first tagged release does not do | A stable, versioned bytecode, then another implementation of it |
| Selectable optimization profiles | One fixed set of optimizations | Generation profiles for speed, memory and size ([design record 063](design/records/063-configurable-optimization-pass-pipeline.md), accepted, not implemented) |
| Every use | Batch trees, typed values, recognition, recovery, `Document` and streaming, with per-target gaps ([What One Grammar Gives You](why-pego.md#what-one-grammar-gives-you)); a language server for `.pego` sources | Editor-grade tooling for the languages users define ([Editor-Grade Parsing](#editor-grade-parsing)) |
| The best measured speed in each | Generated Go is slower than Go's hand-written standard-library parsers on every comparable workload (1.1–5.4× for typed values, 1.6–6.8× for trees); no cross-tool benchmark yet | The targets of [Performance Leadership](#performance-leadership), first for the targets and uses that exist, then for each new one |
| Safe by default | A nesting cap only | Work limits on by default, in generated parsers too; optional input-length and memory caps ([Predictable Cost and Safety](#predictable-cost-and-safety)) |

**Positioning.** The reason to choose PEGO is breadth under one contract. One PEG-based grammar yields typed values,
recognition, incremental documents and bounded streams, in every target that supports them. Every backend returns the
same results for the features it shares, and the tests check this. PEGO needs no cgo.

- **Today.** That breadth is the claim. PEGO has no editor ecosystem, no grammar library and no speed lead.
- **Ambition.** Speed, memory and size are a pillar. The goal is to match and then beat hand-written parsers in every
  target and use while returning typed values, and to show it in a public cross-tool benchmark
  ([Performance Leadership](#performance-leadership)).
- **Not claimed.** PEGO is not safe by default for untrusted input yet, and it is not editor-grade yet. Both are goals
  with stated gaps ([Predictable Cost and Safety](#predictable-cost-and-safety),
  [Editor-Grade Parsing](#editor-grade-parsing)).

**First proof.** The identity covers every target and use, but proof is earned in order. The first users to win are
Go teams that own a DSL, configuration, query or policy language and want typed values, declared error recovery and
incremental reparsing from one grammar, with no cgo ([Order of Proof](#order-of-proof)). This orders the work; it does
not narrow what PEGO is for.

**Non-goals.**

- Competing with tree-sitter's grammar corpus or editor integrations in the near term. PEGO grammars do not target
  tree-sitter query compatibility.
- General context-free parsing. PEGO has no ambiguous grammars, GLR or parse forests; ordered choice is the model.
- A linear-time promise for every grammar. The project states costs and bounds work instead (see
  [Predictable Cost and Safety](#predictable-cost-and-safety)).
- A semantic framework. Name resolution, scoping and type checking of the user's language stay in user code. The
  editor work stops at syntax-level server plumbing.
- Grammar-constrained decoding for language models (GBNF and similar). It checks token prefixes at sampling speed
  inside an inference loop, a different product with its own incumbents.
- Compatibility before the first release. Grammar syntax, the Go API, `.pegoc`, JSON forms and bytecode change freely
  until then.
- Deferred by policy: go-gettable ready-made parser modules and 32-bit Go. Further target languages and a second VM
  implementation are goals of the Targets and engines track, after the release.

## Market Map

| Segment | What they need | Incumbents | Switching cost | PEGO today |
|:--|:--|:--|:--|:--|
| Go teams that own a DSL, configuration, query or policy language | Typed values, good errors, bounded cost, plain `go build` | Hand-written parsers, goyacc, pigeon, pointlander/peg, participle, ANTLR Go runtime; embedding CEL (cel-go) or Expr where an expression language fits | Low to medium: a grammar rewrite per language | Partial fit: typed generated Go, declared recovery, a default depth cap, stdlib only. Work limits must be generated in and enabled (1.30–1.47× slower on generated Go); no memory or input-size cap; no grammar classifier |
| Go developer tools for a custom language (linters, formatters, servers) | Incremental trees that tolerate errors, in pure Go | tree-sitter through cgo, gotreesitter, gpeg, hand-written | Medium: tree-sitter grammars and queries are reusable assets | Partial: engine `Document` and declared recovery ship. A `Document` has no `#missing`, so an unclosed construct with no `#recover` path yields no tree. There is no language server for user grammars and no UTF-16 position map |
| The same parser in a Go backend and in TypeScript | Identical results on both sides | ANTLR (Go, JavaScript and TypeScript targets), tree-sitter (Go binding or gotreesitter, plus `web-tree-sitter`), two hand-written parsers | Medium | Partial: generated TypeScript has no `Document` or streaming, positions are not UTF-16, and nesting is limited by Node's stack |
| Stream and log processing of grammar-defined record formats | Bounded memory, throughput | Hand-written scanners, format-specific decoders | Low | Partial: memory is bounded; CSV streaming is 11.5× slower than `encoding/csv` |
| Editors needing highlighting for many languages | Grammar corpus, queries, editor embedding | tree-sitter, gotreesitter, Lezer, TextMate grammars | High: corpora, queries and editor integrations | Weak: no highlighting output, no corpus |
| TypeScript DSLs with full IDE support | Typed AST, scoping, LSP | Langium, Chevrotain | High: Langium also generates the semantic layer | Weak: TypeScript output has no `Document` and no language server |
| Compilers of large programming languages | Best diagnostics and speed for one language | Hand-written front ends (Roslyn, rust-analyzer, `go/parser`) | Very high | Weak: ten ready-made parsers prove expressiveness, but generated Go is slower than `go/parser` |

## Incumbents

Each entry gives the incumbent's moat, how PEGO differs, what PEGO can replace, where PEGO loses today, and the likely
counter-move.

### `tree-sitter` and `gotreesitter`

Checked against tree-sitter 0.27.1 and gotreesitter 0.56.0.

- **Moat.**
  - tree-sitter's list of parsers has about 510 entries (about 480 repositories; fetched 2026-10-29), with highlight,
    injection and tag queries. Editors load these grammars; Helix's `languages.toml` has 303 grammar entries.
    `web-tree-sitter` had about 7.9 million npm downloads in the week ending 2026-10-27.
  - The runtime returns a tree for every input and reports changed ranges. `tree-sitter fuzz` checks every grammar
    under random edits and undo.
  - gotreesitter runs 206 of these grammars in pure Go. It adds typed query code generation (`cmd/tsquery`), UTF-16
    coordinates and a WebAssembly runtime. Its deterministic work limits (iterations, stack depth, nodes) are on by
    default with thresholds derived from the input, and an edited incremental parse stops where a fresh parse would.
    A per-parse memory budget and a process-heap ceiling are also on by default; a timeout and a cancellation flag
    are available.
- **Difference.** PEGO is PEG-based and builds typed values from the grammar. tree-sitter is LR(1) with bounded GLR,
  and its nodes are dynamic. tree-sitter documents no worst-case bound; its GLR version count is capped by design, and
  an open report shows that the cap can drop a correct branch (issue 6057). PEGO specifies classes of growth. Both
  PEGO and gotreesitter count work deterministically, but gotreesitter's limits and memory budget are on by default,
  while PEGO's limits are opt-in today and it has no memory limit.
- **Can replace.** A tree-sitter grammar written only to parse one language a Go team owns, where typed values and
  generated TypeScript matter more than queries and an editor corpus.
- **Loses today.** It loses on the corpus, queries, highlighting and editor distribution. It also loses on a tree for
  every input with no recovery annotations, changed ranges, UTF-16 positions, and work limits and a memory budget on
  by default. All of these are available in pure Go through gotreesitter, so cgo alone is not an argument for PEGO.
  No speed comparison exists.
- **Counter-move.** gotreesitter's v1 design plan requires incremental results to equal fresh parses; meeting it would
  remove the remaining difference in the incremental contract. Other tree-sitter issues are examples, not a pattern:
  a cancellation-latency report is open (issue 6067), and a recovery-cost report was fixed and closed on 2026-10-03
  (issue 5967).

### ANTLR 4

Checked against ANTLR 4.13.2 and the Go runtime 4.13.1.

- **Moat.** The grammars-v4 library, ten runtime targets including Go, JavaScript and TypeScript, IDE plugins, books
  and long familiarity. Recovery is automatic and needs no grammar annotations (single-token insertion and deletion,
  then resynchronization). Direct left recursion with precedence by alternative order is supported.
- **Difference.** PEGO is incremental and streams, its value actions do not depend on the target, and it needs no Java
  toolchain. ALL(*) is O(n⁴) in theory (Parr, Harwell and Fisher, Theorem 6.3), and PEGO states no polynomial bound
  either. PEGO's difference is its stated cost model and its work limits, not a better bound.
- **Can replace.** ANTLR in Go services where the Java build step or the lack of incremental parsing hurts. Two user
  reports describe Go-target speed and memory problems (issues 1901 and 3279, from 2017 and 2021), but they predate
  the 4.13 Go runtime module and are not evidence about it.
- **Loses today.** Grammar library, target breadth, automatic recovery, maturity, tutorials.
- **Counter-move.** The tool is maintained without a release since August 2024 (commits continue). ANTLR 5, a single
  WebAssembly runtime, has had no commit since July 2024. antlr-ng ports the tool to TypeScript (1.0.10, June 2025).
  A revival would bring a portable runtime.

### Lezer

Checked against `@lezer/lr` 1.4.11.

- **Moat.** It is CodeMirror 6's parser system. `@lezer/lr` had about 18.5 million npm downloads in the week ending
  2026-10-27. Parsing is step-wise (`PartialParse.advance()` with `stopAt`), mixed-language parsing is built in, and
  trees are compact.
- **Difference.** Lezer trees are untyped by design, and the runtime is JavaScript only. PEGO's TypeScript output is
  typed but has no `Document`.
- **Can replace.** Little today. A CodeMirror integration would need TypeScript `Document`s and a tree adapter.
- **Loses today.** Everything editor-facing in the browser.
- **Counter-move.** None expected while PEGO does not compete inside CodeMirror. Development moved off GitHub in April
  2026; npm releases continue.

### Langium and Chevrotain

Checked against Langium 4.4.0 and Chevrotain 13.2.0.

- **Moat.** One grammar yields a typed TypeScript AST, scoping, validation and a full language server. Recovery inserts
  and deletes tokens automatically, and parsing can run in a worker with cancellation (opt-in, Node).
- **Difference.** Langium reparses changed documents in full and is TypeScript only. PEGO is Go-first, incremental
  within a document, and streams. PEGO has no semantic layer and no language server for user grammars.
- **Can replace.** Langium only where a Go backend must share the parser.
- **Loses today.** The whole IDE story.
- **Counter-move.** Tree-level incremental parsing in Chevrotain or Langium would remove PEGO's incremental difference
  in TypeScript. It was requested in Chevrotain issue 843, which is closed; no incremental API is documented.

### Go Parser Generators

Checked against goyacc in `golang.org/x/tools`, pigeon 1.3.0, pointlander/peg 1.0.1, participle 2.1.4 and gpeg at
commit `d601f967`.

- **Moat.** Familiarity and simplicity. goyacc is part of `golang.org/x/tools`. participle needs no generation step.
  pigeon is the established Go PEG generator. It supports left recursion, including indirect recursion, behind an
  experimental flag, and recovery through labeled failures.
- **Difference.**
  - pigeon returns `any` (a type-safety request has been open since 2018) and makes memoization opt-in.
  - participle has no left recursion and no generated parser (only an experimental generated lexer). Its error
    handling stops at a partial AST up to the error.
  - pointlander/peg documents none of recovery, incrementality or streaming.
  - gpeg is an incremental PEG engine in Go with a published evaluation (Yedidia and Chong, SLE 2021). Edit
    application is logarithmic. The paper reports reparses of about 1 ms for a Java highlighting grammar on 1–100 MB
    inputs (AMD Ryzen 5 1600, Go 1.16); this was not re-measured on the checked commit, whose VM was rewritten. gpeg
    runs any grammar incrementally and reuses results where patterns are marked for memoization. It builds untyped
    captures, has no left recursion, uses hand-written recovery patterns and has no depth or work limits. Its equality
    test against a fresh parse is disabled at the checked commit.
  - PEGO has typed output, left recursion that is not experimental, the Operators Rule, `#recover`, streaming, limits,
    and a fresh-parse equality contract that it states and tests.
- **Can replace.** pigeon and pointlander/peg grammars, which have similar PEG semantics. participle where recovery
  is needed. goyacc grammars for languages that suit ordered choice.
- **Loses today.** participle's zero-step setup. gpeg's edit cost on very large documents, since PEGO's edit
  application grows with document size, and its published scaling study. There is no measured speed comparison with
  any of them.
- **Counter-move.** gpeg's latest commits (2026-08-19) rewrote its VM; typed captures or limits there would make it
  the closest Go alternative. participle may add parser generation, which is requested in its open issue 213.

### Embedded Expression Languages

Checked against cel-go 0.30.0 and Expr 1.17.8.

- **Moat.** A team whose "language" is a filter, rule or policy expression can embed an existing language and write
  no grammar. CEL states that evaluation is linear in the expression and input sizes when macros are disabled. Expr
  states that its programs always terminate and are side-effect free. Both type-check expressions against the host's
  types.
- **Difference.** PEGO defines a syntax; it does not evaluate it. It fits when the syntax is the team's own (search
  syntax, configuration, a domain notation), or when an existing expression language does not fit the users.
- **Can replace.** Nothing, when CEL or Expr fits; this is the first incumbent to concede.
- **Counter-move.** None needed; they already cover their part of the segment.

### Hand-Written Parsers and the Go Standard Library

- **Moat.** Speed and control. The Go compiler left yacc in Go 1.6, and `go/parser`, `encoding/json` and
  `encoding/csv` are hand-written. On PEGO's own workloads they are faster than generated Go: 1.1–5.4× for typed
  values, 1.6–6.8× for trees, 8.05× for recognition and 11.5× for streaming (engine, not generated Go). The JSON
  baseline is `encoding/json` `Unmarshal` into `any`, as built by Go 1.27.1; a `jsontext` baseline is not measured. Language models now make a small hand-written
  recursive-descent parser cheap to produce.
- **Difference.** From a grammar, PEGO returns positions and rule names or typed values, recovers, reparses
  incrementally and limits work. A hand-written parser gets each of these by hand, and differential, fuzzed
  equality checks do not come with it.
- **Can replace.** New hand-written parsers for languages a team owns, where maintenance cost dominates speed.
- **Loses today.** Raw throughput on every comparable workload, and familiarity.
- **Counter-move.** None; this is the default that PEGO must beat, first on cost and then on speed (see
  [Performance Leadership](#performance-leadership)).

### Other PEG Tools

Checked against pest 2.9.5, cpp-peglib 1.30.0, CPython's PEG parser (PEP 617) and Ohm 17.5.0.

- **Moat.** Their host ecosystems (Rust, C++, CPython itself, JavaScript).
- **Difference.**
  - pest documents the most complete resource policy of the group: a call limit (opt-in), a stack guard that is on by
    default on native targets, and a statement that input-size and depth limits alone do not bound work. It has no
    left recursion, and its output is untyped.
  - cpp-peglib has left recursion, recovery and an opt-in depth limit, with `std::any` values.
  - CPython's parser shows that a packrat PEG with left recursion can serve a production language.
  - Ohm 17 ships the Dubroy and Warth incremental packrat design; the Ohm 18 beta removes it.
- **Can replace.** Not directly; their host languages differ. They are reference points for features and limits.
- **Counter-move.** A Go port of one of them, which is unlikely.

### LR Tools With Measured Recovery

Checked against grmtools 0.15.0 and Menhir 20260209.

- **Moat.** Quantified error recovery: CPCT+ repairs 98.37% of 200,000 invalid Java files within a 0.5 s timeout.
  Menhir adds per-state error messages with checked coverage.
- **Difference.** PEGO's recovery is declared per construct (`#recover`) and unmeasured.
- **Loses today.** Any claim about recovery quality.
- **Counter-move.** Not applicable; the lesson is to measure recovery the same way.

### Hand-Written Incremental Front Ends

Checked against the Roslyn and rust-analyzer design notes.

- **Moat.** A decade of tuning for one language, lossless trees that exist for every input, and semantic analysis
  built on them.
- **Difference.** They are not grammar-driven. Roslyn's team states the property that PEGO states and tests:
  "incremental reparsing produces the same tree as normal parsing". It also explains how ambient context breaks it.
- **Lesson.** rust-analyzer's notes say that its incremental reparse needs no extra state, and that in practice
  "incremental reparsing doesn't actually matter much for IDE use-cases, parsing from scratch seems to be fast
  enough". Full-fidelity trees that always exist matter more to editors than reuse. PEGO's `Document` costs 5.6×
  the memory of the tree alone. It must earn that cost with measured latency wins on realistic edit traces, and the
  equality check in `pego edit-fuzz` must stay in the default test path.

### Competitor Strengths

What the incumbents do better today, and what PEGO's documents must not obscure:

- tree-sitter: a tree for every input (ERROR and MISSING nodes); changed ranges; persistent trees with structural
  sharing; included ranges for embedded languages; `tree-sitter fuzz`
  for every grammar author; UTF-16 input; one grammar natively and in the browser.
- gotreesitter: 206 grammars in pure Go; typed query code generation; deterministic work limits and a per-parse
  memory budget, both on by default; a timeout and cancellation; UTF-16; injections; a Wasm runtime.
- Lezer: step-wise `PartialParse` with `stopAt`; `parseMixed` for nested languages; compact buffer-backed trees.
- ANTLR: automatic grammar-independent recovery; direct left recursion with precedence; grammars-v4; pure-Go and
  TypeScript runtimes from one grammar; one runtime test suite run against every target; generated listeners and
  visitors.
- Langium and Chevrotain: automatic token insertion, deletion and resynchronization; optional parsing in a worker
  with cancellation; the whole semantic layer.
- gpeg: logarithmic edit application; reuse of memoized repetitions; a peer-reviewed evaluation up to 100 MB.
- pest: the most candid public resource policy of the group, with a stack guard on by default on native targets.
- pigeon: left recursion (experimental) and labeled-failure recovery in a Go PEG generator.
- cel-go and Expr: no grammar at all, with stated evaluation bounds.
- rust-analyzer, rowan and Roslyn: lossless trees, cheap edits without extra state, a tree for every input.
- grmtools and Menhir: recovery and error messages checked or measured at build time.

## Order of Proof

PEGO aims to serve the parsing need of every segment of the market map in the long term. The non-goals above still
apply: a grammar corpus to rival tree-sitter's, general context-free parsing and the semantic layer of a language stay
out of scope. The ranking below decides where PEGO earns proof first, so that one segment's adoption makes the case
for the next; it orders the work, it does not limit the scope.

### Scoring Criteria

Each segment is scored from 1 (low) to 3 (high) on five criteria, and the total ranks the segments. The scores are
judgements until the size proxies are measured.

| Criterion | 3 means | 1 means |
|:--|:--|:--|
| Need | A documented pain that costs users time, safety or a toolchain | Nice to have |
| Gap | Incumbents do not cover the need in the segment's language | A mature incumbent covers it |
| Readiness | PEGO ships what the segment needs today | Most of it is planned |
| Reach | Users find PEGO where they already are (Go modules, Go documentation) | They choose from an editor or platform ecosystem |
| Size | Many teams have the need (proxies: importers of goyacc, pigeon and participle; Go repositories with `.y` or `.peg` files) | Few teams |

### Ranking

| Rank | Segment | Need | Gap | Readiness | Reach | Size | Total |
|--:|:--|:-:|:-:|:-:|:-:|:-:|--:|
| 1 | Go teams that own a DSL, configuration, query or policy language | 3 | 2 | 2 | 3 | 2 | 12 |
| 2 | Go developer tools for a custom language | 3 | 2 | 1 | 3 | 1 | 10 |
| 3 | The same parser in Go and TypeScript | 2 | 2 | 2 | 2 | 1 | 9 |
| 4 | Stream processing of grammar-defined records | 2 | 1 | 2 | 2 | 1 | 8 |
| 4 | Editor highlighting for many languages | 2 | 1 | 1 | 1 | 3 | 8 |
| 6 | TypeScript DSLs with IDE support | 2 | 1 | 1 | 1 | 1 | 6 |
| 7 | Compilers of large programming languages | 1 | 1 | 1 | 1 | 1 | 5 |

Segment 1 is the first proof (the wedge).

- **Gap scores 2.** The ANTLR Go runtime and pigeon cover much of the need. What they do not cover is typed values
  from a target-independent grammar, the same parser in TypeScript, deterministic work counts and incremental
  reparsing under one contract.
- **Readiness scores 2 for trusted input and 1 for untrusted input.** A tagged release is missing; the compatibility
  policy and the Go version that generated code needs are written down ([Releasing](releasing.md)) but no release
  carries them yet. For untrusted input, safe defaults are missing too (see
  [Predictable Cost and Safety](#predictable-cost-and-safety)).
- **Segment 2 Readiness scores 1.** It needs a language server, UTF-16 positions and tree-producing recovery in a
  `Document`.
- **Sensitivity.** Segment 1 stays first under any single-point change to any score.

## Wedge and Sequencing

Work runs in tracks. Until the release, the Release checklist and at most one other track are active; after it, at most
two tracks are active at a time, each with one major item in progress. When a track frees up, the next one is taken in
this order: Safety, Credibility, Performance, Editor, TypeScript parity, Adoption, Targets and engines. Only one
milestone is a gate: **Credibility**. It is met when the cross-tool benchmark is published with its harness and its
losses. Until then, no document claims speed relative to another tool. Performance targets are tracked continuously;
they are not gates, and the release does not wait for them.

**Release (the first milestone, a checklist).** A version that segment 1 can depend on:

- a tagged v0.x release with a compatibility policy that names the stable surfaces (grammar syntax, the generated Go
  API) and the unstable ones (`.pegoc`, bytecode, JSON forms);
- the Go version that generated code needs, documented: Go 1.27.1, the same as the module, with the effect of
  `go get -tool` on a consumer module ([Go Versions](releasing.md#go-versions));
- work limits on by default: `pego gen` generates parsers with work controls by default (what `-work-controls` turns
  on today), one fixed budget proportional to the input length (c·n with a large constant), and a lint warning for
  grammars outside the specification's sufficient conditions for linear work; at whatever overhead the limits have
  then, plus a work-limit flag in `pego parse` and [SECURITY.md](../SECURITY.md) updated to these defaults; published
  performance figures are measured with the limits on; reducing the overhead follows in the Performance track, and the
  release does not wait for the complexity classifier;
- one work budget in place of two counters;
- readable long expected lists;
- typed entry points under their final names;
- README and site claims that match this document's limits (tagline, untrusted input, tested runtimes);
- the **wedge benchmark**: the same DSL-sized grammars (a calculator, a filter language, JSON) in PEGO, pigeon,
  participle and the ANTLR Go runtime. It reports time on short and long inputs, allocations, generated code size,
  binary-size delta and work-limit overhead, with every loss published. This is the first credibility work and the
  cheapest one;
- a minimal launch with the tag: an announcement, a Go forum post, and a benchmark page with the wedge results and the
  playground link. The metric clocks start on the launch date.

Ready-made parsers stay in-repo test workloads; making them go-gettable is deferred by policy and is not a release
condition.

| Track | Next items | Later |
|:--|:--|:--|
| Credibility (the gate) | The wedge benchmark; then the full cross-tool harness on Apple silicon and Linux x86-64; recovery quality on two languages | Recovery quality on more languages |
| Performance | The work-limit overhead first, then the targets below by impact; a `jsontext` baseline | Generation profiles once the pass pipeline exists |
| Safety | Default work limits; the work-limit flag; `SECURITY.md`; optional input-length and memory caps; regression suites with step ratios | The complexity classifier (research, not on the critical path to release) |
| Editor | A language server for one user grammar; a public position map with UTF-16; tree-producing recovery in a `Document` | Semantic tokens through the server; an action-free concrete tree; text query syntax and standalone highlighting output |
| TypeScript parity | UTF-16 positions | `Document` and streaming in generated TypeScript |
| Adoption | `llms.txt` and a compact grammar reference; pigeon and participle migration recipes; a contributor guide | A pigeon grammar converter |
| Targets and engines | None before the release | A third target language, once every backend is on the Core IR and the conformance suite runs independent of the target; a second implementation of the bytecode in another language, once the bytecode is stable and versioned |
| Deferred by policy | None | go-gettable ready-made parsers; 32-bit Go |

The order follows the order of proof. Segment 1 needs Release and the wedge benchmark. Segment 2 needs the Editor
track, and Credibility makes its adoption defensible. Segment 3 needs TypeScript parity. Each new target language or
engine then extends the same contract and the same targets.

## Pillars

### Correctness by One Contract

For the features they share, every backend and target gives the same values, errors and positions, within each
backend's limits. A `Document` gives what a fresh parse gives.

- **Mechanism today.**
  - Differential tests across the three engine backends, both position units, memoization on and off, recognition,
    and generated Go and TypeScript.
  - `pego edit-fuzz` for `Document`s.
  - Ten ready-made parsers checked against reference implementations: all of `GOROOT/src` against `go/parser`,
    CPython's `ast.dump` on the standard library and 61,540 snippets, all 402 yaml-test-suite cases, and 2,527 CEL
    conformance tests.
- **Goal.** Feature parity is listed per backend, never approximated, and each gap in
  [What One Grammar Gives You](why-pego.md#what-one-grammar-gives-you) either closes or stays documented. Today only
  the Go and TypeScript generators use the Core IR. Moving the closure and bytecode backends onto it is an accepted
  design ([design record 062](design/records/062-backend-independent-optimization-ir.md)) that is not implemented. Once it lands,
  one optimization serves every backend.

### Performance Leadership

Speed, memory and size are a pillar. The aim is the best measured speed in every target language, engine and use,
against the fastest tools for each platform and with every loss published, with memory and size chosen per program. The
targets below are the start of that aim: they cover generated Go, generated TypeScript and the engine, which exist
today, and each new target or engine adds its own rows against the best tools for its language.

**Baseline.** The design avoids allocation by construction: slab storage for nodes and lists, pooled frames, deferred
memoization, typed values built without an intermediate tree, and value elision in recognition. The
[optimization catalog](optimizations/README.md) indexes 85 mechanisms, and each has a page with its evidence. It also
keeps 123 numbered records of measured changes and the experiments that did not pay off. 62 of the 85 mechanisms
have no maintained same-source control, so their contribution cannot be re-measured at a given commit.

| Measure (workload) | Today |
|:--|:--|
| Typed `ParseAST` time ÷ standard library (JSON, XML) | 1.11×, 1.33× |
| Typed `ParseAST` time ÷ standard library (CSV, two arithmetic workloads) | 3.10×, 3.36×, 5.41× |
| Generated `Parse` time ÷ standard library (five comparable workloads) | 1.62–6.76× |
| Generated `Recognize` time ÷ `json.Valid` (JSON) | 8.05× |
| Streaming, closure backend ÷ `encoding/csv` read loop (50,000 records) | 11.5× |
| Typed `ParseAST` allocated bytes ÷ standard library (JSON, XML) | 1.18×, 1.25× |
| Generated Go with finite work limits ÷ without (`Parse`; JSON and XML, generated with `-work-controls`) | 1.30–1.47× |
| Engine backends with a context or limits active ÷ inactive | 1.45–1.79× |
| Allocations per parse: generated `Recognize`; typed `ParseAST` ÷ standard library (JSON, CSV, XML) | 0 on JSON, CSV and Operators Rule arithmetic, 74–1,700 on the other workloads; 260, 184, 263 against 53 k, 10 k, 80 k |
| Generated TypeScript `parse` ÷ generated Go `Parse` (seven workloads) | 2.43–4.00× |
| `Document` one-character edit and reparse (Minilang, 89 KB; engine backends) | 116–133 µs |
| `Document` at 100,000 lines, one-digit replacement (incremental guide, Apple silicon) | `Edit` 0.33–0.36 ms, then reparse 0.61–1.85 ms; length-changing edits not measured |
| `Document` memory ÷ tree memory at 100,000 lines (incremental guide) | 5.6× (151 MiB against 27 MiB) |
| Largest generated `parser.go` (DuckDB) | 1.59 MB |
| Short-input latency, generated package size and binary-size delta against pigeon, participle and ANTLR Go | Not measured |

The JSON baseline is `encoding/json` `Unmarshal` into `any`, as built by Go 1.27.1. The report does not record the
`GOEXPERIMENT` setting or which implementation of the package it measured, and a baseline that decodes with
`jsontext` directly is not measured; both are benchmark gaps. Standard-library ratios are a minimum bar for
leadership, not a comparison with the fastest Go parsers. The JSON rows move to the `jsontext` baseline once it
exists.

**Targets.** Ratios are measured with the repository suite on the same machine and revision, in the default
configuration. From the first release, generated parsers have work controls by default and published figures are
measured with the limits on, so every speed row includes their cost. The standard-library parsers do
different work (`encoding/json` builds no positions, `encoding/xml` returns tokens, `encoding/csv` returns records,
`go/parser` builds a positioned Go syntax tree), so each row names the output class it compares; typed values and
recognition are the like-for-like comparisons and carry the strictest targets. "Credibility" is the target for the
Credibility milestone report; "long-term" is the leadership goal. A value marked "stretch" needs a mechanism without a
measured prototype, or it conflicts with what the outputs differ in; it becomes a commitment once a prototype is
measured. The unmarked values are commitments.

**First item: the work-limit overhead.** With limits on by default, their overhead multiplies every other speed row,
so cutting it comes first in the Performance track.

| Measure (output class) | Credibility | Long-term | Mechanisms |
|:--|:--|:--|:--|
| Work-limit overhead, generated Go `Parse` and `ParseAST`, limits on ÷ off | ≤ 1.15×; ≤ 1.05× (stretch) | ≤ 1.05×; ≤ 1.02× (stretch) | One work budget; charging input by position change at rewinds and rule exits instead of per unit read; one charge per straight-line block; context polls only on recursive cycles and loop back-edges |
| Typed `ParseAST` ÷ standard library, JSON and XML (typed values with spans) | < 1.0× | < 1.0× | Statically typed generated Go; byte positions; smaller string nodes |
| Typed `ParseAST` ÷ standard library, JSON and XML (typed values without spans, byte positions) | ≤ 1.0× | ≤ 0.6× (stretch) | The above; an option to generate values without spans |
| Typed `ParseAST` ÷ standard library, CSV (without spans) and arithmetic (with spans, against `go/parser`'s positioned AST) | ≤ 2.0× | ≤ 1.0× | Byte-search scans for character runs; memoizing members of left-recursion cycles; cross-rule optimization; statically typed generated Go |
| Generated `Parse` ÷ standard library, comparable workloads (a positioned tree with rule names, a richer output than the baselines) | ≤ 2.0× | ≤ 1.5×; ≤ 1.0× (stretch) | Cross-rule optimization; expected sets on request; depth checks only where recursion is possible; smaller nodes; optional arenas |
| Generated `Recognize` ÷ `json.Valid` (acceptance and error offset, no expected sets) | ≤ 3.0× | ≤ 1.5×; ≤ 1.0× (stretch) | Expected sets on request; depth checks only where recursion is possible; byte positions; a table-driven scanner for lexical rules (not yet designed) |
| Allocations per generated `Recognize`, pooled parser in steady state | 0 on JSON, CSV and arithmetic (met today) | 0 on all eight workloads (stretch) | Pooled scratch; zero-copy captured text; pooled left-recursion and recovery state |
| Allocations per typed `ParseAST` | Count grows no faster than the logarithm of the input (slab growth only) | 0 with a caller-owned arena (stretch) | Slab storage; statically typed generated Go; optional arenas |
| Typed allocated bytes ÷ standard library, JSON and XML | ≤ 1.0× (with spans) | ≤ 0.8× (with spans); ≤ 0.5× without spans (stretch) | Statically typed generated Go; values without spans; smaller string nodes; byte positions |
| Generated TypeScript `parse` ÷ generated Go `Parse` | ≤ 2.0× | ≤ 1.5×; ≤ 1.2× (stretch) | Close the TypeScript Flow IR slowdown; shared sub-expressions; cheaper typed conversion and UTF-8 decoding; JavaScript-specific layouts for the stretch value |
| Generated Go streaming ÷ `encoding/csv` read loop (benchmark to be added) | ≤ 2.0× (stretch) | ≤ 1.0× (stretch) | None yet beyond the batch mechanisms |
| `Document` edit plus reparse at 100,000 lines, p99 | Same-length edits ≤ 1 ms; length-changing edits ≤ 2 ms (stretch) | Both ≤ 0.5 ms (stretch) | Edits and reparses that do not grow with the document; reused input tables |
| `Document` memory ÷ tree memory | ≤ 3× | ≤ 2×; ≤ 1.5× (stretch) | Byte positions; compact memo entries; memoizing only reusable units in a `Document`; values without spans |
| Short-input latency (≤ 1 KB) ÷ the faster of pigeon and participle, same grammar and output class | ≤ 1.0×; ≤ 0.7× (stretch) | ≤ 0.7×; ≤ 0.5× (stretch) | Generate only used features; cheaper per-parse options; pooled scratch |
| Minimal generated package and binary-size delta | Measured and published against pigeon, participle and ANTLR Go | Smallest of the set at equal features | Generate only used features; expected sets on request; profiles |
| Size profile, DuckDB `parser.go` | ≥ 15% smaller than the speed profile | Smallest of the cross-tool set at equal features | Profiles; generate only used features; depth checks only where recursion is possible |
| Cross-tool batch parsing (harness below), same output class | Published, with losses reported | Fastest pure-Go framework on every published batch workload, with every loss published | All of the above |
| Cross-tool incremental edit latency, same traces and output class | Published against gotreesitter and gpeg | Fastest pure-Go framework on same-length edits; within 1.5× of tree-sitter's C runtime (stretch) | Edits and reparses that do not grow with the document |

A target that the first cross-tool baseline shows to be wrong is revised in this document, with the evidence.

**Mechanisms.** The planned work that moves these numbers, with the evidence that motivates each:

- **Statically typed generated Go.** Typed parsers pass every value as `any` and build values through dynamic helpers,
  although the type checker knows each type; removing the boxing targets typed time and bytes.
- **One work budget.** Two counters are charged today; one budget simplifies the API and the charging code on the
  controlled path.
- **Cheaper work charging.** A controlled generated parse charges every unit it reads and polls at every rule call;
  inlining the common charge left 1.30–1.47× on `Parse`. Charging the input by position change at rewinds and rule
  exits counts the same reads deterministically (forward reads equal the end position plus the rewound distance),
  a straight-line block can be charged once with its static count, and the context needs polling only on recursive
  cycles and loop back-edges. None of these has a measured prototype yet.
- **Depth checks only where recursion is possible.** Every rule entry checks the depth today. Only rules on a
  recursive cycle can exhaust the stack, so the other checks are removable code and time.
- **Expected sets on request.** A recorded experiment that skipped expected sets saved about 10% time on Minilang and
  nothing on JSON and CSV, and it was not adopted
  ([experiments](optimizations/README.md#experiments-that-did-not-pay-off)). The case for this mechanism rests on
  generated size, which is still to be measured.
- **Cross-rule optimization.** Inlining and shared prefixes across rules in generated code, where nodes, values, memo
  entries and errors stay the same; disabled under tracing.
- **Generate only used features,** and prune unreachable rule bodies and typed rule tables, for size.
- **Byte positions as the default, if measured better.** Code-point positions convert the whole input to `[]rune`,
  four bytes per code point.
- **Optional arenas, lazy text and compact or absent spans.** A `TNode[string]` is half span; uses that never read
  positions pay for them.
- **A configurable pass pipeline with `speed`, `memory` and `size` profiles**
  ([design record 063](design/records/063-configurable-optimization-pass-pipeline.md)), so that each trade-off is a
  measured option instead of a fixed choice.
- **Linear behavior where it can be linear.** Two changes remove quadratic and exponential families that also cost
  time on ordinary grammars: memoizing members of left-recursion cycles, and reusing repetition runs after
  backtracking ([design record 064](design/records/064-grammar-complexity.md)).
- **Document input tables reused across parses.** In code points, a warm, unedited typed `Document` parse takes about
  12× the time it takes in bytes, because it rebuilds the source table
  ([design record 044](design/records/044-generated-incremental-and-stream-parsers.md)).
- **Edits and reparses that do not grow with the document.** Memo entries already take an edit when they are looked
  up, but the reparse after a one-digit edit grows linearly from 10,000 to 100,000 lines (incremental guide), because
  a resumed repetition walks every reused element, and a length-changing edit moves every later node. Tree-structured
  repetition records and node positions relative to their parent would remove both; relative positions change the
  `Node` API, which returns absolute positions today.
- **Memoizing only reusable units in a `Document`.** A `Document` keeps a memo entry per rule call next to its tree
  (incremental guide); entries that a reparse can never reuse cost memory without saving time.
- **VM instructions for class runs and terminal programs,** if profiles of the language parsers justify them.
- **Close the TypeScript Flow IR slowdown.** Generated TypeScript prints every rule from the Flow IR. On the recovery
  workload it is 4–6% slower than the printer it replaced, for a cause not yet identified
  ([record 122](optimizations/122-typescript-from-flow-ir.md)); finding the cause also serves size.

**Proof.** A performance claim beyond this repository needs a cross-tool benchmark that maintainers of the other tools
can rerun and dispute. The wedge benchmark comes first and covers pigeon, participle and the ANTLR Go runtime.

- **Workloads.** Fixed inputs from a seeded generator plus pinned public corpora: JSON, CSV, XML, a large Go corpus
  (`GOROOT/src`), TypeScript and Python files, short DSL inputs, and recorded edit traces for incremental parsing.
- **Output classes.** Each comparison names what both sides produce (acceptance only, a positioned tree, typed values,
  changed ranges). One ratio never mixes classes.
- **Tools.**
  - The Go standard library, including `encoding/json/jsontext`.
  - tree-sitter through its official Go binding, gotreesitter and gpeg.
  - The ANTLR Go runtime, pigeon and participle.
  - Upstream grammars are used where they exist, and grammar sources and versions are pinned. cgo-based tools live in
    a separate module, so PEGO itself stays cgo-free.
- **Machines.** Apple M3 Max and a stated Linux x86-64 machine. The report states the CPU, OS, Go version,
  `GOMAXPROCS`, load and sample count.
- **Harness.** It lives in the repository and runs with one command. Results are generated, never edited by hand, and
  raw rows are committed with each published run. Losses go in the same table as wins. Maintainers of the compared
  tools are invited to review their configurations before publication.

### Predictable Cost and Safety

PEGO does not promise linear time. It states what a parse costs and bounds the work. It is not yet safe by default
for untrusted input; the goals below make it so.

- **Today.**
  - The specification defines work, the expressions that repeat it, classes of growth and sufficient conditions for
    linear work ([Time and Memory](../spec/runtime/parsing.md#time-and-memory)).
  - The nesting depth is capped by default.
  - The work limit (`MaxWork`) and context cancellation are opt-in. Generated parsers carry them only
    when generated with `-work-controls`. They cost 1.30–1.47× on generated Go `Parse` and 1.45–1.79× on the engine
    backends.
  - For a given artifact, input and options, the counts are deterministic. With `MaxWork` set, a parse stops once
    its counted work exceeds it, whatever its class of growth.
  - Cancellation follows the caller's clock, and a blocked reader or callback cannot be interrupted.
  - There is no memory or input-size limit. Work limits do not bound memory, and the specification has a grammar with
    a linear number of evaluations and exponential value size.
  - `pego parse` has no work-limit flag. [SECURITY.md](../SECURITY.md) states what a parse bounds today and what the
    caller must bound.
- **Goal: work limits on by default.** Every parse gets a work budget proportional to the input length (c·n). At the
  release, c is one fixed, large constant, calibrated on the repository grammars and ready-made parsers so that
  ordinary grammars stay well below it, and a lint warns about grammars outside the specification's sufficient
  conditions for linear work. An unlimited parse has to be requested explicitly. The complexity classifier of the
  research track can later refine c per grammar; the release does not wait for it. Generated parsers are included:
  from the first release, `pego gen` generates them with work controls by default, as `-work-controls` does today,
  and published performance figures are measured with the limits on. The default ships at the overhead the limits
  have then; the overhead target follows separately.
- **Goal: one place for production limits.** Optional caps on input length and on estimated memory (memo entries and
  result trees) fail with the same kind of error as the work limit.
- **Other goals.**
  - A lint for self-growing variables.
  - A regression suite that asserts step ratios at n, 2n, 4n and 8n.
  - One work budget.
  - A work-limit flag in `pego parse`.
- **Research track.** A sound classifier that labels each start rule as linear, linear given an environment bound,
  polynomial of degree k or exponential, with a witness
  ([design record 064](design/records/064-grammar-complexity.md), proposed). It is not on the critical path to
  release.

### Typed, Portable Output

- **Today.** `node[T]` in the language; Go structs, `TNode[T]` and sealed interfaces in generated Go; TypeScript
  interfaces; stdlib-only Go; dependency-free TypeScript; a published bytecode specification implemented by the Go
  VMs.
- **Goals.** Static types through generated Go; union interfaces generated from value types; `Document` and streaming
  in TypeScript; UTF-16 positions in TypeScript; then more target languages and a second VM implementation, each
  held to the same contract.

### Editor-Grade Parsing

- **Today.**
  - An incremental `Document` for any grammar, with a stated and fuzz-tested fresh-parse contract. Reuse depends on
    rule structure.
  - `#recover` on every backend.
  - A language server and VS Code extension for `.pego` sources.
- **Not editor-grade yet.**
  - A `Document` returns a tree only if the grammar's `#recover` rules handle each error. `#missing` is unavailable in
    a `Document`.
  - Edit application grows with document size, and memory is 5.6× the tree.
  - There is no changed-range API, no language server or highlighting for user grammars, and no UTF-16 position map.
  - PEGO is therefore not recommended for reparsing on every keystroke without these qualifiers.
- **Goal: editor-grade error tolerance.** A tree for every buffer state in a `Document`, through `#missing` in
  `Document`s and on every backend, or through recovery that needs fewer annotations. Further goals: a language
  server for a user grammar, a public position map including UTF-16, an action-free concrete-tree mode, measured
  recovery quality and an end-to-end edit-latency benchmark.

## What Must Be True

| Goal | Measure | Today | Milestone or track |
|:--|:--|:--|:--|
| Users can depend on a version | Tagged v0.x release; compatibility policy naming stable and unstable surfaces | No tag | Release |
| Generated code states its requirements | Minimum Go version documented; `go get -tool` effect on consumers stated | Documented in [Releasing](releasing.md#go-versions): Go 1.27.1 for PEGO and generated code; `go get -tool` raises a consumer's `go` line to 1.27.1 | Release |
| Untrusted input is bounded by default | Default work budget (one fixed c·n budget, and a lint warning for grammars outside the sufficient conditions for linear work); work controls generated by default; `SECURITY.md`; work-limit flag in `pego parse` | Limits opt-in, and in generated parsers only with `-work-controls`; `SECURITY.md` states today's policy; no flag | Release |
| Limits are simple | One work budget across Go API, engine, generated Go and TypeScript | Two counters | Release |
| Errors are readable | Long expected lists capped or grouped, with a test on a large grammar | Full lists | Release |
| Public claims match measurements | README and site claims match this document's limits (tagline, untrusted input, tested runtimes) and [Benchmark Results](benchmarks.md) | The README's figures, tagline, runtime list and untrusted-input statement match; site pages not audited | Release |
| The wedge is measured | Wedge benchmark against pigeon, participle and ANTLR Go published | None | Release (credibility work) |
| Speed is comparable across tools | Cross-tool report on two machines | None | Credibility (gate) |
| Recovery is measured | Repair rate and time on a corpus of invalid inputs, two languages first | Unmeasured | Credibility track |
| Performance improves | The targets of [Performance Leadership](#performance-leadership), tracked per report | See baseline | Performance track |
| Cost is classified | Classifier labels every grammar in `examples/` and `parsers/` | Specification only; classifier proposed | Safety track (research) |
| A user grammar gets editor support | Language server for one ready-made language, setup documented for VS Code and Neovim | `.pego` only | Editor track |
| TypeScript reaches parity | UTF-16 positions, then `Document` and streaming in generated TypeScript | None of these | TypeScript parity track |
| Every change is checked automatically | Automated checks run on each change | Paused; local validation only | Ongoing |
| The test suite runs often | Root suite time measured and published | Not measured | Ongoing |
| More than one person can release | A second maintainer with review and release rights; documented release process | One maintainer | Ongoing |

## Risks

Each risk has a trigger that forces a decision, not only a signal to watch.

| Risk | Likelihood / impact | Trigger | Decision or mitigation |
|:--|:--|:--|:--|
| Language models make hand-written parsers cheap, and they know ANTLR, tree-sitter and pigeon grammars but not `.pego` | High / high | The share of PEGO-generated parsers among new public Go parser modules does not rise for two consecutive quarters after the launch | Argue with verified properties (fresh-parse equality, limits, differential tests) that generated hand-written code lacks; publish `llms.txt`, a compact grammar reference and worked conversions |
| A cgo-free tree-sitter path matures: gotreesitter reaches v1 with incremental equality, or tree-sitter runs under a pure-Go WebAssembly runtime such as wazero | Medium / high | gotreesitter v1, or a general wazero-based tree-sitter module | Demote segment 2; redirect the Editor track to language servers for typed DSLs; compete on typed values, generated TypeScript and limits |
| Visibility: PEGO loses on awareness, not on design (3 stars and no release after three years; gotreesitter reached 581 stars in eight months) | High / high | Fewer than 10 external importers six months after the launch | Launch plan with channels (Go forums, newsletters, a talk); playground share links in every guide; reconsider segment 1 messaging |
| The Go 1.27.1 floor narrows adoption; `go get -tool` raises a consumer module's `go` line to 1.27.1 | Medium / medium | Requests for older Go versions; consumers pinning old toolchains | Reconsider the floor if requests for older Go versions arrive; document `go run` as an alternative to the tool route |
| Format churn after the release (Core IR for every backend, profiles, possible default changes) | Medium / medium | A format or API change needed after the tag | Release as v0.x; name stable surfaces (grammar syntax, generated Go API) and unstable ones (`.pegoc`, bytecode, JSON forms); the formatter and `pego fix` rewrite old syntax |
| A safety incident: an exponential grammar or an unbounded input hangs or exhausts memory in a user's service | Medium / high | One public report of a hang or out-of-memory | Before release: limits on by default, generated parsers included; `SECURITY.md`; guides that require limits for untrusted input. After an incident: revisit the default budget and the priority of the input-length and memory caps |
| Bus factor: one maintainer holds the work and the knowledge | High / high | Any pause in maintenance longer than a month | Design records and specifications carry the reasoning; a scripted release process; a contributor guide and good-first-issue labels; reduce the surface (consider freezing one of the two Go bytecode VMs; a second implementation of the bytecode belongs to the Targets and engines track, in another language, after the release) |
| Research pull: research-heavy goals (classifier, profiles, a second VM) compete with the release | Medium / medium | A research item is in progress while a Release item is open | The classifier stays a research track; one major item in progress per track |
| Benchmark methodology is challenged | High / medium | Issues from other maintainers | Published harness, pinned versions, output classes, invited review, losses reported |
| Typed output stops being a difference: gotreesitter already generates typed Go structs for query matches, and tree-sitter's `node-types.json` exists for code generators | Medium / medium | A typed tree generator for tree-sitter grammars in Go gains users | Lead on values built by grammar actions (not only typed nodes), generated TypeScript from the same grammar and the tested contract |
| tree-sitter documents complexity bounds and memory limits | Medium / medium | Documented bounds or a memory budget in tree-sitter itself | The safety difference narrows; compete on typed values and deterministic counts |
| gpeg or another Go incremental PEG adds typed output and limits | Low / medium | Releases or tags there | Adopt logarithmic edit application; lead on the tested contract and cross-tool evidence |
| ANTLR 5 or antlr-ng delivers a portable runtime | Low / low | New releases | ANTLR stays non-incremental; emphasize incremental parsing and streaming |
| Scope sprawl: ten ready-made parsers, five backends and many options dilute maintenance | High / medium | Suite time grows; stale guides | Ready-made parsers and new targets stay deferred; measure suite time |

## Metrics

**North star.** The number of external public modules that import PEGO or a PEGO-generated parser (pkg.go.dev
importers plus a GitHub code search for the generated-file header). It counts teams that shipped a PEGO parser, which
is the wedge's success.

Thresholds count from the launch date and are revisited once measured.

| Metric | Source | Threshold | Informs |
|:--|:--|:--|:--|
| External importing modules (north star) | pkg.go.dev; code search | ≥ 10 within six months of the launch; ≥ 50 within 18 months | Segment 1 |
| External issue and pull-request authors | GitHub | ≥ 5 within six months of the launch | Bus factor, adoption |
| Time from install to first generated parser | Timed tutorial walkthrough | ≤ 10 minutes | Funnel: trial |
| Stars and inbound links, as visibility proxies | GitHub | Tracked, no threshold | Funnel: awareness |
| Wedge benchmark rows won and lost | Wedge benchmark | Published; losses fall over time | Credibility |
| Performance ratios of [Performance Leadership](#performance-leadership) | `go run ./bench/report` and the cross-tool harness | The targets | Performance track |
| Equality failures found by `pego edit-fuzz` and differential tests | Test runs | Zero open at each release | Correctness |
| Recovery repair rate and time | Recovery evaluation | Published for two languages | Credibility, Editor |
| Root test-suite time | Local runs, then automated checks | Measured; no growth between releases | Bus factor |

**Pivot rules.** If segment 1 has fewer than 10 external importers 12 months after the launch, revisit the
wedge with the importer and issue data. If no external user adopts a `Document` within 12 months of the first user-grammar language
server, the Editor track moves to maintenance.

## Sources

PEGO facts are from this repository. Performance figures are from [Benchmark Results](benchmarks.md),
[Performance](performance.md), [Incremental Parsing Performance](guide/incremental-performance.md) and
[Streaming](guide/streaming.md). Comparisons checked on 2026-10-29 against the versions named above:

- tree-sitter 0.27.1: [repository and documentation](https://github.com/tree-sitter/tree-sitter/tree/v0.27.1);
  [`fuzz.rs`](https://github.com/tree-sitter/tree-sitter/blob/v0.27.1/crates/cli/src/fuzz.rs);
  [list of parsers](https://github.com/tree-sitter/tree-sitter/wiki/List-of-parsers); Helix
  [`languages.toml`](https://github.com/helix-editor/helix/blob/master/languages.toml); Go
  [binding](https://github.com/tree-sitter/go-tree-sitter); issues
  [6057](https://github.com/tree-sitter/tree-sitter/issues/6057) (open),
  [6067](https://github.com/tree-sitter/tree-sitter/issues/6067) (open),
  [5967](https://github.com/tree-sitter/tree-sitter/issues/5967) (closed 2026-10-03); npm downloads of
  `web-tree-sitter`
- gotreesitter 0.56.0: [README](https://github.com/odvcencio/gotreesitter/blob/v0.56.0/README.md),
  [API guide](https://github.com/odvcencio/gotreesitter/blob/v0.56.0/docs/api-guide.md),
  [`parser_api.go`](https://github.com/odvcencio/gotreesitter/blob/v0.56.0/parser_api.go) (`ParseWorkLimits`),
  [v1 design plan](https://github.com/odvcencio/gotreesitter/blob/v0.56.0/docs/v1-design.md)
- gpeg at commit `d601f967`: [repository](https://github.com/zyedidia/gpeg) (`incremental_test.go`); Yedidia and Chong,
  [Fast Incremental PEG Parsing](https://zyedidia.github.io/preprints/gpeg_sle21.pdf) (SLE 2021)
- ANTLR 4.13.2: [repository](https://github.com/antlr/antlr4), [targets](https://github.com/antlr/antlr4/blob/master/doc/targets.md),
  [left recursion](https://github.com/antlr/antlr4/blob/4.13.2/doc/left-recursion.md),
  [runtime test suite](https://github.com/antlr/antlr4/tree/4.13.2/runtime-testsuite), Go runtime
  [4.13.1](https://github.com/antlr4-go/antlr) (`error_strategy.go`), issues
  [1901](https://github.com/antlr/antlr4/issues/1901) and [3279](https://github.com/antlr/antlr4/issues/3279);
  [antlr5](https://github.com/antlr/antlr5); [antlr-ng](https://github.com/antlr-ng/antlr-ng); Parr, Harwell and Fisher,
  [Adaptive LL(*) Parsing](https://www.antlr.org/papers/allstar-techreport.pdf) (OOPSLA 2014)
- Lezer 1.4.11: [guide](https://lezer.codemirror.net/docs/guide/), [reference](https://lezer.codemirror.net/docs/ref/)
  (`PartialParse`, `parseMixed`), [`lr` README](https://github.com/lezer-parser/lr); npm downloads of `@lezer/lr`
- Langium 4.4.0 and Chevrotain 13.2.0: [Langium documentation](https://langium.org/docs/introduction/),
  [`async-parser.ts`](https://github.com/eclipse-langium/langium/blob/main/packages/langium/src/parser/async-parser.ts),
  [Chevrotain issue 843](https://github.com/Chevrotain/chevrotain/issues/843)
- pigeon 1.3.0: [`doc.go`](https://github.com/mna/pigeon/blob/v1.3.0/doc.go), [issue 62](https://github.com/mna/pigeon/issues/62)
- participle 2.1.4: [README](https://github.com/alecthomas/participle/blob/v2.1.4/README.md), issues
  [213](https://github.com/alecthomas/participle/issues/213), [102](https://github.com/alecthomas/participle/issues/102)
- pointlander/peg: [repository](https://github.com/pointlander/peg); goyacc:
  [documentation](https://pkg.go.dev/golang.org/x/tools/cmd/goyacc), [Go 1.6 release notes](https://go.dev/doc/go1.6)
- cel-go 0.30.0: [README](https://github.com/google/cel-go); Expr 1.17.8: [README](https://github.com/expr-lang/expr)
- pest 2.9.5: [SECURITY.md](https://github.com/pest-parser/pest/blob/master/SECURITY.md); cpp-peglib 1.30.0:
  [README](https://github.com/yhirose/cpp-peglib); CPython: [PEP 617](https://peps.python.org/pep-0617/); Ohm 17.5.0:
  [18.0 release notes](https://github.com/ohmjs/ohm/blob/main/doc/releases/ohm-js-18.0.md), Dubroy and Warth,
  [Incremental Packrat Parsing](https://ohmlang.github.io/pubs/sle2017/incremental-packrat-parsing.pdf) (SLE 2017)
- grmtools 0.15.0: Diekmann and Tratt, [Don't Panic!](https://arxiv.org/abs/1804.07133) (ECOOP 2020); Menhir:
  [manual](https://gallium.inria.fr/~fpottier/menhir/manual.html)
- Roslyn: [parser design notes](https://github.com/dotnet/roslyn/blob/main/docs/compilers/Design/Parser.md);
  rust-analyzer: [syntax notes](https://github.com/rust-lang/rust-analyzer/blob/master/docs/book/src/contributing/syntax.md)
