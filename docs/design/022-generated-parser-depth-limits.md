# 022. Configurable Depth Limits in Generated Parsers

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-10

## Summary

Make the maximum nested rule-call count configurable both when generating a standalone parser and for an
individual parsing invocation. Keep the existing default of 100,000 calls. The generation option becomes the
parser's default; a positive per-call override replaces it for that invocation only. Preserve calls without options while moving Go/typed Go entry points to functional options; keep TypeScript's existing
unit arguments and add options-aware entry points there.

The generation API is `pego.WithGeneratedMaxDepth(n)` and the CLI flag is `pego gen -max-depth n`.
Generated Go uses `Parse(input, opts ...ParseOption)`, `WithMaxDepth(n)` and `WithUnit(unit)`; TypeScript exposes
`ParseOptions` and `parseWithOptions`. The APIs and parser-local accounting described here are implemented.

## Motivation

At `bc4f820`, the generated Go parser's 100,000-call guard accepts Go source with 16,000 nested parentheses but
returns `nesting too deep: more than 100000 rule calls` at 17,000 and 30,000. Go 1.27.1's `go/parser` accepts all
three inputs. The exact input is `"package p; var x = " + strings.Repeat("(", n) + "1" + strings.Repeat(")", n)`.
Both generated `Recognize` and `ParseAST`, with byte positions, show this difference. An isolated
source overlay changing only the guard to 600,000 accepts all three inputs in both entry points. The existing
`parsers/golang/TestNesting` passes on the unmodified baseline, including its normal error at 30,000; that expected
rejection would no longer hold on the overlay. No stack crash was reproduced.

The engine already exposes `WithMaxDepth`; generated parsers use a fixed constant. A grammar's number of helper
calls makes its syntactic nesting ceiling differ from another grammar's even at the same rule-call limit. A
standalone parser may serve ordinary inputs and occasional deeply nested inputs in the same process. A per-call
override permits that distinction without regenerating or changing concurrent callers' limits.

## Goals

- Preserve default parsing behavior, position units, no-option calls and rule-call accounting; document the Go
  migration from optional unit arguments and old function-value signatures.
- Allow explicit generation-time defaults and lower or higher per-invocation ceilings.
- Use one effective ceiling across generated Go `Parse`, `ParseRule`, `Recognize` and `ParseAST`, including typed
  direct and conversion modes, ordinary calls, Pratt operands, dispatch and left-recursive paths.
- Support the corresponding TypeScript entry points with the same zero/default and positive-override contracts.
- Validate configuration before parsing or writing generated output; reset pooled state without limit leakage.

## Non-goals

- Matching a reference parser's maximum syntactic nesting or changing distributed ready-made parser defaults.
- Adding a mutable global setter, cancellation, work budgets, or changing the grammar/serialized program format.
- Removing dependence on the host call stack. A larger resource ceiling cannot guarantee acceptance at that depth.

## Design

### Generation-time default

```go
code, err := pego.GenerateGo(g, "parser", "main", pego.WithGeneratedMaxDepth(600_000))
```

```sh
pego gen -g grammar.pego -pkg parser -max-depth 600000 -o parser.go
```

`GenOptions.MaxDepth` stores the option internally. Zero selects the existing default of 100,000; a positive value
selects the generated parser's default. Negative values are errors. For TypeScript, the value must also fit an
exact JavaScript integer. The CLI uses the same validation as the Go API. Omitted, zero and explicit 100,000 options
produce identical output within the new generator version. Unsupported typed TypeScript generation remains an error.

### Per-invocation Go API

The generated package follows the engine's functional-option style. `Result` below denotes the grammar's
existing typed result; it is not a new wrapper type.

```go
type ParseOption func(*parseOptions)

func WithUnit(unit Unit) ParseOption
func WithMaxDepth(n int) ParseOption

func Parse(input string, opts ...ParseOption) (*Node, error)
func ParseRule(name, input string, opts ...ParseOption) (*Node, error)
func Recognize(input string, opts ...ParseOption) error
func ParseAST(input string, opts ...ParseOption) (Result, error)
```

`Recognize` is emitted when recognition is requested, and `ParseAST` when typed Go output is requested.
The options data type `parseOptions` is internal to the generated package. Its unit defaults to CodePoints;
its depth defaults to the generated value. There are no Go `WithOptions` entry points or exported options data
struct. Options apply in order and the last setting for each field wins; validate the effective depth after
all options are applied. `WithMaxDepth(0)` resets that field to the generated default. A nil option panics when
applied, matching the engine's existing functional-option behavior.

```go
node, err := parser.Parse(input, parser.WithUnit(parser.Bytes), parser.WithMaxDepth(600_000))
```

A negative effective invocation limit returns a configuration error before matching input or evaluating actions.
Zero means the generated default, never an unlimited or zero-call parser. A positive value overrides the default
in either direction. Configuration validation precedes rule lookup in `ParseRule`, making invalid limits
predictable. Unit selection retains the current matching/position behavior; this feature does not introduce a
separate unit-validation change. Recognition, typed values, recovery results and error contracts remain intact.

Calls without options keep the same syntax and defaults. Existing optional-unit calls migrate from
`Parse(input, Bytes)` to `Parse(input, WithUnit(Bytes))`, and similarly for ParseRule, Recognize and ParseAST.
The old `[]Unit` variadic expansion is replaced with options: choose its first unit to retain the old behavior,
or build `[]ParseOption` explicitly. Function-value types change from `func(string, ...Unit)` to
`func(string, ...ParseOption)` (with the corresponding signatures for other entry points). This is an explicit
Go source migration; do not hide it with `...any` or a union-style compatibility overload.

### Per-invocation TypeScript API

```ts
export interface ParseOptions {
  unit?: Unit;
  maxDepth?: number;
}

parseWithOptions(input, options?)
parseRuleWithOptions(name, input, options?)
recognizeWithOptions(input, options?)
```

These functions accept the existing input types and return the same result types as their corresponding
entry points. Existing `parse`, `parseRule` and `recognize` keep their signatures. Omitted `maxDepth` and zero use
the generated default; a positive safe integer overrides it. Reject negative numbers, fractions, NaN, infinity
and integers outside JavaScript's exact range through the existing result/error mechanism before matching. Unit
selection retains existing defaults. Do not overload the existing unit parameter with an options union.

### Accounting, ownership and implementation

Resolve the effective depth once per invocation and initialize parser-local state before entering any rule.
All existing depth comparisons and fatal diagnostics use that value. Memo answers do not add nested calls;
calls that execute a body and nested Pratt operands do. First-character dispatch must preserve counter/error
behavior near the ceiling. Do not reinterpret the counter as brackets, AST nodes or syntactic nesting.

Go resolves options before entering the parser and does not retain the option slice or functions. TypeScript resolves scalar fields immediately,
so later mutation of the caller's object does not affect an in-progress invocation. Go's pooled plain and typed
parsers must reset the effective limit on every checkout, including after configuration/depth errors and recovery;
a prior override cannot leak into the next call. Each concurrent or reentrant invocation has independent state.
Typed conversion delegates to the node path with the same resolved options; typed direct parsing uses the same effective ceiling.
Recognize retains value-free semantics and does not start evaluating actions.

Generated Go uses the goroutine stack; TypeScript uses the JavaScript host stack. A host can exhaust its stack
before a high configured limit is reached. Preserve existing fatal/host-stack handling and document the limitation.
The engine's iterative bytecode backend remains the option for an explicit parsing stack.

### Compatibility and implementation sequence

TypeScript adds options-aware entry points. Go changes the existing entry points to functional options. Generated
source changes to include the option constructors and parser-local field; byte identity with the old generator is not a goal. Default parsing results, limits, errors and existing
function names remain unchanged; Go optional-unit arguments and function-value signatures require migration. Preserve deterministic new output and equality among equivalent
settings. New exported names follow the existing typed generator's runtime-name collision policy: append underscores
to colliding grammar type names in stable sorted order. Thus an existing grammar type `ParseOption` can become
`ParseOption_`, and another `ParseOption_` can become `ParseOption__`. Reserve `WithUnit` and `WithMaxDepth` too. Aliases and typed return signatures can change
on regeneration; callers using those types or `ParseAST` function values must migrate to the new names. In addition to
these typed-return changes, all Go function-value consumers must use the new functional-option signature. Include this migration in
the generation guide/release documentation, along with the new functions' potential collisions with handwritten
same-package declarations. Such handwritten declarations are outside the generator's input and must be renamed
by the consumer; do not emit duplicate declarations or silently promise unconditional source compatibility.
Serialized `.pegoc` files and engine parse options are unaffected. Ready-made parser regeneration updates generated
APIs while retaining their current defaults and documented reference deviations.

1. Add generation-option validation, the functional Go/options-aware TypeScript API and parser-local effective limit together.
   Keep TypeScript legacy wrappers, typed direct/conversion routes and recognition aligned in the same reviewable unit.
2. Validate generated consumers and benchmark ordinary/default/override paths before adopting the implementation.
3. Regenerate all existing parsers; update public API comments, CLI help, generation/TypeScript/runtime guides and
   development status with the code. Keep parser feature work separate from these generator regression gates.

## Alternatives considered

- **Generation-time configuration only.** Keeps invocation code constant and permits custom builds, but cannot select
  limits for ordinary and deep requests handled by the same distributed parser. Use it as the default plus an override.
- **Per-parse options only.** Supports individual requests but cannot establish a custom default for all legacy calls.
  Both layers serve different callers and share one validation/accounting contract.
- **Separately named Go WithOptions functions.** Preserve old unit-vararg signatures but split everyday parsing
  across two function families. Use functional options on the existing Go names, consistent with the engine,
  and document the unit/function-value migration. TypeScript retains its separate options-aware family.
- **Overload old Go parameters with any/interface unions.** Could preserve some raw Unit calls, but obscures the
  functional-option contract and still does not preserve all old slice/function-value types. Use ParseOption.
- **Mutable global limit.** Requires little API surface but couples concurrent requests and pooled parser reuse. Use
  parser-local state initialized from a copied invocation value.
- **Raise every generated parser's default.** Changes existing resource policy and cannot equate rule calls with
  language-specific nesting. Keep the existing default and distributed-parser deviations.
- **Iterative generated calls.** Can remove host call-stack dependence, but needs a broader design spanning values,
  captures, recovery, memoization, Pratt and left recursion, with independent performance gates.
- **Retain only the iterative-engine workaround.** Already supports deep parsing but requires an engine dependency
  instead of the standalone generated package and does not expose a standalone per-request policy.

## Testing

The reference/depth probes above are complete. The implementation adds the following regression coverage:

- `TestGeneratedMaxDepthValidation` and `TestWithGeneratedMaxDepth` check generation/API default equivalence,
  custom defaults, invalid/range errors and last-setting precedence. `TestGenMaxDepth` checks the same CLI defaults
  and confirms invalid options preserve an existing output file.
- `TestGeneratedDepthOptions` compares small limits with equally configured engines across ordinary recursion,
  left recursion, Pratt prefixes/RHS operands, first-character dispatch and recovery, both position units and typed
  direct/conversion output. It also covers sequential pool reuse, concurrent invocations, reentrant option evaluation,
  zero/custom defaults, largest native int, negatives, function-value signatures and nil options.
- `TestGeneratedDepthNameCollisions` compiles external direct/conversion consumers and checks deterministic
  underscore mappings for `ParseOption`, `ParseOption_`, `WithUnit` and `WithMaxDepth`, including aliases and typed
  function values.
- `TestGeneratedTSDepthOptions` runs the corresponding recursion/dispatch/recovery matrix on string and byte inputs,
  rejects negative/fractional/nonfinite/unsafe limits, accepts the maximum safe integer, verifies immediate scalar
  option resolution during an action and action-free recognition, and type-checks with strict TypeScript.
- `TestGeneratedDepthBenchmarkWorkloads` generates and validates standalone paired benchmark controls without
  timing them. Its same source works on the pinned baseline and the candidate, preserving comparable default
  entry points and separate small, large and deep inputs. See the performance section for the measurement gate.

The integrated root suite, all ten standalone parser module suites and vet checks, parser freshness,
regeneration and site tests pass on Go 1.27.1 and Node.js 24.19.0, including strict `tsc`.
Timed measurements are separate from these correctness checks.
The complete acceptance checklist is:

- Compile/run unchanged no-option consumers and migrated option/function-value consumers for every entry point,
  both Go position units,
  typed direct/conversion paths and TypeScript string/byte inputs. Preserve recovery results and Recognize behavior.
- Use predictable small recursion limits at, below and one call above the boundary. Cover ordinary recursion,
  Pratt prefix/RHS nesting, left recursion, dispatch and rule-specific parsing against equally configured engines.
- Test omitted/zero/explicit defaults, custom generated defaults, lower/higher overrides, negatives, largest Go int, repeated options/last-setting precedence and nil-option behavior,
  and TypeScript exact-integer boundaries/fractions/nonfinite values. Check API and CLI validation before output.
- Alternate low/high/default pooled calls and successes/failures; test parallel invocation and reentrant action
  calls where supported. Neither invocation nor typed conversion may retain Go option functions or share effective limits; TypeScript
  does not retain or mutate the caller's options object.
- Check deterministic generation and equivalent-default byte identity within the new version. Cover public-name
  collisions and exact deterministic suffix mappings for `ParseOption`, `ParseOption_`, `WithUnit` and `WithMaxDepth`,
  including alias/direct/conversion result types and function-value consumers. Regenerate all existing parsers,
  run root tests, every parser module/vet and site checks before commit.

## Performance and results

Measurements compare `e45ff82` with the depth-option implementation on the same
source base, using Go 1.27.1, Node.js 24.19.0, Apple M3 Max and `GOMAXPROCS=4`.
Three alternating baseline/candidate rounds run without competing builds or
benchmarks. The standalone grammar is `n+`, with `n = "(" n ")" / "é"`: small
inputs nest eight parentheses, large inputs repeat that item 1,024 times, and
deep inputs nest 8,192 parentheses. Both units, Node, recognition and typed
direct/conversion paths are measured separately. Go runs use 400ms per case;
TypeScript uses 20,000/100/20 timed iterations after warmup for small/large/deep.

The Go direct-entry controls report these candidate/baseline median time
ratios. Three pairs establish scoped measurements, not a significance test:

| Scope | Median ratio across measured cases |
|:--|:--|
| Small inputs, all APIs and units | 1.005–1.239 |
| Large inputs, all APIs and units | 0.953–1.171 |
| Deep inputs, all APIs and units | 0.989–1.055 |

Small CodePoints recognition costs 183–186ns versus 177–180ns in direct
output (median ratio 1.038, paired range 1.016–1.041); conversion output
is 183–184ns versus 178–180ns (1.026, 1.021–1.031). Bytes recognition,
which now passes `WithUnit(Bytes)`, costs 269–286ns versus 219–226ns
(1.219, 1.189–1.310); conversion is 275–277ns versus 222–225ns
(1.239, 1.226–1.245). Large CodePoints Node parsing in direct output has
a measured median increase of 17.1% (paired range 3.9–19.3%); the other
large median ratios span 0.953–1.045. Do not promise zero overhead.

Callsites matter: the separate callback-adapter controls retain a larger
small-recognition increase, with median ratios 1.649/1.677 for CodePoints
and 1.512/1.601 for Bytes in direct/conversion output. Removing dynamic
guards or moving the parser field in isolated diagnostic builds did not
remove that difference. These probes do not establish its mechanism or
a general throughput penalty; the direct-entry and callback observations
are both retained here.

No-option CodePoints calls add no allocation. Passing any functional option,
including an equal or zero depth and a byte-unit option, adds 16 bytes and
one allocation per invocation because the configuration is passed by pointer
to option functions. The captured option slice/functions are not retained by
parsers. On measured large/deep cases, this remains one extra allocation per
call rather than per rule. A future optimization must preserve ordering,
validation precedence, reentrancy and independent pooled invocations.

TypeScript legacy-call median ratios span 0.841–1.039 across these workloads;
all three-pair ranges span 0.833–1.051. Options and high-depth controls pass
independently. JIT warmup and host-stack behavior make these observations
insufficient to claim a general TypeScript speedup.

The existing approximately 256KiB JSON corpus uses three alternating 1s rounds
with the same toolchain and isolated CPU policy. Its generated CodePoints Node,
Bytes Node, typed AST and recognition median ratios are respectively
0.982 [0.942–1.090], 0.947 [0.921–1.040], 0.965 [0.957–1.027] and
1.011 [0.977–1.041]. Each paired range includes parity; this corpus does not
establish a consistent throughput regression or improvement. No-option Node
and typed AST allocation counts are unchanged; byte-unit Node parsing adds
the configuration allocation, with small amortized pool differences across
samples. These larger inputs do not erase the small-input costs above.
Reproduce with `BenchmarkParse/JSON/generated`, `generated_ast` and
`BenchmarkRecognize/JSON/generated` in `bench`, keeping baseline unit calls
and migrated `WithUnit` calls aligned.

Generated source grows from 99,946 to 101,512 bytes for typed direct Go,
96,847 to 98,399 for conversion Go, and 80,444 to 82,291 for TypeScript.
An identical production consumer linking both Go variants and calling
Node/AST/recognition without options grows from 2,981,746 to 2,998,626 bytes
(+0.57%). These are consumer executable sizes, not benchmark test-binary
sizes, whose additional benchmark controls would confound attribution.

Reproduce the standalone controls with `TestGeneratedDepthBenchmarkWorkloads`
and `PEGO_DEPTH_BENCH_DIR`, copying that test to the pinned baseline. Build
fixture binaries before measuring `BenchmarkDepthDirect` and
`BenchmarkDepthOptions`; run `node ts/bench.mjs` separately. Routine raw
results stay outside Git.

## Limitations and open questions

The generation-time plus per-invocation scope and preserved 100,000-call default are selected. The Go functional-option form and TypeScript options-aware form are selected;
compatibility/collision handling, runtime ownership, regression tests and serialized measurements
are complete. Functional options have the measured allocation/callsite costs above. Raising an override
does not guarantee host-stack capacity or reference-parser nesting parity. Broader per-invocation cancellation/work
budgets can extend these APIs in their own designs without changing this depth contract.
