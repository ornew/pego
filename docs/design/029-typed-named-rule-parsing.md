# 029. Parse Named Rules as Typed Values

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Generated parsers built with `-types` should offer `ParseRuleAST`, a
whole-input API that selects a grammar rule by name and returns its generated
typed representation as `any`. It leaves the declared signatures and parse
semantics of `ParseRule` and `ParseAST` unchanged, but reserving the new
exported name can rename a colliding generated type and its underscore
cascade. The API uses the generated parser's `ParseOption` values, including
position-unit and maximum-depth options.

## Motivation

Generated Go parsers expose `ParseRule(name, input, opts...) (*Node, error)`
and `ParseAST(input, opts...) (StartType, error)`. Typed generation emits a
typed rule table, but the public typed entry point only parses the configured
start rule. Callers that need a typed subrule must currently parse a larger
wrapper or use `ParseRule` and reproduce generated conversion behavior.

The XML parser has this use case for entity replacement content and declaration
sequences: it wraps the fragment in a synthetic document, parses a `Document`,
extracts a subvalue, and adjusts spans. A named typed entry point can expose
the selected rule's value directly while keeping the generic `ParseRule` API
available.

## Goals

- Add `ParseRuleAST(name, input, opts...) (any, error)` only to parsers
  generated with `-types`.
- Select the result type from the selected grammar rule's existing generated
  type mapping; do not introduce another type system.
- Preserve whole-input, action, recovery, error and position-unit semantics.
- Resolve and validate options before looking up the rule, as required by the
  generated parser options contract.
- Preserve `ParseRule` and `ParseAST` declarations, start-rule behavior and
  parse semantics; identify and test the generated-name collision migration
  for grammars that define a type named `ParseRuleAST`.

## Non-Goals

- Changing the return type or behavior of `ParseRule` or `ParseAST`.
- Generating a separate public function for every grammar rule.
- Adding generic type parameters, a parser-specific type registry or a new
  conversion contract.
- Changing grammar semantics or the XML parser as part of the API design.

## Design

### Public API and Result Mapping

For a typed generated parser, emit:

```go
func ParseRuleAST(name, input string, opts ...ParseOption) (any, error)
```

The function applies and validates `opts` exactly once before resolving
`name`, then dispatches by the existing rule-name table using those resolved
options. An unknown name returns `(nil, err)` with the same undefined-rule
error used by `ParseRule`. Calls parse the entire input, just
like `ParseRule` and `ParseAST`; a prefix match with trailing input is an
error.

The returned dynamic value follows the selected rule's inferred type and the
existing `ParseAST` mapping:

- structs and terminals use their generated Go values; terminal values retain
  their `Span` and text;
- scalar fields and aliases remain `int`, `string` or `bool`; lists and optional
  rule values use their generated slice or pointer/interface representation.
  Grammar rules retain the existing node-producing contract, so this API does
  not enable scalar top-level rule results;
- declared aliases use the generated alias type; node unions return the
  generated union interface with the concrete member as the dynamic value;
- CST node types return `*Node`, while open-ended `node`, terminal and `any`
  mappings return `any`.

Generated documentation should list each rule's Go result type. Callers can
use a type assertion or type switch after selecting a rule by name. `any` is
necessary because the return type cannot be chosen statically from a runtime
string.

If `#recover` produces syntax errors, preserve the generated `ParseAST`
contract: return the value together with `SyntaxErrors`. An `Error` where a
struct or terminal is expected becomes the documented nil value; union and
list positions follow the existing generated conversion rules. For an ordinary
parse failure without a recovered value, box the selected result type's zero
value in `any` (including a typed nil for pointer or slice results; a nil interface stays nil
when assigned to `any`); an
option or unknown-name error returns a plain nil because no rule type was
selected. A successful rule whose semantic value is nil also returns the selected type's
zero value with a nil error. Thus successful `any`, `node` and union-interface
nil results return plain nil from `ParseRuleAST`. Legacy `ParseAST` retains its
existing conversion behavior, which can box a nil `*Node` in an `any` result.
Action errors, syntax errors and trailing-input errors retain their existing
classification.

### Implementation and Invariants

Emit a name switch from the grammar's rule table. Each case must use the
selected rule's own entry and inferred result type; it must not route through
the configured start rule. Select direct typed execution only when the existing
full `typedGen.runtimeOK` guard succeeds for the grammar and selected result.
That guard also rejects field-reading actions or predicates and any
unrepresentable field type anywhere in the generated structs; do not replace
it with a selected-result-only check. On the direct path, call the corresponding
typed rule in `trules` and use the same generated mapping as `ParseAST`.
Otherwise, use the matching Node rule in `rules` and the existing conversion
semantics, even if the selected rule's result alone appears representable.

Reuse the existing typed runtime, converters, parser pool and result ownership
rules. If a result converter is currently generated only for the start type,
make the required per-rule converters available without changing the result
or output of the existing `ParseAST` entry point. Keep one parse per call and
preserve the generated runtime's frame, memo and scratch-recycling behavior.

Options must be applied exactly once and resolved before rule lookup, so invalid
options take the same precedence over an unknown rule name as for the other
generated entry points. Reuse a resolved-options helper for dispatch rather
than replaying the option functions in `tparse`. The selected rule is parsed
with those resolved options, including the requested `Unit` and depth limit.

### Compatibility and Support

The new entry point is additive at the API level for typed generated Go
parsers, and it does not change `ParseRule`'s `*Node` return or `ParseAST`'s
configured start-rule behavior. However, generated identifiers are allocated
from a reserved-name set: adding `ParseRuleAST` can rename a grammar-generated
type or alias with that name by appending underscores, and a collision cascade
can rename later generated identifiers. This can break source consumers that
name those generated types directly or change the spelling of `ParseAST`'s
static return type even though its behavior is unchanged. Document this
regeneration/migration effect and test grammars with `ParseRuleAST` and
`ParseRuleAST_` type names. Untyped parsers continue to expose only
`ParseRule`. No grammar, serialized format, TypeScript API or runtime option
changes are required.

### Implementation Sequence

1. Derive and emit per-rule result mappings from the same type metadata used by
   `ParseAST`, including any converters needed for Node fallback.
2. Emit `ParseRuleAST` dispatch for typed parsers, preserving option
   validation before name lookup and using the selected rule in either the
   typed or Node-conversion path.
3. Document the generated name-to-type mapping and add the API to the typed
   code-generation guide.

## Alternatives Considered

- **Change `ParseRule` to return `any` for typed parsers.** This breaks callers
  that expect `*Node` and makes the typed and untyped APIs inconsistent. Keep
  `ParseRule` stable and give typed conversion a distinct name.
- **Generate one statically typed public function per grammar rule.** This
  gives static return types, but expands the API and requires a naming and
  collision policy for arbitrary grammar rule names. It also does not provide
  dynamic name lookup.
- **Use a generic `ParseRuleAST[T]`.** Go cannot infer `T` from `name`; callers
  must supply a type that can disagree with the selected rule, requiring a
  runtime assertion without added safety.
- **Require callers to parse a `Node` and convert it themselves.** This
  duplicates generated conversion logic and leaves the typed subrule use case
  cumbersome.

## Testing

Use a synthetic typed grammar with named rules returning structs, terminals,
lists, optional values, declared aliases, node unions, CST nodes and `_`,
with scalar fields and aliases. Verify existing scalar rule rejections remain
unchanged. Compare each result with that rule's existing Node parse followed by
the generated conversion mapping. Cover rules whose result type differs from
the configured start rule, and verify aliases and union interfaces with type
assertions.

Test unknown names, invalid options before unknown-name lookup, exactly-once
option application, both position units, depth defaults and overrides, trailing
input, action failures, syntax failures, and `#recover`. Assert the value/error
pair and `SyntaxErrors` behavior, including `Error` results in struct, terminal,
union and list positions. Run both direct typed execution and forced Node-conversion
fallback. Include a named struct and a named terminal reached through an action that reads a
field; add an unrelated struct with an unrepresentable field to force the full
`runtimeOK` fallback even when the selected terminal or struct would otherwise
be representable. Compile consumer fixtures for both execution modes and
assert the documented zero and typed-nil results on ordinary failure. Also
cover successful nil-valued `_`, `node` and union rules and verify legacy
`ParseAST` nil boxing remains unchanged. Add
grammar types named `ParseRuleAST` and `ParseRuleAST_` to verify underscore
cascades, consumer compilation, and any `ParseAST` return-type spelling
change. For the XML subrule use case, compare `content` and declaration
results with the existing wrapper-based behavior on nested markup, Unicode and
malformed input; verify spans remain relative to the fragment without
caller-side shifting.

## Performance and Results

Implemented by generated per-rule dispatch and the resolved-options typed
runtime helper. Focused validation covers external direct and conversion
consumers, the full grammar fallback guards, all currently valid result
mappings, selected-type zero values, recovery, option precedence and single
application, both position units, depth overrides, concurrency and exported
name collisions. The unchanged XML grammar validates content and declaration
fragments against wrapper-based parsing after normalizing wrapper spans.
Existing alias/depth controls and real Node/strict TypeScript depth checks also
pass. Scalar rule rejection remains unchanged. The full engine suite, parser
modules, generated-output freshness checks and Go vet pass.

Measured on 2026-10-11 with Go 1.27.1 on Apple M3 arm64, GOMAXPROCS=4,
five alternating parent/candidate pairs and 300 ms per benchmark sample. The
parent generator is `f8c2399`; regenerating fixtures from `6fac146` produces
identical source. Both fixtures use the same benchmark harness and inputs.
Across 36 existing Node, Recognize and typed AST cases for capture-heavy,
capture-free and TypeScript grammars generated as Go, allocation counts are
unchanged. Paired median candidate/parent time ratios range from 0.962 to 1.024
for Node, 0.978 to 1.022 for Recognize and 0.919 to 1.077 for AST. No case is
slower in all five pairs; these short measurements do not establish a speedup
or eliminate smaller costs. Generated source grows by 3,681 bytes (3.14%),
890 bytes (0.88%) and 198,575 bytes (3.91%) respectively. Application binary
size and compilation cost remain unmeasured.

A separate XML comparison uses the generated `content` rule on a Unicode
fragment, and on that fragment repeated 128 times. The baseline parses a
prebuilt `<r>fragment</r>` and returns its content; the named entry point parses
the fragment directly. Both return the content through `any`. With the same
five-pair protocol, paired median named/wrapper time ratios are 0.622 and
0.617 for the small code-point and byte cases, and 0.886 and 0.869 for the
large cases. Allocation counts fall from 9 to 8 and 10 to 9 for small inputs,
and from 36 to 34 and 37 to 35 for large inputs. Median allocated bytes fall
from about 5.16 KB to 4.39 KB, and 104.7 KB to 97.2 KB respectively.
This compares two parsing workloads with different outer structures, rather
than isolating an optimization of identical work. Wrapper construction and
span adjustment are outside the timed region, so the measurements do not
include their potential savings. Conversion-heavy named-rule performance and
additional recovery workloads remain unmeasured.

## Limitations and Open Questions

Because rule names are selected at runtime, the API cannot give callers a
statically inferred return type. Callers must use the documented per-rule
mapping and check the dynamic value. Generated type-name collisions can
require source updates when a parser is regenerated, even though the existing
entry-point behavior remains unchanged. The API does not
change type inference or add a typed wrapper for each rule.
