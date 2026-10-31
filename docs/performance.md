# Performance

This document defines benchmark workloads and methods and analyzes the latest
full-suite results. The [optimization catalog](optimizations/README.md)
describes mechanisms and their evidence; commits provide chronology. Its
mechanism pages and backend table describe current applicability; numbered
records preserve scoped historical evidence. Same-source reference controls
are added incrementally under
[design 024](design/records/024-optimization-mechanism-controls.md).
Update the catalog in the same commit as an optimization. Correctness-only fixes do not belong in the catalog; record measured performance impact in their commit messages.

Keep a summary with the baseline commit, candidate change, workload, environment, sample method,
results, tradeoffs and reproduction command. Focused raw output is local working data and is not
committed by default; source commits and benchmark code allow reruns. Keep raw output in Git only
when it has exceptional long-term value that a summary and rerun cannot preserve. The existing
full-suite `bench/results.txt`, with `bench/results-ts.txt` and `bench/results-size.txt`, is the input to the generated
results document.

The measured results of every benchmark are in [benchmarks.md](benchmarks.md), which is generated from a run; this
document describes the benchmarks, analyzes the results, and records how they came about.

- [Where ZeroParse stands](#where-zeroparse-stands): an analysis of the latest results
- [How to measure](#how-to-measure): the benchmarks, their workloads and method, and tools for measuring a change
- [Optimization catalog](optimizations/README.md): mechanisms, applicability,
  numbered evidence records and experiments

## Where ZeroParse Stands

This analysis describes the generated full-suite snapshot in [benchmarks.md](benchmarks.md), measured on 2026-10-29 at commit `13b3d186` (Go 1.27.1, Apple M3 Max, darwin/arm64, 16 cores, `GOMAXPROCS=16`). It contains 253 benchmark cases, three samples per case, and the Go benchmarks took 892.334 seconds; the generated TypeScript timings and the parser sizes were captured in the same run. This is one revision's snapshot, not a before-and-after comparison; ratios below compare operations within this run and do not attribute changes to a particular mechanism.
Budget-control behavior and its generated-Go cost are described in
[design 033](design/records/033-cancellable-parses-and-budgets.md) and
[optimization record 096](optimizations/096-specialize-generated-go-method-helpers.md).
The snapshot measures active controls on JSON and XML (`BudgetControlModes`, `ControlledParse`); the ordinary parsing
benchmarks run with the controls inactive. Relative to the inactive mode on the three interpreters in both position
units, a cancelable context takes 1.45–1.68× the time, finite budget limits 1.54–1.79× and both together 1.54–1.79×, with
the same allocation counts. Generated Go with finite budget limits takes 1.30–1.47× its ordinary `Parse` time and
1.47–1.61× its ordinary (CodePoints) `ParseAST` time. Neither this snapshot nor these records imply deployed behavior.

**Backends.** Across all eight parsing workloads, generated Go Node parsing takes 0.44–0.61× closure time in CodePoints and 0.50–0.66× in Bytes. Recursive VM parsing takes 1.14–1.32× closure time in CodePoints and 1.13–1.35× in Bytes. Iterative VM parsing takes 1.33–1.76× closure time in CodePoints and 1.35–1.76× in Bytes. The slowest relative case is iterative Operators Rule arithmetic at 1.76× closure in both units. Closure remains the default (see the [Choosing a Backend](guide/backends.md)); the VMs serve loaded compiled grammars (a `.zpc` file holds one bytecode module of one mode and loads on the bytecode backends only) and, iteratively, deep nesting.

**Typed values.** `ParseAST` directly constructs values for JSON, CSV, XML, both calculator grammars and Outline. For those six direct workloads, it takes 0.69–0.98× generated CodePoints `Parse` time and allocates 0.27–0.41× its bytes. Minilang and Recovery use converted output: `ParseAST` takes 0.95× generated `Parse` time on both and allocates 0.28× and 0.30× its bytes, respectively. On JSON and XML, direct `ParseAST` takes 1.11× and 1.33× standard-library time, with 1.18× and 1.25× its allocated bytes. These operations return different structures and should not be treated as equivalent outputs. CSV constructs typed `File`, `Record` and `Field` values directly.

Generated typed-output liveness cleanup is not implemented. Its scope and measurement gates are in [design 034](design/records/034-typed-output-liveness.md); [record 094](optimizations/094-generated-typed-output-liveness.md) holds the measured candidate.
A separate generated-Go typed-allocation bootstrap and its focused measurements are recorded in [optimization record 098](optimizations/098-bootstrap-generated-typed-output-allocations.md); they are not part of this full-suite snapshot.

**Recognition.** Across all eight workloads in CodePoints, closure recognition takes 0.62–0.80× closure Node-parse time, while generated recognition takes 0.43–0.70× generated Node-parse time. Simple grammars can recognize with zero allocations; predicates that read values (XML tag names, Outline indentation), left-recursion growth and recovery still require values or state. Typed `ParseAST` takes 1.54× generated `Recognize` time on JSON and 1.56× on XML because it returns typed values.

**The standard library.** Where a comparable parser exists, standard-library parsing takes 1.62–6.76× less time than generated `Parse`: JSON, CSV, XML, and the two calculator workloads. The gap is widest for left-recursive arithmetic; the parsers produce different outputs. `json.Valid`, a hand-written state machine, takes 8.05× less time than generated `Recognize` in this snapshot.

**Generated TypeScript.** Under Node 24.13.0, generated `parse` takes 2.43–4.00× the time of generated Go `Parse` on the seven workloads that the TypeScript benchmark covers (Recovery is not measured). `parseAST` takes 1.12–1.43× `parse` time and `recognize` 0.31–0.72×. These are single-process means on the same inputs, not a comparison of equivalent outputs.

**Generated size.** A generated Go `parser.go` ranges from 25.3 KB (CSV) to 1.59 MB (DuckDB), and a binary that calls the parser's `ParseAST` from 2.15 to 6.45 MB, most of which is the ZeroParse runtime and the standard library. The TypeScript `parser.ts` ranges from 32.0 KB to 3.32 MB, which is 6.57–257 KB gzipped with `gzip -9`, plus a runtime of 146 KB (36.9 KB gzipped) that every parser shares.

**Grammar style.** The calculator workload takes 2.30× as long with left recursion as with Operators Rule parsing on closure (1.98× generated, 1.62× typed). The generated Recovery workload takes about the same time as Minilang, but skips broken statements and therefore performs different matching work.

**VM Parse Timing Recheck (2026-10-14).** Five alternating 1-second samples on Go 1.27.1 (Apple M3 Max, darwin/arm64, `GOMAXPROCS=16`, `GOGC=100`, `GOMEMLIMIT=off`) compared the historical grammar revisions `8177b32` and `296a2c1`. They are sibling commits with the same parent, so this ratio is not a chronological before/after. All eight VM medians were below parity, but paired ranges span 0.959–1.027; the earlier short-run slowdown was not reproduced. Allocation counts changed by at most one, with no broad memory change.

| Input | VM | Unit | `8177b32 / 296a2c1` median (paired range) |
|:--|:--|:--|--:|
| JSON | Bytecode | Bytes | 0.980 (0.975–1.014) |
| JSON | Bytecode | CodePoints | 0.976 (0.964–1.011) |
| JSON | Iterative | Bytes | 0.982 (0.959–1.027) |
| JSON | Iterative | CodePoints | 0.989 (0.981–0.991) |
| Operators Rule arithmetic | Bytecode | Bytes | 0.979 (0.973–0.989) |
| Operators Rule arithmetic | Bytecode | CodePoints | 0.982 (0.972–0.986) |
| Operators Rule arithmetic | Iterative | Bytes | 0.990 (0.987–1.005) |
| Operators Rule arithmetic | Iterative | CodePoints | 0.988 (0.961–0.992) |

The run covered four parser backends—closure, recursive VM, iterative VM and generated Go—two grammars and both position units (240 serialized rows). JSON inputs were 262,220 bytes / 255,295 runes (SHA-256 `1f66b46fe4b8009e7932f4f309d073835969cb34002c95a5ab29225254f00ddb`); Operators Rule arithmetic was 133,457 bytes/runes (SHA-256 `cb5fe1452f3f605804e5b32e9f20e9a266aafd9472c6c25053b52bcd8a62f0be`). Closure and generated-Go controls were included; historical runtime sources and the generated-Go controls matched across the sibling revisions. The JSON source differed by two redundant groups; source parsing and compilation were outside the timed loop, but the compiled grammar may still affect parsing, so the comparison does not isolate a runtime cause. On the eight VM profiles, current main `a1acd16` medians versus `296a2c1` ranged 0.998–1.027; this cumulative comparison does not isolate a later change. No runtime change or default-policy conclusion follows from this verification.

**Incremental parsing.** In this snapshot, a one-character edit and reparse of the 300-function Minilang document takes 116–133 µs across the three backends, compared with 15.6–24.5 ms for a fresh full parse. For the 50,000-record CSV document, edit and reparse take 1.76–1.80 ms and allocate 487–504 KB/op.

**Generated XML capture scratch.** An earlier focused comparison found slower
generated XML parsing and recognition despite lower allocated bytes. At main
commit `244be64`, five alternating 500 ms rounds against the original parent
measured 0.941–0.948× median time across Parse and Recognize, with lower
allocations and matching input and Node output. A same-source disabled control
also favored scratch in the medians. Intervening changes prevent attribution
of the parent comparison to scratch. [Record 092](optimizations/092-reuse-generated-node-capture-scratch.md)
preserves the earlier result, the paired ranges, and the measurement limits.

**Streaming.** In this run, reading and discarding 50,000 CSV records plus the header takes 78.5–99.0 ms and 22–28 MB/s. Cumulative allocation is 94.8–106 MB/op, or about 44–49 bytes per input byte. The ZeroParse streaming backends allocate about 0.16–0.21 objects per data record. Batch closure parsing of 5,000 records (211 KB) uses about 28 bytes per input byte and 0.06 allocations per record. These are cumulative allocation figures, not retained-heap measurements. The focused stream-storage measurements are described in [record 089](optimizations/089-isolate-stream-prefixes-and-batch-scratch-cleanup.md). The full snapshot does not isolate the effect of any individual change.

**Preparation.** A compiled grammar holds the module of one mode. Loading a `.zpc` file in this snapshot takes 0.15× (JSON) and 0.07× (Minilang) of source-compilation time in parse mode, 0.09× and 0.05× in recognize mode, and 0.16× and 0.08× in document mode. Loading runs on the bytecode backend and includes backend setup by one parse that the mode serves, with empty input. Focused allocation and timing measurements for attribute normalization are recorded in [optimization 095](optimizations/095-attribute-copy-on-change.md); this full snapshot does not attribute preparation results to that mechanism.

## How to Measure

The benchmarks are in `bench/`. `go run ./bench/report` runs all of them, measures the generated TypeScript parsers under Node and the sizes of the
generated parsers (about 20 minutes), and regenerates [benchmarks.md](benchmarks.md); see [the benchmark workloads](#benchmark-workloads) for what they measure.

```bash
# Full benchmark suite (all backends, both position units, stdlib counterparts), and the results document
go run ./bench/report

# Only the TypeScript timings and the sizes, keeping the saved Go results
go run ./bench/report -capture ts,size

# A single workload, with CPU and allocation profiles
go test ./bench -run '^$' -bench 'BenchmarkParse/JSON/closure/codepoints' -benchtime 20x \
    -cpuprofile cpu.out -memprofile mem.out -o bench.test
go tool pprof -top -nodecount 30 bench.test cpu.out
go tool pprof -sample_index=alloc_space -top -nodecount 30 bench.test mem.out
```

Saved benchmark rows used by the report must contain a positive iteration count and exactly one finite, positive
`ns/op` value plus finite, nonnegative `B/op` and `allocs/op` values. Zero allocation values are valid; a missing metric
is not treated as zero. Repeated rows for one benchmark must use the same metric set, and all benchmarks used by one
report must have the same number of samples. The sample count may be one for a complete single-run report. The
TypeScript results (`bench/results-ts.txt`) carry only the time and throughput of each run; their samples are counted
separately from the Go results.

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

`internal/gen/gogen/gogen_typed_output_retention_test.go` exports a generated fixture
and checks escaped terminal, string-node, shared-structure and returned-list values
in both position units. It exports `slabs`, `legacy`, `objects`, `slices` and
`individual` subdirectories. `slabs` uses the first-three-object/first-nonempty-
list bootstrap followed by slabs; `legacy` disables that bootstrap while
retaining the 8-value and 64-element slab policy. The other controls disable
object slabs, list slabs or both at generation time. Clone reachable text
before dropping the parser and input so legitimate input-string retention
does not count as slab retention. See [record 098](optimizations/098-bootstrap-generated-typed-output-allocations.md)
for the focused results and limits.

```bash
ZP_TYPED_OUTPUT_RETENTION_DIR=/tmp/typed-output-retention \
  go test ./internal/gen/gogen -run '^TestGeneratedTypedOutputRetentionFixture$' -count=1
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

The `match` fixture discards a large public string node (`TNode[string]`)
before returning a small one from the same typed-runtime string-node block.
Object/list reference controls do not change that block: treat string-node
ownership as a separate domain when evaluating a cleanup strategy. Clone the
returned string-node text just like terminal text, and retain the manual
string-node control in both position units.

### Benchmark Workloads

`TestWorkloads` in `bench/` checks, on reduced inputs, that every input can be parsed by every backend and that the
ZeroParse backends return the same results.

- Inputs are generated by `internal/bench/inputs` from a fixed random seed, so they are identical across runs. They include
  multi-byte characters such as Japanese text.
- The grammars are taken unchanged from `parsers/` (JSON, CSV, XML) and `examples/` (the calculator in its Operators Rule and
  left-recursive forms, minilang, and outline). The generated parsers in `internal/bench/gen/` are generated from them (`go generate ./bench`);
  the `Document` and stream parsers are separate packages, so the batch parsers keep their generated form.
- Each benchmark runs three times and the median is reported.
- Comparisons with the standard library measure different outputs. Every ZeroParse backend builds a tree whose nodes carry
  positions (start and end) and rule names, and records the expectations at the farthest failure. The standard-library
  parsers produce:
  - `encoding/json`: decoding into `any` (no positions).
  - `encoding/csv`: `ReadAll` (a list of field strings with quotes removed).
  - `encoding/xml`: reading tokens with `Decoder.Token` (no tree).
  - `go/parser.ParseExpr`: a Go expression AST (with positions).

| Workload | Grammar | Input | Main focus |
|:--|:--|:--|:--|
| JSON | `parsers/json` | Array of objects, 262 KB | Lexical repetition, AST of join types |
| CSV | `parsers/csv` | 5,000 rows (211 KB); fields with quotes, commas and newlines | Simple repetition, many terminals |
| XML | `parsers/xml` | Nesting, attributes, character references, comments, CDATA; 262 KB | Nesting, a predicate comparing start-tag and end-tag names |
| Arith_OperatorsRule | `examples/calculator/calc.zp` | Expression with 20,000 terms (133 KB), parentheses nested up to depth 40 | Operators Rule loop and longest match |
| Arith_LeftRec | `examples/calculator/calc_lr.zp` | Same as above | Left-recursion seed growing |
| Minilang | `examples/minilang` | 300 functions (89 KB) | PEG statements nested with Operators Rule expressions, keyword exclusion |
| Recovery | `examples/minilang` | Same as above, with one line in seven broken | `#recover`, expectation recording |
| Outline | `examples/outline` | 5,000 lines (83 KB), depth up to 10 | Predicates and variables (non-memoized rules) |
| Incremental | `examples/minilang` | Repeated one-character insertions and deletions in 300 functions | Memo reuse with `Document`, on every backend and a parser generated with `-document` (compared with a full parse each time) |
| Incremental, long | `parsers/csv` | Repeated one-character insertions and deletions in the middle of 50,000 records | Resuming long repetitions with `Document`, on every backend and a parser generated with `-document` |
| Stream | `parsers/csv` | 50,000 rows (2.2 MB) | Reading and discarding with `ParseStream`, on every backend and a parser generated with `-stream` |
| Prepare | JSON, minilang | None | Compiling from source and loading `.zpc` (in each mode) |

The 2026-10-09 run and the current analysis use the ready-made grammars in `parsers/csv` and `parsers/xml`.
Older runs used the simpler grammars of `examples/csv` and `examples/xml`, since removed. Their figures do not
isolate the effect of a runtime change across that grammar transition; use the paired measurements in each tuning entry.

The numbered records preserve evidence for individual changes; commits provide their chronology. Records before 13
were measured on a 4-vCPU Intel Xeon virtual machine and are several times slower in absolute terms.

The first benchmark run on that virtual machine took about 510 ms for JSON and allocated 206 MB in 2.6 million
allocations.

### Generated TypeScript

Generated TypeScript parsers are measured with Node (24 or later) by running a generated `parser.ts` under
`node --experimental-strip-types`: `bench/ts-parse.mjs` for parsing a file with `parse`, `parseAST` or `recognize` (the
entry points that [benchmarks.md](benchmarks.md) reports, captured by `go run ./bench/report`), `bench/ts-typed.mjs` for the typed
workloads that `TestGeneratedTSTypedWorkloads` and `TestGeneratedTSTypedSharedDAG` write when
`ZP_TS_TYPED_DIR` is set, and `bench/ts-literal.mjs` for literal matching. Compare two generators by
alternating them in each round, run at least six rounds, and compare medians and ranges. The form without
`-budget-controls` is the default, so compare it with the controlled form separately. Results are in
[record 122](optimizations/122-typescript-from-flow-ir.md).

## Mechanism Catalog

See the [mechanism catalog](optimizations/README.md#mechanism-index) for
stable IDs, backend applicability, default status, controls and evidence links.

## Change Log

See the [optimization catalog](optimizations/README.md).

## Experiments That Did Not Pay Off

The historical experiment table is in the [optimization catalog](optimizations/README.md#experiments-that-did-not-pay-off).

## Grammar Authoring Guidelines for Performance

Practical grammar-level advice is in the [optimization catalog](optimizations/README.md#grammar-authoring-guidelines-for-performance).
