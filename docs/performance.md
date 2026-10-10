# Performance

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
Python and tiny JSON controls in entry 75 are outside this suite too. Entries 76 and 77 describe later focused
generated-Go measurements; this full-suite snapshot predates those changes. This run covers 131 cases in 459.013 seconds
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

See the [optimization catalog](optimizations/README.md).

## Experiments that did not pay off

The historical experiment table is in the [optimization catalog](optimizations/README.md#experiments-that-did-not-pay-off).

## Grammar authoring guidelines for performance

Practical grammar-level advice is in the [optimization catalog](optimizations/README.md#grammar-authoring-guidelines-for-performance).
