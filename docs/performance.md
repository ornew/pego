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

## Where PEGO Stands

This analysis describes the clean full-suite snapshot in [benchmarks.md](benchmarks.md), measured on 2026-10-13 at commit `c73bda3` (Go 1.27.1, Apple M3 Max, darwin/arm64, 16 cores, `GOMAXPROCS=16`). It covers 131 cases, three samples per case, in 460.078 seconds. The snapshot includes the sparse deferred-memo candidate; the same-run ratios below are unpaired and cannot attribute changes to that mechanism. The focused series-baseline comparison is recorded separately in [the sparse deferred-memo record](optimizations/087-sparse-deferred-memo-seen-pages.md).

**Backends.** Generated Go Node parsing takes 0.40–0.58× closure time in CodePoints and 0.46–0.62× in Bytes. Recursive VM/closure time ranges are 1.09–1.37× in CodePoints and 1.11–1.36× in Bytes; iterative VM/closure ranges are 1.15–1.87× and 1.20–1.87×. Iterative Operators Rule arithmetic is the slowest relative case at 1.87× closure in both units. Closure remains the default (see the [runtime guide](guide/runtime.md#backends)); the VMs serve grammars without their AST and, iteratively, deep nesting.

**Typed values.** For direct Go values (`ParseAST` for JSON, CSV, XML, both calculators and Outline), unpaired `ParseAST`/generated Node ratios are 0.66–0.92× time, 0.28–0.43× allocated bytes and 0.52–0.89× allocations. The Operators Rule calculator takes 0.92× generated `Parse` time; CSV returns its dedicated typed AST. On JSON and XML, `ParseAST` takes 0.84× and 0.88× the standard-library time, with 1.10× and 1.18× its bytes and about 201× and 280× fewer allocations, while recording positions and error expectations. Minilang and Recovery convert CST nodes: `ParseAST` takes 1.05–1.06× generated `Parse` time and 1.40–1.42× allocated bytes.

**Recognition.** Closure recognition/full Node parsing takes 0.53–0.76× time; generated recognition/Node parsing takes 0.38–0.68×. Simple grammars can recognize with zero allocations; predicates that read values (XML tag names, Outline indentation), left-recursion growth and recovery still require values or state. Typed `ParseAST` takes 1.59× generated `Recognize` time on JSON and 1.64× on XML; these paths return different results because `ParseAST` builds typed values.

**The standard library** takes 1.25–5.38× less time than generated `Parse` where a comparable parser exists. The gap is widest for left recursion and CSV, which produce different outputs. `json.Valid`, a hand-written state machine, takes 5.52× less time than generated `Recognize` in this snapshot.

**Grammar style.** The calculator workload takes 2.49× as long with left recursion as with Operators Rule parsing on the closure backend (2.04× generated, 1.67× typed). The generated Recovery workload takes about the same time as Minilang, but skips broken statements and therefore performs different matching work.

**Incremental parsing.** An edit and reparse of the 300-function Minilang document takes 129–143 µs across the three backends, compared with 12.0–20.9 ms for a full parse. For the 50,000-record CSV document, edit and reparse take 2.20–2.27 ms and allocate 2.70–2.72 MB/op.

**Generated XML capture scratch.** A focused paired comparison against the exact parent found higher time despite lower allocated bytes: generated Node `Parse` was 1.254× slower in CodePoints and 1.394× in Bytes, and `Recognize` was 1.380× slower. Against a same-source disabled reference, scratch took 0.966×/0.976×/0.922× time; both builds remained slower than the exact parent. With `GOGC=off`, five fixed-iteration pairs overlapped parity, so the cost is GC-sensitive. See [record 092](optimizations/092-reuse-generated-node-capture-scratch.md) for the scoped evidence.

**Streaming.** Reading and discarding 50,000 CSV records plus the header takes 67.6–81.0 ms and 26.8–32.0 MB/s. Cumulative allocation is 89.4–98.0 MB/op, or about 41.2–45.2 bytes per input byte. The PEGO streaming backends allocate about 0.16–0.20 objects per data record. Batch closure parsing of 5,000 records (211 KB) uses about 26 bytes per input byte and 0.06 allocations per record. These are cumulative allocation figures, not retained-heap measurements. The focused stream-storage measurements are described in [record 089](optimizations/089-isolate-stream-prefixes-and-batch-scratch-cleanup.md). The full snapshot does not isolate the effect of any individual change.

**Preparation.** Loading a `.pegoc` file takes 0.43–0.55× source-compilation time for the closure backend, 0.53–0.70× for bytecode and 0.18–0.27× without the AST. Loading includes backend setup by one parse of empty input.

## How to Measure

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

### Generated Typed-Output Retention

`internal/engine/gen_typed_output_retention_test.go` exports a generated fixture
and checks escaped terminal, Match, shared-structure and returned-list values
in both position units. It exports `slabs`, `objects`, `slices` and `individual`
subdirectories; the latter three disable object slabs, list slabs or both at
generation time. The default generated parser is unchanged. Clone reachable text before dropping the parser and input so
legitimate input-string retention does not count as slab retention.

```bash
PEGO_TYPED_OUTPUT_RETENTION_DIR=/tmp/typed-output-retention \
  go test ./internal/engine -run '^TestGeneratedTypedOutputRetentionFixture$' -count=1
cd /tmp/typed-output-retention/slabs
go test -c -o typed-retention.test .
./typed-retention.test -test.run '^$' -test.bench '^BenchmarkTypedOutputParse$' -test.benchmem -test.benchtime=500ms
./typed-retention.test -test.run '^$' -test.bench '^BenchmarkTypedOutputRetainedHeap$' -test.benchtime=1x
```

The throughput benchmark prebuilds each input and parses without cloning.
For retained-heap comparisons, alternate parser and manual-control
processes for each shape and unit across five fresh-process pairs. The heap
diagnostic clones text, warms both variants, drops the parser/input owners and
forces three GCs before reporting the last escaped result's live-heap delta.
This is retained heap, not peak or cumulative allocation.

The `structure` fixture discards a large scalar field in the same object
chunk as the returned structure. The `slice` fixture fills the first object
chunk before producing returned elements; its distinct large/small holder
types isolate retention through unused list backing cells. Compile the same
test binary in each exported subdirectory and alternate all four variants
to measure the mechanisms independently. Reference controls preserve values,
sharing, spans, non-nil empty lists and append isolation; they are internal
measurement tools rather than public parser options.

The `match` fixture discards a large public Match before returning a small
one from the same typed-runtime Match block. Object/list reference controls
do not change that block: treat Match ownership as a separate domain when
evaluating a cleanup strategy. Clone the returned Match text just like
terminal text, and retain the manual Match control in both position units.

### Benchmark Workloads

`TestWorkloads` in `bench/` checks, on reduced inputs, that every input can be parsed by every backend and that the
PEGO backends return the same results.

- Inputs are generated by `bench/inputs.go` from a fixed random seed, so they are identical across runs. They include
  multi-byte characters such as Japanese text.
- The grammars are taken unchanged from `parsers/` (JSON, CSV, XML) and `examples/` (the calculator in its Operators Rule and
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
| Arith_OperatorsRule | `examples/calculator/calc.pego` | Expression with 20,000 terms (133 KB), parentheses nested up to depth 40 | Operators Rule loop and longest match |
| Arith_LeftRec | `examples/calculator/calc_lr.pego` | Same as above | Left-recursion seed growing |
| Minilang | `examples/minilang` | 300 functions (89 KB) | PEG statements nested with Operators Rule expressions, keyword exclusion |
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

## Where Each Optimization Applies

See the [applicability table and implementation notes](optimizations/README.md#where-each-optimization-applies).

## Change Log

See the [optimization catalog](optimizations/README.md).

## Experiments That Did Not Pay Off

The historical experiment table is in the [optimization catalog](optimizations/README.md#experiments-that-did-not-pay-off).

## Grammar Authoring Guidelines for Performance

Practical grammar-level advice is in the [optimization catalog](optimizations/README.md#grammar-authoring-guidelines-for-performance).
