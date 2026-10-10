# 022. Configurable Depth Limits in Generated Parsers

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-10

## Summary

Make the maximum nested rule-call count configurable both when generating a standalone parser and for an
individual parsing invocation. Keep the existing default of 100,000 calls. The generation option becomes the
parser's default; a positive per-call override replaces it for that invocation only. Preserve existing entry-point
signatures and add options-aware entry points for Go, typed Go and TypeScript.

The proposed generation API is `pego.WithGeneratedMaxDepth(n)` and the CLI flag is `pego gen -max-depth n`.
Generated Go exposes `ParseOptions` and `ParseWithOptions`; TypeScript exposes the corresponding `ParseOptions`
and `parseWithOptions`. This record describes proposed APIs, not implemented behavior.

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

- Preserve default parsing behavior, position units, existing call syntax and rule-call accounting.
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

The generated package adds the following proposed value and functions. `Result` below denotes the grammar's
existing typed result; it is not a new wrapper type.

```go
type ParseOptions struct {
    Unit     Unit // CodePoints by default, as in existing entry points.
    MaxDepth int  // Zero uses the generated default; positive overrides it.
}

func ParseWithOptions(input string, options ParseOptions) (*Node, error)
func ParseRuleWithOptions(name, input string, options ParseOptions) (*Node, error)
func RecognizeWithOptions(input string, options ParseOptions) error
func ParseASTWithOptions(input string, options ParseOptions) (Result, error)
```

`RecognizeWithOptions` is emitted when recognition is requested, and `ParseASTWithOptions` when typed Go output is
requested. Existing `Parse`, `ParseRule`, `Recognize` and `ParseAST` keep their `unit ...Unit` signatures, including
first-unit behavior. Function-value compatibility holds where generated type names do not change; the collision
migration below covers typed return signatures. They use the generated default. An empty options value has
the same behavior as an ordinary call without a unit. Unit selection retains existing behavior; this feature does
not introduce a separate unit-validation change.

```go
node, err := parser.ParseWithOptions(input, parser.ParseOptions{
    Unit: parser.Bytes, MaxDepth: 600_000,
})
```

A negative invocation limit returns a configuration error before matching input or evaluating actions. Zero
means the generated default, never an unlimited or zero-call parser. A positive value overrides the default in
either direction; it is a chosen limit rather than permission to bypass other resource limits. Configuration
validation precedes rule lookup in `ParseRuleWithOptions`, making invalid limits deterministic. No additional
recognition/tree/typed semantics change; existing recovery-result and error contracts remain intact.

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

The options value is read at entry and is not retained or mutated. TypeScript resolves scalar fields immediately,
so later mutation of the caller's object does not affect an in-progress invocation. Go's pooled plain and typed
parsers must reset the effective limit on every checkout, including after configuration/depth errors and recovery;
a prior override cannot leak into the next call. Each concurrent or reentrant invocation has independent state.
Typed conversion delegates to the options-aware node path; typed direct parsing uses the same effective ceiling.
Recognize retains value-free semantics and does not start evaluating actions.

Generated Go uses the goroutine stack; TypeScript uses the JavaScript host stack. A host can exhaust its stack
before a high configured limit is reached. Preserve existing fatal/host-stack handling and document the limitation.
The engine's iterative bytecode backend remains the option for an explicit parsing stack.

### Compatibility and implementation sequence

The options-aware entry points are additive for generated packages without naming collisions. Generated source
changes to include the new functions, options and parser-local field; byte identity with the old generator is not a goal. Default parsing results, limits, errors and existing
function names and parameter lists remain unchanged. Preserve deterministic new output and equality among equivalent
settings. New exported names follow the existing typed generator's runtime-name collision policy: append underscores
to colliding grammar type names in stable sorted order. Thus an existing grammar type `ParseOptions` can become
`ParseOptions_`, and another `ParseOptions_` can become `ParseOptions__`. Aliases and typed return signatures can change
on regeneration; callers using those types or `ParseAST` function values must migrate to the new names. Legacy
function-value compatibility is guaranteed only where generated type names do not change. Include this migration in
the generation guide/release documentation, along with the new functions' potential collisions with handwritten
same-package declarations. Such handwritten declarations are outside the generator's input and must be renamed
by the consumer; do not emit duplicate declarations or silently promise unconditional source compatibility.
Serialized `.pegoc` files and engine parse options are unaffected. Ready-made parser regeneration updates generated
APIs while retaining their current defaults and documented reference deviations.

1. Add generation-option validation, the options-aware Go/TypeScript API and parser-local effective limit together.
   Keep legacy wrappers, typed direct/conversion routes and recognition aligned in the same reviewable unit.
2. Validate generated consumers and benchmark ordinary/default/override paths before adopting the implementation.
3. Regenerate all existing parsers; update public API comments, CLI help, generation/TypeScript/runtime guides and
   development status with the code. Keep parser feature work separate from these generator regression gates.

## Alternatives considered

- **Generation-time configuration only.** Keeps invocation code constant and permits custom builds, but cannot select
  limits for ordinary and deep requests handled by the same distributed parser. Use it as the default plus an override.
- **Per-parse options only.** Supports individual requests but cannot establish a custom default for all legacy calls.
  Both layers serve different callers and share one validation/accounting contract.
- **Replace unit varargs or overload existing parameters.** Could expose options under the shortest names, but breaks
  Go function-value signatures or complicates TypeScript typing. Add separately named functions instead.
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

- Compile/run legacy function-value and call consumers alongside every new entry point, both Go position units,
  typed direct/conversion paths and TypeScript string/byte inputs. Preserve recovery results and Recognize behavior.
- Use predictable small recursion limits at, below and one call above the boundary. Cover ordinary recursion,
  Pratt prefix/RHS nesting, left recursion, dispatch and rule-specific parsing against equally configured engines.
- Test omitted/zero/explicit defaults, custom generated defaults, lower/higher overrides, negatives, largest Go int
  and TypeScript exact-integer boundaries/fractions/nonfinite values. Check API and CLI validation before output.
- Alternate low/high/default pooled calls and successes/failures; test parallel invocation and reentrant action
  calls where supported. Neither invocation nor typed conversion may mutate options or share effective limits.
- Check deterministic generation and equivalent-default byte identity within the new version. Cover public-name
  collisions and exact deterministic suffix mappings for `ParseOptions`, `ParseOptions_` and `ParseWithOptions`,
  including alias/direct/conversion result types and function-value consumers. Regenerate all existing parsers,
  run root tests, every parser module/vet and site checks before commit.

## Performance and results

The baseline/600,000 overlay establishes acceptance at the three measured Go depths, not reference maximum parity
or throughput. Implementation measurements remain pending. Compare ordinary parsing with the old generated parser,
new legacy entry points, zero/default options and an equal explicit override. Measure node, recognition, typed
direct/conversion and TypeScript paths separately; then measure deep inputs with sufficient limits separately from
source generation and compilation. Record source/binary size, time, allocations and relevant retained state.

Resolving an override once avoids validation in every nested call, but replacing a constant comparison with a field
access and adding wrappers may affect performance. That is a hypothesis to measure, not a claim of zero overhead.
Use pinned baseline/candidate commits and toolchains, paired samples without competing CPU work, and preserve scoped
uncertainty. Record measured correctness costs in the implementation commit; keep routine raw logs local.

## Limitations and open questions

The generation-time plus per-invocation scope and preserved 100,000-call default are selected. Exact API spelling
and compatibility/collision handling require implementation review; no new depth API has landed. Raising an override
does not guarantee host-stack capacity or reference-parser nesting parity. Broader per-invocation cancellation/work
budgets can extend these APIs in their own designs without changing this depth contract.
