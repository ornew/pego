# 77. Inline value-building and memoized Node expressions in generated Go parsers

- The generated Go Node runtime now inlines supported value-building and
  memoized expression bodies, extending the value-free plain-rule path in
  change 76. Captures, actions, predicates, scoped repeats and projections
  can use the direct emitter. Typed generation and TypeScript are unchanged.
- Existing `call`, `invoke`, `invokePlain` and `finish` wrappers retain
  ownership of depth accounting, frames, trail/environment rollback,
  actions, recovery, memoization and node naming. Unsupported local cut or
  recovery, Pratt expressions, left-recursion leaders and unresolved capture
  references keep general dispatch; direct rules may call fallback rules.
- Generated Node trees, errors and recognition match the engine across both
  units. Tests cover memoized action/repeat frames, expectation fallback,
  projections, predicates, retained saved trees, external `ParseRule` and
  route selection.
- Focused measurements: Go 1.27.1, darwin/arm64, Apple M3 Max, 16 CPUs;
  baseline `b3f00b5`; five alternating 300 ms pairs for 32 Node/recognition
  conditions and eight `ParseAST` controls. Ratios are candidate/baseline
  ratios within each pair, summarized by their median. JSON/CSV/XML/Outline
  Node median ratios range 0.817–0.937×. Pratt Node median ratios are
  1.010×/1.032×, but paired ranges include 1 and raw time ranges overlap.
  Recognition paired ranges stay below 1 for JSON CodePoints (median 0.945×),
  Minilang CodePoints/Bytes (0.981×/0.966×), Recovery (0.958×/0.967×), XML
  CodePoints (0.860×) and Outline (0.824×/0.902×); many remaining conditions
  include parity.
  ParseAST direct controls show no systematic change. Node-conversion
  controls save about 188 KB/six allocations for Minilang and 273 KB/six
  allocations for Recovery in this schedule; this is Node-construction
  propagation, not typed-emitter adoption or retained-heap evidence. These
  focused results do not replace the pending full-suite checkpoint.
- Three paired isolated compiler runs (`go tool compile -pack`, shared
  standard-library export files) measured median candidate/baseline ratios
  JSON 0.967×, XML 0.743×, Minilang 0.886× and DuckDB 0.343×; JSON ranges
  overlap. Across all 17 standalone generated Go parser sources, size falls
  0.45–17.96%. For the four compiler samples, source falls 3.1–9.1% and
  compiler archive size falls 13.9–37.0%. This is compiler work and archive
  size, not linked executable size or a cold build.
- A separate synthetic memoized-action grammar with 128 records measures
  median paired ratios of 0.932× for CodePoints and 0.981× for Bytes over
  five alternating 300 ms pairs; raw Bytes time ranges overlap. The grammar
  parses `a-1234567?;` records: the first ordered-choice
  branch calls a memoized pair rule then fails on `!`, and the second branch
  reuses it before `?;`. Trees/errors match; bytes and allocations per
  operation are unchanged (about 90.7 KB and 8 allocations).
  Use this fixture:

  ```pego
  type Pair struct { Left Match, Right Match }
  type Doc struct { Items []Pair }
  def main: Doc = items:row+ $$ -> new Doc{Items: $items}
  def row = p:pair "!" ";" / p:pair "?" ";" -> $p
  def pair: Pair = left:@(?a-z) "-" right:number -> new Pair{Left: $left, Right: $right}
  def number = @(?0-9)+
  ```

  Run this benchmark from the generated parser package, once for each unit:

  ```go
  func BenchmarkMemoAction(b *testing.B) {
      input := strings.Repeat("a-1234567?;", 128)
      for _, tc := range []struct { name string; unit Unit }{
          {"CodePoints", CodePoints}, {"Bytes", Bytes},
      } {
          b.Run(tc.name, func(b *testing.B) {
              b.SetBytes(int64(len(input)))
              b.ReportAllocs()
              for b.Loop() {
                  if _, err := Parse(input, tc.unit); err != nil { b.Fatal(err) }
              }
          })
      }
  }
  ```

  Compare generated outputs and errors for successful and malformed records
  against the baseline parser before timing.
- Reproduce the public controls with
  `go test ./bench -run '^$' -bench '^BenchmarkParse$/./^generated$/(codepoints|bytes)$' -benchtime=300ms -count=1`,
  `go test ./bench -run '^$' -bench '^BenchmarkRecognize$/./^generated$' -benchtime=300ms -count=1`
  and
  `go test ./bench -run '^$' -bench '^BenchmarkParse$/./^generated_ast$' -benchtime=300ms -count=1`.
  The public Recognize benchmark uses its default unit; reproduce Bytes by
  invoking `Recognize(input, Bytes)` for the same workloads. Alternate five
  pairs against baseline `b3f00b5` with the same grammars, generation options
  and benchmark harness, using each revision's own generated outputs. For
  the public suite, `go generate ./bench` regenerates its parsers; prebuild
  each revision separately with `go test -c -o bench.test ./bench`, then
  alternate those binaries with the same selectors and `-test.benchmem`.
  For the memo case, generate the grammar above with
  `pego gen -g memo.pego -pkg memo -o parser.go`, then use the loop above.
