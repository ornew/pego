# 74. Smaller first chunks for generated typed values

- Previously, `astNew` allocated 256 values for the first struct or terminal
  of each type, and `astSlice` allocated 1,024 elements for the first small
  list. These allocations dominate tiny `ParseAST` inputs. Each type now
  starts with 8 values and doubles to 256; list chunks start at 64 and
  double to 1,024, satisfying each requested length. Lists over 256 elements
  remain exact-size allocations. The direct typed runtime and conversion
  after Node parsing share this allocator; ordinary `Parse`, `Recognize`
  and the engine backends do not change.
- Compare baseline `70511fa` against this allocator change on Go 1.27.1,
  Apple M3 Max, darwin/arm64, using identical accepted inputs and five
  alternating 300 ms pairs with no competing builds or tests. Medians:

  | Workload | Time before → after | B/op before → after | Allocs/op before → after |
  |:--|:--|:--|:--|
  | JSON, 29 bytes, CodePoints | 5.047 → 1.067 µs | 60,027 → 2,212 | 7 → 7 |
  | JSON, 29 bytes, Bytes | 5.307 → 1.143 µs | 60,013 → 2,213 | 7 → 7 |
  | CEL, all 27 policies (2,891 bytes) per operation | 528.034 → 265.460 µs | 3,213,388 → 433,589 | 283 → 311 |
  | JSON, about 256 KiB | 3.047 → 3.032 ms | 2,461,914 → 2,458,128 | 234 → 268 |
  | CEL, about 256 KiB | 19.803 → 19.243 ms | 6,688,103 → 6,702,389 | 537 → 632 |

  Tiny JSON takes 0.211/0.215× the baseline time and 0.037× its bytes,
  with unchanged allocation counts. The CEL policy aggregate takes 0.503×
  the time and 0.135× the bytes, adding 28 allocations. These three timing
  ranges do not overlap. Large JSON/CEL take 0.995/0.972× the time with
  overlapping ranges, adding 34/95 allocations. The CEL large control
  allocates about 14 KB more; no large-input speedup is established.
- The Unicode ownership fixture parses three lists of 1/24/257/1,025
  terminals and structs plus an empty list, in 14/152/1,550/6,158 input
  bytes. The four direct-construction time ratios are
  0.193/0.480/0.881/0.976×; only the largest ranges overlap. For conversion
  after Node parsing they are 0.501/0.757/0.980/1.052×; the last two ranges
  overlap. The largest conversion median rises 231.395 → 243.324 µs
  (+5.2%), despite reducing bytes 1,160,112 → 1,126,989. Both paths add
  0/7/8/8 allocations at these lengths. Record this cost alongside the
  small-input gains; range overlap is not a statistical equivalence test.
- Each owner grows from a 24-byte slice to a 32-byte slice-plus-size state
  on this architecture. The extra warm-up allocations are bounded by the
  maximum chunk sizes. Go's JSON compiler diagnostics still inline the
  `astNew` shape but not the larger `astSlice` shape; timings above include
  that tradeoff. Input length was rejected as a per-type sizing proxy
  because it overestimates rare types; the existing maximum list chunk
  preserves large-output allocation amortization.
- Generated direct/conversion tests cover both units, Unicode spans,
  value-pointer uniqueness, empty and optional fields, value-growth
  boundaries, partial list chunks and exact-size large lists. Lists retain
  capacity equal to length. Appending or parsing a later failing/small
  input leaves retained results unchanged. Returned arrays are never
  recycled or cleared. Smaller chunks do not eliminate discarded sibling
  reachability through a returned pointer; that remains a separate live
  heap problem. These figures measure cumulative allocation traffic,
  not retained heap.
- Reproduce JSON with
  `cd parsers/json && go test -run '^$' -bench 'Benchmark(SmallParseAST|ParseAST)$' -benchtime=300ms -count=1`
  and CEL with
  `cd parsers/cel && go test -run '^$' -bench 'Benchmark(PoliciesParseAST|LargeParseAST)$' -benchtime=300ms -count=1`.
  Use the final benchmark sources on both production versions. For the
  ownership workloads, generate both paths with
  `PEGO_TYPED_CHUNK_DIR=/tmp/pego-chunks go test ./internal/engine -run '^TestGeneratedTypedChunkOwnership$' -count=1`,
  using a Go overlay of baseline `internal/engine/gen_types.go` when building
  the parent. In each generated `false`/`true` directory, benchmark
  `BenchmarkChunkParseAST` with the same 300 ms flags. Alternate five pairs
  of the prebuilt binaries. Focused raw output remains local; a full-suite
  checkpoint follows this optimization.
- The subsequent full-suite checkpoint measures clean commit `5610082`:
  131 cases with three samples each, 457.810 seconds. Compared with the
  previous `98ef6db` snapshot, typed-workload median times range
  0.965–1.056× and bytes 0.967–1.010×. Allocation counts increase by
  34/12/38/12/12/88/81/7 for JSON, CSV, XML, Pratt, left recursion,
  minilang conversion, recovery conversion and outline, respectively.
  These separate full runs do not isolate a speedup or regression from
  this method; the alternating controls above remain the causal comparison.
  The current backend, recognition, incremental and stream analysis is
  recorded under [Where PEGO stands](../performance.md#where-pego-stands).
