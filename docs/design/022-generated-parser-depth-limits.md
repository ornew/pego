# 022. Configurable Depth Limits in Generated Parsers

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-10

## Summary

Expose the maximum nested rule-call count as a generation option, keeping the current default of 100,000 calls.
The proposed public API is `pego.WithGeneratedMaxDepth(n)` and the CLI flag is `pego gen -max-depth n`.
The chosen limit is embedded in the standalone Go or TypeScript parser. Parsing entry points keep their existing
signatures and each parser invocation has its own counter.

This removes the fixed ceiling for callers generating parsers for deeper inputs. Matching a language reference
parser's maximum syntactic nesting is a separate requirement: one syntactic layer can invoke several grammar rules.
The distributed Go parser's current default and its documented deviation remain unchanged under this proposal.

## Motivation

At `bc4f820`, the generated Go parser's 100,000-call guard accepts Go source with 16,000 nested parentheses but
returns `nesting too deep: more than 100000 rule calls` at 17,000 and 30,000. Go 1.27.1's `go/parser` accepts all
three inputs. The exact input is `"package p; var x = " + strings.Repeat("(", n) + "1" + strings.Repeat(")", n)`.
Both generated `Recognize` and `ParseAST`, with byte positions, show this difference. An isolated
source overlay changing only the guard to 600,000 accepts all three inputs in both entry points. The existing
`parsers/golang/TestNesting` passes on the unmodified baseline, including its normal error at 30,000; that expected
rejection would no longer hold on the overlay. No stack crash was reproduced.

The engine already exposes `WithMaxDepth`; generated parsers use a fixed constant. A grammar's number of helper
calls makes its syntactic nesting ceiling differ from another grammar's even at the same rule-call limit. Raising
the default globally would change resource behavior for every generated parser without defining reference parity.

## Goals

- Allow an explicit generation-time ceiling above or below the default.
- Preserve default output, existing parse signatures, rule-call accounting and position units.
- Apply one ceiling to generated Go `Parse`, `ParseRule`, `Recognize` and `ParseAST`, including conversion mode,
  direct rules, ordinary calls, Pratt operand calls and left-recursive paths.
- Apply the equivalent option to generated TypeScript `parse`, `parseRule` and `recognize`.
- Reject invalid option values before writing output, with deterministic generation and CLI diagnostics.

## Design

### Public behavior

Proposed usage:

```go
code, err := pego.GenerateGo(g, "parser", "main", pego.WithGeneratedMaxDepth(600_000))
```

```sh
pego gen -g grammar.pego -pkg parser -max-depth 600000 -o parser.go
```

`GenOptions.MaxDepth` stores the option internally. Zero selects the existing default; a positive value selects
that many nested rule calls. Negative values are errors. For TypeScript, the limit must also fit an exact JavaScript
integer. The CLI uses the same validation as the Go API. Zero and an explicit default value produce the same parser
as generation without the option. Unsupported typed TypeScript generation remains an error.

The generated ceiling is fixed at generation time. There is no global setter, mutable process-wide limit or new
per-parse option. All existing entry points use their parser's embedded ceiling, and pooled invocations reset the
counter as before. Existing fatal depth errors keep their format and show the selected number.

### Accounting and safety

Retain the current counting rules. Memo answers do not add nested calls; calls that actually execute a body and
nested Pratt operands do. First-character dispatch must preserve the counter/error behavior near the ceiling.
Do not reinterpret the counter as brackets, AST nodes or grammar-specific syntactic nesting.

A higher counter limit does not make the host stack larger or make recursive generation iterative. Generated Go
uses the goroutine stack, and TypeScript uses the JavaScript host stack. A host can exhaust its stack before a large
configured counter is reached. Use the engine's iterative bytecode backend for inputs that require a guaranteed
explicit parsing stack. This option is a chosen resource limit, not a promise to accept a reference parser's maximum.

### Compatibility and implementation

Resolve and validate the generation option once, then embed the selected constant in the runtime copied into the
output. Every existing depth comparison uses that constant; no extra comparison or allocation is added to parsing.
Keep default generated output unchanged. Custom output documents the selected ceiling. Serialized `.pegoc` files,
the grammar language, engine parse options and ready-made parser defaults are unaffected.

Update the public API comment, CLI help, generation/runtime/TypeScript guides and development status with the code.
Document how to regenerate a custom standalone parser and preserve the Go parser's existing conformance deviation.

## Alternatives considered

- **Raise every generated parser's default.** This immediately increases some ceilings, but changes resource policy
  for existing users and still cannot equate rule-call limits with language-specific nesting. Keep the default.
- **Per-parse generated options.** This would also let callers tune distributed parsers without regenerating, but
  needs a separate API design spanning current variadic `Unit` arguments, typed entry points and ready-made wrappers.
  A generation option solves the fixed-constant restriction with less API surface; per-parse resource controls can
  be considered with cancellation and work budgets.
- **Iterative generated calls.** Explicit call frames can remove dependence on the host call stack. This is a broader
  runtime/generator design involving return values, captures, recovery, memoization, Pratt and left recursion, with
  independent performance gates. Increasing a constant does not implement it.
- **Retain only the existing iterative-engine workaround.** This already supports deep parsing, but requires the
  engine dependency instead of a standalone generated package.

## Testing

The reference/depth probes described above are complete. Implementation tests remain proposed:

- Compile and run generated consumers just below and above small configured limits, in both position units and
  every supported Go entry point, including typed direct/general/conversion paths and recognition.
- Check ordinary recursion, Pratt prefix/RHS nesting, left recursion and first-character dispatch behavior against
  the engine configured with the same limit. Check successful reuse after a depth error.
- Run equivalent TypeScript consumers with a small host-safe limit; preserve normal error formatting.
- Check negative, zero, default, positive and JavaScript integer-boundary options, public API and CLI entry points.
- Verify default output is byte-identical, regenerate all 17 existing parsers, run root tests, every ready-made parser
  module and vet, and site checks before committing the implementation.

## Performance and results

The baseline/600,000 overlay establishes acceptance at the three measured Go nesting depths, not exact reference
maximum parity or throughput. Benchmark evidence for the implementation remains pending. Compare normal generated
parsing at the default with an equivalent explicitly configured parser, then measure deep inputs separately from
source parsing/generation. Record compiler metadata for each binary, allocations and sampling conditions. Parsing
operations should remain unchanged; any measured effects belong in the implementation commit. Keep raw logs local.

## Open decision

Confirm whether preserving current defaults while exposing generation-time control is the intended resolution, or
whether the distributed Go parser must instead match the reference's maximum syntactic nesting. The latter requires
its own grammar/runtime contract and validation across different recursive constructs; the measured 30,000-parentheses
overlay alone does not establish it. Do not mark either outcome implemented from this proposal.
