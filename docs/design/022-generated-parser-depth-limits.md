# 022. Configurable Depth Limits in Generated Parsers

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-10

## Summary

Make the maximum nested rule-call count configurable both when generating a standalone parser and for an
individual parsing invocation. Keep the existing default of 100,000 calls. The generation option becomes the
parser's default; a positive per-call override replaces it for that invocation only. Preserve calls without options while moving Go/typed Go entry points to functional options; keep TypeScript's existing
unit arguments and add options-aware entry points there.

The proposed generation API is `pego.WithGeneratedMaxDepth(n)` and the CLI flag is `pego gen -max-depth n`.
Generated Go uses `Parse(input, opts ...ParseOption)`, `WithMaxDepth(n)` and `WithUnit(unit)`; TypeScript exposes
`ParseOptions` and `parseWithOptions`. This record describes proposed APIs, not implemented behavior.

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

These proposed functions accept the existing input types and return the same result types as their corresponding
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

The reference/depth probes above are complete. Implementation checks remain proposed:

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

The baseline/600,000 overlay establishes acceptance at the three measured Go depths, not reference maximum parity
or throughput. Implementation measurements remain pending. Compare ordinary parsing with the old generated parser,
new no-option entry points, zero/default options and an equal explicit override. Measure node, recognition, typed
direct/conversion and TypeScript paths separately; then measure deep inputs with sufficient limits separately from
source generation and compilation. Record source/binary size, time, allocations and relevant retained state.

Resolving an override once avoids validation in every nested call, but replacing a constant comparison with a field
access and adding wrappers may affect performance. That is a hypothesis to measure, not a claim of zero overhead.
Use pinned baseline/candidate commits and toolchains, paired samples without competing CPU work, and preserve scoped
uncertainty. Record measured correctness costs in the implementation commit; keep routine raw logs local.

## Limitations and open questions

The generation-time plus per-invocation scope and preserved 100,000-call default are selected. The Go functional-option form and TypeScript options-aware form are selected;
compatibility/collision handling and implementation details require implementation review; no new depth API has landed. Raising an override
does not guarantee host-stack capacity or reference-parser nesting parity. Broader per-invocation cancellation/work
budgets can extend these APIs in their own designs without changing this depth contract.
