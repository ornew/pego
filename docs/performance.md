# Performance

This document defines benchmark workloads and methods and analyzes the latest
full-suite results. The [optimization catalog](optimizations/README.md)
describes mechanisms and their evidence; commits provide chronology. Its
numbered records and applicability table remain a partial view until the
mechanism-inventory proposal in
[design 024](design/024-optimization-mechanism-controls.md) is implemented.
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
- [Optimization catalog](optimizations/README.md): mechanisms, applicability,
  numbered evidence records and experiments

## Where PEGO stands

An analysis of the results in [benchmarks.md](benchmarks.md) measured on
2026-10-11 at clean source commit `04fe467` (Apple M3 Max, Go 1.27.1,
`darwin/arm64`, 16 cores). The figures below summarize that run; compare the
measured commit against [benchmarks.md](benchmarks.md) before relying on them.

This clean-checkout snapshot covers 131 cases, with three samples per case, in
459.086 seconds at default `GOMAXPROCS=16`. It includes the current optimization
set but does not isolate any one change. The focused paired measurements in
the [catalog](optimizations/README.md) use narrower controls for attribution.
Unless labeled Bytes, ratios below use CodePoints. Typed-value and
standard-library comparisons also use CodePoints. Preparation differences in
this snapshot are unpaired. Focused comparisons did not establish a persistent
material slowdown; see the durations, controls and small allocation costs in
[the stream storage measurements](optimizations/089-isolate-stream-prefixes-and-batch-scratch-cleanup.md#alternatives-and-measurements).

**Backends.** Generated Go Node parsing takes 0.44–0.64× closure time across
both position units. The recursive VM takes 1.08–1.42× closure time, and the
iterative VM takes 1.18–1.91×. The iterative VM is slowest on Pratt expressions
(1.91× in Bytes), where it runs a state machine on its own stack. The closure
backend remains the default (see the [runtime guide](guide/runtime.md#backends));
the VMs are for grammars without their AST and, iteratively, for deep nesting.

**Typed values.** For grammars whose values have direct Go types (`ParseAST` for
JSON, CSV, XML, both calculators and outline), time is 0.61–0.87× generated
`Parse` time, memory is 0.23–0.35×, and allocations are 0.30–0.85×. The Pratt
calculator takes 0.87× generated `Parse` time; CSV returns its dedicated typed
AST. On JSON and XML, `ParseAST` takes 0.78× and 0.89× the corresponding
standard-library time, with 1.10× and 1.18× its bytes and about 201× and 280×
fewer allocations, while recording positions and error expectations. CSV
`ParseAST` takes 2.30× the standard-library time and 1.45× its bytes;
`encoding/csv` builds no tree. Minilang and Recovery include CST nodes and
convert the Node tree: `ParseAST` takes 1.05× and 1.08× generated `Parse` time,
respectively, and about 1.26× its memory.

**Allocations.** PEGO makes about 300–2,400 allocations per Node parse, against
roughly 10,000–80,000 for the standard-library parsers. Nodes, lists and fields
come from slabs, and the memo, input buffers and stacks are reused across
parses. The allocated bytes remain substantial: closure-backend Node parses
use about 26–130 bytes per input byte, with left recursion highest because it
grows intermediate results.

**Recognition.** `RecognizeOnly` takes 0.61–0.74× full-parse time on the
closure backend. Generated `Recognize` takes 0.37–0.64× generated Node `Parse`
time. Simple grammars can recognize with zero allocations; predicates that read
values (XML tag names, outline indentation), left-recursion growth and recovery
still require values or state. Typed `ParseAST` takes 1.66× the generated
`Recognize` time on JSON, 1.58× on XML and 1.22× on the left-recursive
calculator; these paths return different results because `ParseAST` builds
typed values.

**The standard library** takes 1.3–5.5× less time than generated `Parse` where a
comparable parser exists. The gap is widest for left recursion (5.5× against
`go/parser`'s hand-written precedence climbing) and CSV (2.9×;
`encoding/csv` builds no tree). `json.Valid`, a hand-written state machine,
takes 5.4× less time than generated `Recognize` here. Standard-library
allocation counts are about 33–126× higher on these workloads.

**Grammar style.** The calculator workload takes 2.5× as long with left
recursion as with Pratt parsing on the closure backend (2.0× generated, 1.6×
typed), consistent with growing a seed at every level. Generated Recovery and
Minilang take similar time, but Recovery skips broken statements and therefore
performs different matching work.

**Position units** vary by workload and backend; the tables report both so
users can choose based on the positions their application needs.

**Incremental parsing.** An edit and reparse of the 300-function Minilang
document takes 130–148 µs across the three backends, compared with 12.4–20.9 ms
for a full parse. For the 50,000-record CSV document, the edit and reparse take
2.36–2.45 ms and allocate 2.70–2.73 MB across all three backends.

**Streaming** 50,000 CSV records (2.17 MB) takes 68.9–84.4 ms, with 26–31 MB/s
throughput. It cumulatively allocates 41–45 bytes per input byte and about
0.16–0.20 allocations per record, against 26 bytes per input byte and 0.06
allocations per record for closure batch parsing of 5,000 records (211 KB).
These are cumulative allocations, not retained-heap measurements; separate
long-stream tests check retained state and heap growth. Streaming and batch
use different input sizes, and per-element scratch still causes more
allocations than batch reuse.

**Preparation.** Loading a `.pegoc` file takes 0.45–0.49× source-compilation
time for the closure backend, 0.53–0.71× for bytecode and 0.20–0.26× for a
file without the AST.

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

The numbered records preserve evidence for individual changes; commits provide their chronology. Records before 13
were measured on a 4-vCPU Intel Xeon virtual machine and are several times slower in absolute terms.

The first benchmark run on that virtual machine took about 510 ms for JSON and allocated 206 MB in 2.6 million
allocations.

## Where each optimization applies

See the [applicability table and implementation notes](optimizations/README.md#where-each-optimization-applies).

## Change log

See the [optimization catalog](optimizations/README.md).

## Experiments that did not pay off

The historical experiment table is in the [optimization catalog](optimizations/README.md#experiments-that-did-not-pay-off).

## Grammar authoring guidelines for performance

Practical grammar-level advice is in the [optimization catalog](optimizations/README.md#grammar-authoring-guidelines-for-performance).
