# Performance Tuning Log

This document describes benchmark workloads and methods, analyzes the latest full-suite results, and links to the optimization catalog. The catalog preserves the numbered change history, evidence, applicability table, and experiments that did not pay off.
Update the catalog in the same commit as an optimization. Correctness-only fixes do not belong in the catalog; record measured performance impact in their commit messages.

Keep a summary with the baseline commit, candidate change, workload, environment, sample method,
results, tradeoffs and reproduction command. Focused raw output is local working data and is not
committed by default; source commits and benchmark code allow reruns. Keep raw output in Git only
when it has exceptional long-term value that a summary and rerun cannot preserve. The existing
full-suite `bench/results.txt` remains the input to the generated results document.

The measured results of every benchmark are in [benchmarks.md](benchmarks.md), which is generated from a run; this
document describes the benchmarks, analyzes the results, and records how they came about.

- [Where PEGO stands](#where-pego-stands): an analysis of the latest results
- [How to measure](#how-to-measure): the benchmarks, their workloads and method, and tools for measuring a change
- [Optimization catalog](optimizations/README.md): applicability, numbered changes, and experiments

## Where PEGO stands

An analysis of the results in [benchmarks.md](benchmarks.md) as measured on 2026-10-10 at commit 2c68ccf (Apple M3
Max, Go 1.27.1). The figures below are rounded from that run; regenerating the results does not update this analysis,
so check the commit above against the one in [benchmarks.md](benchmarks.md) before relying on it.

This checkpoint uses three samples per case after the Go expectation-filter change (75). It is a current
workload snapshot, not an interleaved estimate of any single optimization's effect. The focused sample-constructor,
duplicate-capture checker, variable-dependency graph and redundant no-edit Document schedules are outside
this full suite; their paired measurements remain in entries 70–73. The tiny JSON and current 27-policy CEL
controls are also outside this suite; entry 74 holds their interleaved before/after measurements. The large
Python and tiny JSON controls in entry 75 are outside this suite too. Entry 76 describes later focused
generated-recognition measurements; this full-suite snapshot predates that change. This run covers 131 cases in 459.013 seconds
on a clean measured commit.

**Backends.** For code-point Node parsing, generated Go parsers take 0.53–0.68× the time of the closure backend.
The recursive bytecode VM takes 1.08–1.37× and the iterative VM 1.30–1.91×; the iterative VM pays most on Pratt expressions (1.91×), whose loop runs
as a state machine on its own stack. The closure backend is the default for this reason (see the
[runtime guide](guide/runtime.md#backends)); the VMs are for grammars without their AST and, iteratively, for deep
nesting.

**Typed values.** Where `ParseAST` builds the grammar's Go types directly (JSON, CSV, XML, the calculators, outline),
it takes 0.46–0.93× the time of generated `Parse` and 0.23–0.35 of its memory (0.30–0.85 of its allocations;
outline keeps most of them, in lists). CSV now returns a dedicated typed AST, rather than the Node tree measured
in older runs. On JSON and XML, typed parsing takes 0.79× and 0.88× the time of `encoding/json` and `encoding/xml`,
with 1.10× and 1.18× their bytes and about 201 and 280 times fewer allocations, while recording positions and error
expectations. Where values include CST nodes (minilang), `ParseAST` converts the Node tree: 1.06× the time and
1.26× the memory of generated `Parse`.

**Allocations.** Every backend makes a few hundred to about two thousand allocations per Node parse (318 for CSV,
767 for JSON, about 2,250 for outline on Closure), against 10,000–80,000 for the standard-library parsers: nodes, lists and fields come from
slabs, and the memo, the input buffers and the stacks are reused across parses. The bytes are another matter: a tree
of `Node`s on the closure backend takes 26–130 bytes per input byte (left recursion the most, through the
intermediate results it grows), which is what typed values save.

**Recognition.** `RecognizeOnly` takes 0.61–0.74× the time of a full parse on the closure backend. Most simple
workloads allocate only 4–10 KB; predicates reading values (XML's tag names, outline's indentation) take 3.71 MB and
1.95 MB, left-recursion growth takes 1.25 MB, and minilang/recovery take 0.56–0.68 MB. Generated `Recognize` takes
0.48–0.70× the time of generated `Parse`, yet is slower than typed `ParseAST` on JSON (1.13×), XML (1.07×) and the
left-recursive calculator (1.03×): `ParseAST` runs direct rules, one method per rule, while `Parse` and `Recognize` still run a method
per expression. Direct rules for the Node runtime and the recognizer are the most promising optimization left (see
[the backlog](https://github.com/ornew/pego/issues/1)).

**The standard library** is 1.7–6.4 times as fast as generated `Parse` where it applies. The gap is widest where
PEGO's work is structurally different: left recursion (6.4×, against `go/parser`'s hand-written precedence climbing)
and CSV (3.3×, `encoding/csv` builds no tree at all). `json.Valid`, a hand-written state machine, is 9.8 times as fast
as generated `Recognize` in this run.

**Grammar style.** The same expressions take 2.6 times as long with left recursion as with a Pratt expression on the
closure backend (2.2 generated, 1.5 typed), as you would expect from growing a seed at every level. Minilang with one line in seven broken parses in 0.98× the time of the clean
control in this run. Recovered statements are skipped, so those two inputs perform different matching work.

**Position units** make no consistent difference: byte positions take 0.94–1.08× the time of code points.

**Incremental parsing.** An edit and a reparse of the 300-function minilang program take 129–142 µs, 86–155 times
less than a full parse, on every backend. On the 50,000-record CSV document they take 2.3–2.6 ms, most of it moving
the reused records after the edit; there the VMs allocate 2.6 times as much as the closure backend (7.1 MB against
2.7 MB).

**Streaming** of 50,000 CSV records runs at 0.67–0.73 of the throughput of the 5,000-record batch workload,
a normalized CodePoints comparison across different input sizes. Streaming allocates 48–52 bytes per input byte and about
6.2 allocations per record, against 26 bytes per input byte and 0.06 allocations per record for the closure batch parse.
These are cumulative allocations for each workload, not retained heap measurements. The stream-memory checkpoint
includes memo retirement, construction-reference cleanup and bounded variable histories (changes 67–69); separate
long-stream tests verify retained state and heap growth. Per-element scratch still causes more allocations than batch reuse.

**Preparation.** Loading a `.pegoc` file takes 0.44–0.49× the time of compiling the grammar from source for the
closure backend, 0.51–0.66× for bytecode, and 0.21–0.27× for a file without the AST.

## How to measure

The benchmarks are in `bench/`. `go run ./bench/report` runs all of them (about eight minutes) and regenerates
[benchmarks.md](benchmarks.md); see [the benchmark workloads](#benchmark-workloads) for what they measure.

```bash
# Full benchmark suite (all backends, both position units, stdlib counterparts), and the results document
go run ./bench/report

# A single workload, with CPU and allocation profiles
go test ./bench -run '^$' -bench 'BenchmarkParse/JSON/closure/codepoints' -benchtime 20x \
    -cpuprofile cpu.out -memprofile mem.out -o bench.test
go tool pprof -top -nodecount 30 bench.test cpu.out
go tool pprof -sample_index=alloc_space -top -nodecount 30 bench.test mem.out
```

Notes:

- Wall-clock numbers vary by a few percent between runs on an idle machine, and by 10–20% on a busy one. Prefer `B/op`
  and `allocs/op`, which are nearly deterministic, and confirm time changes with interleaved runs of the binaries
  before and after the change (`-count 3` or more each).
- Results must not change: every optimization is covered by the equivalence tests (`go test ./...`), which compare
  all backends, both position units, memoization on and off, and recognition against full parses.
- A change to shared runtime code (input, memo table, calls) should be measured on the incremental and stream
  benchmarks too (`BenchmarkIncremental`, `BenchmarkStream`), not only on batch parsing: they reach code paths the
  batch benchmarks do not (see the pitfall in change 30).
- `go test -bench` splits the pattern at `/` and matches each part against one level of the benchmark name, so an
  alternation that spans levels (`(Parse/JSON|Recognize/CSV)`) matches nothing; run such sets separately.
- The engine benchmarks read the grammars from `examples/` and `parsers/` when they run, so when comparing a change to a grammar, run
  the benchmark binary while the old grammar is checked out (for example between `git stash` and `git stash pop`), not
  just a binary built from the old code. Generated parsers embed their grammar.
- The numbers in the optimization catalog are for the closure backend with code-point positions unless stated otherwise.

### Benchmark workloads

`TestWorkloads` in `bench/` checks, on reduced inputs, that every input can be parsed by every backend and that the
PEGO backends return the same results.

- Inputs are generated by `bench/inputs.go` from a fixed random seed, so they are identical across runs. They include
  multi-byte characters such as Japanese text.
- The grammars are taken unchanged from `parsers/` (JSON, CSV, XML) and `examples/` (the calculator in its Pratt and
  left-recursive forms, minilang, and outline). The generated parsers in `bench/gen/` are generated from them (`go generate ./bench`).
- Each benchmark runs three times and the median is reported.
- Comparisons with the standard library measure different outputs. Every PEGO backend builds a tree whose nodes carry
  positions (start and end) and rule names, and records the expectations at the farthest failure. The standard-library
  parsers produce:
  - `encoding/json`: decoding into `any` (no positions).
  - `encoding/csv`: `ReadAll` (a list of field strings with quotes removed).
  - `encoding/xml`: reading tokens with `Decoder.Token` (no tree).
  - `go/parser.ParseExpr`: a Go expression AST (with positions).

| Workload | Grammar | Input | Main focus |
|:--|:--|:--|:--|
| JSON | `parsers/json` | Array of objects, 262 KB | Lexical repetition, AST of union types |
| CSV | `parsers/csv` | 5,000 rows (211 KB); fields with quotes, commas and newlines | Simple repetition, many terminals |
| XML | `parsers/xml` | Nesting, attributes, character references, comments, CDATA; 262 KB | Nesting, a predicate comparing start-tag and end-tag names |
| Arith_Pratt | `examples/calculator/calc.pego` | Expression with 20,000 terms (133 KB), parentheses nested up to depth 40 | Pratt loop and longest match |
| Arith_LeftRec | `examples/calculator/calc_lr.pego` | Same as above | Left-recursion seed growing |
| Minilang | `examples/minilang` | 300 functions (89 KB) | PEG statements nested with Pratt expressions, keyword exclusion |
| Recovery | `examples/minilang` | Same as above, with one line in seven broken | `#recover`, expectation recording |
| Outline | `examples/outline` | 5,000 lines (83 KB), depth up to 10 | Predicates and variables (non-memoized rules) |
| Incremental | `examples/minilang` | Repeated one-character insertions and deletions in 300 functions | Memo reuse with `Document` (compared with a full parse each time) |
| Incremental, long | `parsers/csv` | Repeated one-character insertions and deletions in the middle of 50,000 records | Resuming long repetitions with `Document` |
| Stream | `parsers/csv` | 50,000 rows (2.2 MB) | Reading and discarding with `ParseStream` |
| Prepare | JSON, minilang | None | Compiling from source and loading `.pegoc` (with and without the AST) |

The 2026-10-09 run and the current analysis use the ready-made grammars in `parsers/csv` and `parsers/xml`.
Older runs used the simpler grammars of `examples/csv` and `examples/xml`, since removed. Their figures do not
isolate the effect of a runtime change across that grammar transition; use the paired measurements in each tuning entry.

The entries of the change log record the effect of each change when it was made; entries before change 13 were
measured on a 4-vCPU Intel Xeon virtual machine and are several times slower in absolute terms.

Starting point (first benchmark run, on the virtual machine): JSON took ~510 ms and allocated 206 MB in 2.6 M allocations.

## Where each optimization applies

See the [applicability table and implementation notes](optimizations/README.md#where-each-optimization-applies).

## Change log

Numbered change details, evidence, and reproduction commands now live in the [optimization catalog](optimizations/README.md). The anchors below preserve existing `performance.md` links.

<a id="1-value-free-rule-bodies-value-free-twins-transient-rules-0b2c903"></a>
- [1. Value-free rule bodies, value-free twins, transient rules (0b2c903)](optimizations/001-value-free-rule-bodies-value-free-twins-transient-rules-0b2c903.md)

<a id="2-frame-pooling-in-the-iterative-vm-8fab402"></a>
- [2. Frame pooling in the iterative VM (8fab402)](optimizations/002-frame-pooling-in-the-iterative-vm-8fab402.md)

<a id="3-node-fields-as-a-slice-54a5774"></a>
- [3. Node fields as a slice (54a5774)](optimizations/003-node-fields-as-a-slice-54a5774.md)

<a id="4-expected-sets-as-interned-ids-on-a-shared-stack-11a6ce3"></a>
- [4. Expected sets as interned IDs on a shared stack (11a6ce3)](optimizations/004-expected-sets-as-interned-ids-on-a-shared-stack-11a6ce3.md)

<a id="5-memo-table-with-per-position-chains-cf77e4f"></a>
- [5. Memo table with per-position chains (cf77e4f)](optimizations/005-memo-table-with-per-position-chains-cf77e4f.md)

<a id="6-slab-allocation-for-nodes-child-lists-and-frames-5d03bf2"></a>
- [6. Slab allocation for nodes, child lists and frames (5d03bf2)](optimizations/006-slab-allocation-for-nodes-child-lists-and-frames-5d03bf2.md)

<a id="7-recognition-mode-3f7e9bf"></a>
- [7. Recognition mode (3f7e9bf)](optimizations/007-recognition-mode-3f7e9bf.md)

<a id="8-fast-path-for-unmemoized-calls-scan-nesting-limit-d2db87d"></a>
- [8. Fast path for unmemoized calls, `SCAN`, nesting limit (d2db87d)](optimizations/008-fast-path-for-unmemoized-calls-scan-nesting-limit-d2db87d.md)

<a id="9-value-free-pratt-lines-discarding-trivia-in-example-grammars-936857c"></a>
- [9. Value-free Pratt lines; discarding trivia in example grammars (936857c)](optimizations/009-value-free-pratt-lines-discarding-trivia-in-example-grammars-936857c.md)

<a id="10-zero-copy-token-text-reusable-evaluation-context"></a>
- [10. Zero-copy token text; reusable evaluation context](optimizations/010-zero-copy-token-text-reusable-evaluation-context.md)

<a id="11-generated-parsers-catch-up-with-changes-410"></a>
- [11. Generated parsers catch up with changes 4–10](optimizations/011-generated-parsers-catch-up-with-changes-410.md)

<a id="12-memoizing-rules-that-read-variables"></a>
- [12. Memoizing rules that read variables](optimizations/012-memoizing-rules-that-read-variables.md)

<a id="13-allocation-free-pratt-attempts-and-pooled-pratt-frames-in-the-engine-and-vms"></a>
- [13. Allocation-free Pratt attempts and pooled Pratt frames in the engine and VMs](optimizations/013-allocation-free-pratt-attempts-and-pooled-pratt-frames-in-the-engine-and-vms.md)

<a id="14-one-operand-stack-for-vm-expression-code"></a>
- [14. One operand stack for VM expression code](optimizations/014-one-operand-stack-for-vm-expression-code.md)

<a id="15-allocation-free-action-plumbing-in-all-backends"></a>
- [15. Allocation-free action plumbing in all backends](optimizations/015-allocation-free-action-plumbing-in-all-backends.md)

<a id="16-start-positions-and-lambdas-from-chunks-in-the-vms"></a>
- [16. Start positions and lambdas from chunks in the VMs](optimizations/016-start-positions-and-lambdas-from-chunks-in-the-vms.md)

<a id="17-ascii-bitmaps-for-character-classes-in-the-vms"></a>
- [17. ASCII bitmaps for character classes in the VMs](optimizations/017-ascii-bitmaps-for-character-classes-in-the-vms.md)

<a id="18-memoizing-from-the-second-call-at-a-position"></a>
- [18. Memoizing from the second call at a position](optimizations/018-memoizing-from-the-second-call-at-a-position.md)

<a id="19-a-plain-call-path-for-unmemoized-rules-without-captures"></a>
- [19. A plain call path for unmemoized rules without captures](optimizations/019-a-plain-call-path-for-unmemoized-rules-without-captures.md)

<a id="20-splicing-the-memo-table-on-document-edits"></a>
- [20. Splicing the memo table on Document edits](optimizations/020-splicing-the-memo-table-on-document-edits.md)

<a id="21-changes-18-and-19-in-the-generated-parsers"></a>
- [21. Changes 18 and 19 in the generated parsers](optimizations/021-changes-18-and-19-in-the-generated-parsers.md)

<a id="22-bounded-memory-and-less-copying-in-stream-parsing"></a>
- [22. Bounded memory and less copying in stream parsing](optimizations/022-bounded-memory-and-less-copying-in-stream-parsing.md)

<a id="23-splicing-the-text-of-a-document-in-place"></a>
- [23. Splicing the text of a Document in place](optimizations/023-splicing-the-text-of-a-document-in-place.md)

<a id="24-the-plain-call-path-in-the-iterative-vm"></a>
- [24. The plain call path in the iterative VM](optimizations/024-the-plain-call-path-in-the-iterative-vm.md)

<a id="25-discarding-separators-in-the-example-grammars"></a>
- [25. Discarding separators in the example grammars](optimizations/025-discarding-separators-in-the-example-grammars.md)

<a id="26-lambda-calls-without-allocation-in-the-closure-backend"></a>
- [26. Lambda calls without allocation in the closure backend](optimizations/026-lambda-calls-without-allocation-in-the-closure-backend.md)

<a id="27-struct-and-call-operands-on-the-expression-stack-in-the-closure-backend"></a>
- [27. Struct and call operands on the expression stack in the closure backend](optimizations/027-struct-and-call-operands-on-the-expression-stack-in-the-closure-backend.md)

<a id="28-field-lists-and-list-built-ins-without-allocation-in-the-generated-parsers"></a>
- [28. Field lists and list built-ins without allocation in the generated parsers](optimizations/028-field-lists-and-list-built-ins-without-allocation-in-the-generated-parsers.md)

<a id="29-shifting-reused-subtrees-into-the-parsers-chunks"></a>
- [29. Shifting reused subtrees into the parser's chunks](optimizations/029-shifting-reused-subtrees-into-the-parsers-chunks.md)

<a id="30-building-the-code-point-offset-table-on-demand"></a>
- [30. Building the code-point offset table on demand](optimizations/030-building-the-code-point-offset-table-on-demand.md)

<a id="31-specialized-character-class-tests-in-the-closure-backend"></a>
- [31. Specialized character-class tests in the closure backend](optimizations/031-specialized-character-class-tests-in-the-closure-backend.md)

<a id="32-matching-literals-against-loaded-input-in-one-loop"></a>
- [32. Matching literals against loaded input in one loop](optimizations/032-matching-literals-against-loaded-input-in-one-loop.md)

<a id="33-smaller-vm-entries"></a>
- [33. Smaller VM entries](optimizations/033-smaller-vm-entries.md)

<a id="34-one-frame-per-plain-call-in-the-iterative-vm"></a>
- [34. One frame per plain call in the iterative VM](optimizations/034-one-frame-per-plain-call-in-the-iterative-vm.md)

<a id="35-left-recursion-growth-without-allocation"></a>
- [35. Left-recursion growth without allocation](optimizations/035-left-recursion-growth-without-allocation.md)

<a id="36-comparing-texts-without-boxing-them"></a>
- [36. Comparing texts without boxing them](optimizations/036-comparing-texts-without-boxing-them.md)

<a id="37-no-intermediate-lists-in-concat"></a>
- [37. No intermediate lists in `concat`](optimizations/037-no-intermediate-lists-in-concat.md)

<a id="38-calling-plain-rules-directly-from-closure-call-sites"></a>
- [38. Calling plain rules directly from closure call sites](optimizations/038-calling-plain-rules-directly-from-closure-call-sites.md)

<a id="39-a-line-table-for-error-positions"></a>
- [39. A line table for error positions](optimizations/039-a-line-table-for-error-positions.md)

<a id="40-one-pass-to-prepare-code-point-input-in-generated-parsers"></a>
- [40. One pass to prepare code-point input in generated parsers](optimizations/040-one-pass-to-prepare-code-point-input-in-generated-parsers.md)

<a id="41-building-the-offset-table-while-decoding-for-full-parses"></a>
- [41. Building the offset table while decoding, for full parses](optimizations/041-building-the-offset-table-while-decoding-for-full-parses.md)

<a id="42-generated-calls-of-plain-rules"></a>
- [42. Generated calls of plain rules](optimizations/042-generated-calls-of-plain-rules.md)

<a id="43-direct-calls-of-plain-rules-in-the-vms"></a>
- [43. Direct calls of plain rules in the VMs](optimizations/043-direct-calls-of-plain-rules-in-the-vms.md)

<a id="44-changes-36-and-37-in-the-vms-instruction-set-2"></a>
- [44. Changes 36 and 37 in the VMs (instruction set 2)](optimizations/044-changes-36-and-37-in-the-vms-instruction-set-2.md)

<a id="45-moving-reused-trees-in-place-after-document-edits"></a>
- [45. Moving reused trees in place after document edits](optimizations/045-moving-reused-trees-in-place-after-document-edits.md)

<a id="46-direct-calls-of-plain-rules-in-generated-parsers"></a>
- [46. Direct calls of plain rules in generated parsers](optimizations/046-direct-calls-of-plain-rules-in-generated-parsers.md)

<a id="47-recognition-in-generated-parsers"></a>
- [47. Recognition in generated parsers](optimizations/047-recognition-in-generated-parsers.md)

<a id="48-a-typed-runtime-for-parseast"></a>
- [48. A typed runtime for ParseAST](optimizations/048-a-typed-runtime-for-parseast.md)

<a id="49-first-character-dispatch-in-generated-choices"></a>
- [49. First-character dispatch in generated choices](optimizations/049-first-character-dispatch-in-generated-choices.md)

<a id="50-first-character-dispatch-in-the-closure-backend"></a>
- [50. First-character dispatch in the closure backend](optimizations/050-first-character-dispatch-in-the-closure-backend.md)

<a id="51-first-character-dispatch-in-the-vms-instruction-set-3"></a>
- [51. First-character dispatch in the VMs (instruction set 3)](optimizations/051-first-character-dispatch-in-the-vms-instruction-set-3.md)

<a id="52-projected-repetitions-in-generated-parse"></a>
- [52. Projected repetitions in generated `Parse`](optimizations/052-projected-repetitions-in-generated-parse.md)

<a id="53-projected-repetitions-in-the-engine-and-the-vms"></a>
- [53. Projected repetitions in the engine and the VMs](optimizations/053-projected-repetitions-in-the-engine-and-the-vms.md)

<a id="54-resuming-long-repetitions-in-a-document"></a>
- [54. Resuming long repetitions in a `Document`](optimizations/054-resuming-long-repetitions-in-a-document.md)

<a id="55-tracing-hook-on-the-call-path"></a>
- [55. Tracing hook on the call path](optimizations/055-tracing-hook-on-the-call-path.md)

<a id="56-applying-a-document-edit-to-memo-entries-when-they-are-looked-up"></a>
- [56. Applying a `Document` edit to memo entries when they are looked up](optimizations/056-applying-a-document-edit-to-memo-entries-when-they-are-looked-up.md)

<a id="57-pooling-the-scratch-memory-of-whole-input-parses"></a>
- [57. Pooling the scratch memory of whole-input parses](optimizations/057-pooling-the-scratch-memory-of-whole-input-parses.md)

<a id="58-pooling-the-scratch-memory-of-generated-parse-and-recognize"></a>
- [58. Pooling the scratch memory of generated `Parse` and `Recognize`](optimizations/058-pooling-the-scratch-memory-of-generated-parse-and-recognize.md)

<a id="59-resuming-long-repetitions-in-the-vms"></a>
- [59. Resuming long repetitions in the VMs](optimizations/059-resuming-long-repetitions-in-the-vms.md)

<a id="60-direct-rules-in-the-typed-runtime"></a>
- [60. Direct rules in the typed runtime](optimizations/060-direct-rules-in-the-typed-runtime.md)

<a id="61-reading-code-points-directly-in-direct-rules"></a>
- [61. Reading code points directly in direct rules](optimizations/061-reading-code-points-directly-in-direct-rules.md)

<a id="62-comparing-short-literals-in-place-in-direct-rules"></a>
- [62. Comparing short literals in place in direct rules](optimizations/062-comparing-short-literals-in-place-in-direct-rules.md)

<a id="63-a-smaller-node"></a>
- [63. A smaller `Node`](optimizations/063-a-smaller-node.md)

<a id="64-a-smaller-node-in-generated-parsers"></a>
- [64. A smaller `Node` in generated parsers](optimizations/064-a-smaller-node-in-generated-parsers.md)

<a id="65-position-conversion-in-the-language-server"></a>
- [65. Position conversion in the language server](optimizations/065-position-conversion-in-the-language-server.md)

<a id="66-no-memo-key-allocated-for-variables-where-none-is-defined"></a>
- [66. No memo key allocated for variables where none is defined](optimizations/066-no-memo-key-allocated-for-variables-where-none-is-defined.md)

<a id="67-retire-memo-entries-when-their-lookup-slots-are-removed"></a>
- [67. Retire memo entries when their lookup slots are removed](optimizations/067-retire-memo-entries-when-their-lookup-slots-are-removed.md)

<a id="68-release-construction-tracking-when-an-action-returns-nil"></a>
- [68. Release construction tracking when an action returns nil](optimizations/068-release-construction-tracking-when-an-action-returns-nil.md)

<a id="69-bound-persistent-variable-binding-histories"></a>
- [69. Bound persistent variable binding histories](optimizations/069-bound-persistent-variable-binding-histories.md)

<a id="70-keep-repetition-records-across-unchanged-document-parses"></a>
- [70. Keep repetition records across unchanged Document parses](optimizations/070-keep-repetition-records-across-unchanged-document-parses.md)

<a id="71-dependency-propagation-for-sample-constructor-analysis"></a>
- [71. Dependency propagation for sample constructor analysis](optimizations/071-dependency-propagation-for-sample-constructor-analysis.md)

<a id="72-reuse-structurally-equal-duplicate-capture-types"></a>
- [72. Reuse structurally equal duplicate capture types](optimizations/072-reuse-structurally-equal-duplicate-capture-types.md)

<a id="73-dependency-component-propagation-of-variable-reads"></a>
- [73. Dependency-component propagation of variable reads](optimizations/073-dependency-component-propagation-of-variable-reads.md)

<a id="74-smaller-first-chunks-for-generated-typed-values"></a>
- [74. Smaller first chunks for generated typed values](optimizations/074-smaller-first-chunks-for-generated-typed-values.md)

<a id="75-exclude-absent-expectations-before-scanning"></a>
- [75. Exclude absent expectations before scanning](optimizations/075-exclude-absent-expectations-before-scanning.md)

<a id="76-inline-value-free-plain-generated-go-rules"></a>
- [76. Inline value-free plain generated Go rules](optimizations/076-inline-value-free-plain-generated-go-rules.md)

## Experiments that did not pay off

The historical experiment table is in the [optimization catalog](optimizations/README.md#experiments-that-did-not-pay-off).

## Grammar authoring guidelines for performance

Practical grammar-level advice is in the [optimization catalog](optimizations/README.md#grammar-authoring-guidelines-for-performance).
