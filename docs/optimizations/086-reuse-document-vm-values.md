# 86. Reuse the Document VM value stack

**Status:** Candidate; implemented on a development branch, awaiting integration.

Measured 2026-10-11 on Go 1.27.1, darwin/arm64, Apple M3 Max (16 CPUs).

Repeated `Document.Parse` calls already reuse memoized results, but VM rule
evaluations allocated a fresh `[]any` value stack on every parse. Documents now
retain a cleared stack backing array between parses for the recursive and
iterative bytecode VMs. This reduces repeated VM stack allocation without
changing ordinary whole-input parsing or the closure backend.

The stack is cleared after a parse that evaluated rules, including syntax
errors. A whole-root memo hit does not touch the stack and skips the redundant
clear. An interrupted parse discards it. Before and after a parse, the cache is
dropped if `cap(vals)/2 > max(512, input positions)`; division avoids native
integer overflow. This bounds retained capacity in relation to the current
input while allowing small inputs with a large fixed stack. A nullable grammar
may exceed that bound and lose a useful cache, but parsing behavior is
unchanged. The cache may retain capacity proportional to a large current
input. No peak or retained-heap reduction was measured.

This applies only to engine `Document` parsing with the recursive or iterative
bytecode VM. Closure documents do not retain the stack; ordinary `Parse`,
generated parsers and TypeScript are unchanged. The compile-time
`pego_reference_document_vm_values` tag restores fresh-stack allocation for
same-revision comparisons without a runtime selector. `Document` contains one
additional slice header (24 bytes on the measured 64-bit host and 12 bytes on
linux/386). Both optimized and reference variants include this header, so the
comparison does not measure its footprint or document-construction cost.
Oversized-cache trimming occurs at parse boundaries, not immediately on
`Edit`.

`TestDocumentVMValueStorage` compares both position units and all three
backends with fresh parsing through edits, syntax errors, whole-root memo hits,
trace panics, small-after-large input and retained result nodes. It checks that
completed parses clear stack slots, same-sized edits reuse the backing array,
small inputs discard oversized capacity, and aborts discard the cache.
`TestDocumentVMValueStorageForEmptyInput` covers nullable grammars that build
a large stack without consuming input. Optimized and reference builds also
pass focused linux/386 ownership, abort and nullable tests. The native full
suite and all parser modules pass, and regeneration leaves all 17 tracked
parser outputs unchanged.

Five alternating 300 ms pairs on Go 1.27.1, darwin/arm64, Apple M3 Max
(16 CPUs) compare the optimized build with the same-revision
`pego_reference_document_vm_values` build. Ratios are medians of paired
optimized/reference times; ranges show the minimum and maximum paired ratios.

`BenchmarkDocumentReparse` edits one byte in a 100,000-line document and
reparses; `redundant=true` also performs an unchanged parse outside the timer.
All VM cases resume 99,999 repetitions in both builds. The four closure
controls retain 11 allocations and about 1.146 MB/op; all timing ranges overlap
parity. VM time and allocation results are:

| Backend | Unit | Median ratio range across cases | B/op, reference → optimized | Allocs/op, reference → optimized |
|:--|:--|:--|--:|--:|
| Bytecode | CodePoints / Bytes | .582–.611; every paired range below 1 | 10.078 MB → 1.155 MB | 44 → 16 |
| Iterative VM | CodePoints / Bytes | .578–.596; every paired range below 1 | 10.079 MB → 1.156 MB | 52 → 24 |

Across the eight VM cases, paired ranges span .569–.622. Bytes/op falls about
88.5%; each VM case saves 28 allocations while preserving the resumed count.

The 50,000-record CSV `BenchmarkIncrementalLong` edits and reparses one
middle record. Closure is a control (.902–1.029 paired range, 23 allocations
in both builds). Bytecode is .880 [.841–.951], 7.306 MB → 2.915 MB and 66 →
41 allocations; iterative VM is .896 [.833–.941], 7.294 MB → 2.933 MB and
77 → 52 allocations. This benchmark is in `bench`, separate from the engine
benchmarks below.

`BenchmarkDocumentLeftRecursion` covers direct, indirect and nested left
recursion. All nine backend ranges overlap parity. VM cases reduce B/op by
about 7.7% and save 11 allocations; closure allocations and B/op are
unchanged. Fifteen shrink, shrink-with-edit and unchanged-root controls also
overlap parity. After shrinking to one line, the VM cache reports 64 or 128
stack bytes; shrink-with-edit VM cases save three or four allocations. The
unchanged-root memo hit performs no value-stack clear. These controls do not
show a broad parser speedup.

Reproduce the ownership tests and benchmark setup:

```sh
go test ./internal/engine -run '^TestDocumentVMValueStorage' -count=1
go test -tags pego_reference_document_vm_values ./internal/engine \
  -run '^TestDocumentVMValueStorage' -count=1
go test -c -o /tmp/document-vm-values.test ./internal/engine
go test -tags pego_reference_document_vm_values -c \
  -o /tmp/document-vm-values-reference.test ./internal/engine
(
  cd bench
  go test -c -o /tmp/document-vm-values-bench.test .
  go test -tags pego_reference_document_vm_values -c \
    -o /tmp/document-vm-values-reference-bench.test .
)
```

Run both prebuilt binaries in alternating order for five 300 ms pairs:

```sh
for pair in 1 2 3 4 5; do
  if [ "$((pair % 2))" -eq 1 ]; then
    first=document-vm-values-reference; second=document-vm-values
  else
    first=document-vm-values; second=document-vm-values-reference
  fi
  for variant in "$first" "$second"; do
    "/tmp/$variant.test" -test.run='^$' \
      -test.bench='^(BenchmarkDocumentReparse|BenchmarkDocumentLeftRecursion|BenchmarkDocumentVMValuesShrink|BenchmarkDocumentVMValuesShrinkEdits|BenchmarkDocumentVMValuesUnchanged)$' \
      -test.benchtime=300ms -test.benchmem
  done
  for variant in "$first" "$second"; do
    "/tmp/$variant-bench.test" -test.run='^$' \
      -test.bench='^BenchmarkIncrementalLong/(closure|bytecode|iterative)$' \
      -test.benchtime=300ms -test.benchmem
  done
done
```
