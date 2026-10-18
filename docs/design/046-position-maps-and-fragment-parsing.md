# 046. Position Maps and Fragment Parsing

- **Status**: Partially Implemented
- **Author**: @ornew
- **Date**: 2026-10-12

## Summary

Expose a reusable immutable map between UTF-8 byte offsets and editor positions, and add named-rule prefix parsing that reports the exact consumed endpoint. An editor can identify a fragment, parse it with an appropriate rule, and translate both node spans and consumed input back to the original document without changing parser position semantics.

The proposed Go surface is:

```go
type PositionEncoding uint8
const (
    PositionBytes PositionEncoding = iota
    PositionCodePoints
    PositionUTF16
)

type Position struct { Line, Character int }
func NewPositionMap(text string) *PositionMap
func (m *PositionMap) ByteOffset(p Position, encoding PositionEncoding) (int, error)
func (m *PositionMap) Position(byteOffset int, encoding PositionEncoding) (Position, error)
func (m *PositionMap) ByteOffsetFromUnit(offset int, encoding PositionEncoding) (int, error)
func (m *PositionMap) UnitOffset(byteOffset int, encoding PositionEncoding) (int, error)

type PrefixResult struct {
    Node          *Node
    Consumed      int // in the Unit selected by ParseOption; defaults to CodePoints
    ConsumedBytes int // UTF-8 bytes
}
func (p *Parser) ParsePrefix(name, input string, opts ...ParseOption) (PrefixResult, error)
```

The `Parser.ParsePrefix` signature and `PrefixResult` fields shown above are accepted. Recovered success returns the result together with the existing `SyntaxErrors` error, following `Parse`. Position-map names and exact conversion error types remain proposals. `PositionEncoding` is distinct from parser `Unit`: UTF-16 is for editor coordinates, while `Unit` remains the parse-time choice for spans and actions.

## Motivation

`Parser.WithStart` already returns a parser configured with another rule as its start, and generated parsers expose `ParseRule` for whole-input parsing by name. Both require the selected rule to consume the entire supplied input. The missing contract is prefix parsing with a reliable consumed endpoint; a caller otherwise has to infer consumption from a returned node, which is not valid for nil or action-shaped results. A per-call named whole-input helper could be added as a convenience, but it does not provide this missing capability.

The library's `Unit` covers byte and Unicode-code-point counts for node spans, action positions, syntax errors, and `len`. Editors also address text in UTF-16 code units. The language server has a private checkpointed `textIndex` that converts among byte, code-point, and UTF-16 columns. That implementation serves LSP requests but is not reusable by parser clients. Parser error line/column reporting follows parser-specific newline rules, so editor positions should be an explicit mapping rather than silently changing parse diagnostics.

## Goals

- Provide checked conversions among UTF-8 byte offsets and line/character positions in byte, code-point, and UTF-16 units.
- Keep map results tied to one immutable source snapshot and make conversions deterministic for malformed UTF-8.
- Parse a prefix with a caller-selected rule and return consumption in both bytes and the selected parser `Unit`.
- Convert flat byte/unit offsets as well as line/character positions, so any node span inside a fragment can be translated.
- Preserve existing parsing, action, and diagnostic position behavior.

## Non-Goals

- Incremental edit application, changed-range computation, or source ownership by `Document`.
- Parsing an arbitrary subrange while retaining absolute action positions or sharing memo entries with the containing document.
- Changing newline interpretation or the existing `Unit` meaning.

## Design

### Public Behavior and Contracts

`NewPositionMap` snapshots the supplied string value. The returned map is immutable and safe for concurrent reads; it may retain the source bytes for its lifetime. `ByteOffset` maps a zero-based line and character to a UTF-8 byte boundary. `Position` maps a byte boundary back to a line and character. `UnitOffset` and `ByteOffsetFromUnit` convert between flat byte offsets and byte, code-point, or UTF-16 offsets. Negative values, offsets outside the source, invalid encodings, and non-boundary code-point/UTF-16 positions return errors rather than rounding. This is a proposed strict public contract; the LSP adapter must preserve its existing protocol behavior, including clamping a position past a line end and mapping the middle of a surrogate pair to that character's start.

For line splitting, use the language server's established convention: LF, CRLF, and lone CR each end a line, with CRLF treated as one terminator. The line/character `Position` method rejects the boundary between CR and LF because no line/character position can round-trip it. Flat conversions permit that boundary: CR and LF remain two code points and two UTF-16 units. The terminator's starting boundary is the preceding line's end; the boundary after the complete terminator is column zero on the next line. Parser syntax-error line reporting remains governed by `spec/parsing.md` and can differ. Callers should use the coordinate model they need explicitly.

Offsets are byte offsets internally. In code-point mode, each valid Unicode scalar counts once; each invalid UTF-8 byte counts as one replacement code point, matching the parser's decoding behavior. In UTF-16 mode, valid supplementary scalars count as two code units and malformed bytes count as one replacement code unit. Byte encoding counts bytes and permits every byte boundary, including the interior of a valid UTF-8 scalar. Code-point and UTF-16 conversions require decoded-scalar boundaries; UTF-16 positions inside a surrogate pair are rejected. These rules apply to flat offsets and line columns, except that line columns exclude terminators and `Position` rejects the CRLF interior. Flat conversions retain that interior boundary and can translate byte-mode nodes that end inside UTF-8.

`Parser.ParsePrefix` starts at `name` and accepts the same variadic `ParseOption` values as `Parse`. `ConsumedBytes` is the exact UTF-8 byte cursor on successful completion; `Consumed` is that endpoint in the selected `Unit` (default `CodePoints`). The method reports consumption from the runtime cursor, never from `Node.End`: an action may return nil or a node with a span that differs from the parse cursor, and recognition may build no node. A zero-width success returns zero consumption. Recognition returns a nil `Node` with the same measured consumption. A recovered parse returns its consumed endpoint together with the existing `SyntaxErrors` result. An unrecovered syntax failure or fatal action/runtime error returns zero consumption; errors remain errors and are not converted into partial success.

The input is still the complete supplied string for the selected rule: a grammar containing `$$` succeeds only when its end-of-input assertion is reached at the actual end of that string. Cuts and left recursion retain their existing rule semantics. `ParsePrefix` does not imply EOF, and matching a nullable rule is a successful zero-consumption result. On failure, no speculative cursor is exposed.

To parse a source slice, map its start and end to byte offsets and slice the original text. Use a `PositionMap` for the fragment to convert any code-point `Node.Start`/`Node.End` through `ByteOffsetFromUnit`; add the slice's byte start; then use the original map's `Position` or `UnitOffset` as needed. `ConsumedBytes` directly identifies the consumed fragment endpoint. Do not add a byte base to code-point offsets. This preserves local action/error positions and avoids inferring a node's consumption.

### Implementation and Invariants

Extract the LSP checkpoint/index logic behind a small reusable package-level implementation, then have LSP and the public map share the same indexing machinery. Keep the map's source snapshot and checkpoints private. Build checkpoints at character boundaries and convert from the nearest checkpoint, validating both the input coordinate and resulting byte boundary. Keep the public method strict; an LSP adapter preserves the current clamping and surrogate-interior behavior instead of changing protocol results.

Implement `Parser.ParsePrefix` through a runtime entry path that selects `name` and returns the final byte cursor before the whole-input check used by `Parse`. Reuse `parseOptions(opts)` exactly once; do not alter `Parser.start`. Preserve concurrent safety by keeping all parse state in the existing per-call runtime. Keep `Parse`'s whole-input contract unchanged. If a named whole-input convenience is added, implement it through the same named-start machinery and keep its contract separate from prefix parsing.

### Compatibility and Backend Support

The additions are additive Go APIs. Existing `Parser.WithStart`, `Parser.Parse`, generated `ParseRule`, `Unit`, syntax errors, and LSP coordinate behavior remain unchanged. `ParsePrefix` delegates to the selected compiled backend, so closure, recursive bytecode, and iterative bytecode behavior should remain aligned. Recognition and all current parse options retain their meanings. No grammar or bytecode serialization changes are needed.

Document the coordinate model in the public Go API and update LSP internals to use the common index without changing protocol results. If a JavaScript/TypeScript PositionMap is considered later, define its string-index behavior independently; this proposal does not imply a TypeScript API.

### Implementation Sequence

1. Extract the private LSP index into shared indexing machinery; add strict public boundary conversion while preserving the LSP adapter's clamping behavior.
2. Add public `PositionMap` line/character and flat byte/unit conversions with malformed-UTF-8 and CRLF-interior tests.
3. Add `Parser.ParsePrefix` with runtime byte-cursor reporting and differential tests across backends and options.
4. Add a fragment example translating inner node spans and consumed endpoints through byte offsets; keep memo reuse and absolute action offsets out of scope.

## Alternatives Considered

- **Keep the LSP mapper private and ask every client to implement conversions.** This avoids public API commitments but duplicates Unicode and newline edge cases. The existing checkpoint implementation is a concrete basis for a reusable API.
- **Expose only byte/code-point offset conversion.** This is smaller, but clients that need editor ranges would still duplicate line indexing. The proposed line/character methods make the source coordinate model explicit.
- **Use `Parser.WithStart(name).Parse(input)` for fragments.** This already selects a named start rule, but still requires complete-input matching and provides no reliable consumed endpoint. It remains useful for whole-input named parsing, not a substitute for `ParsePrefix`.
- **Add only a range-based `ParseFragment` method.** It would need to decide how rule scope, absolute action positions, syntax-error offsets, and units interact. Explicit slicing plus prefix parsing has clearer local-input semantics and composes with the map.
- **Shift all fragment results to absolute positions.** That would make actions and errors depend on an undocumented base offset and complicate existing `Unit` behavior. Callers can translate spans explicitly.

## Testing

Test conversions at the start/end of input and lines, empty text, LF/CRLF/lone-CR boundaries, multibyte code points, supplementary scalars, malformed UTF-8, and invalid interior offsets. Verify round trips at every accepted boundary for each encoding and mapping. Include byte-mode UTF-8 interior boundaries and CRLF interior flat round trips with line-position rejection. Verify stable error behavior for out-of-range positions. Compare the extracted LSP implementation with existing protocol fixtures.

For `ParsePrefix`, test named starts, incomplete suffixes, EOF assertions, options in both units, recognition, nil-returning and zero-width actions, recovered errors, ordinary failure and fatal action errors. Verify exact `ConsumedBytes` and unit consumption even when `Node` is nil or has a shorter span. Compare each backend with a wrapper-rule reference. Verify arbitrary node spans and consumed endpoints can be translated from a fragment through flat unit conversion and the original map.

## Performance and Results

The LSP index already uses checkpoints to avoid rescanning a long line from its beginning. Reuse that design rather than adding a second index. Before accepting a public implementation, compare conversion time and retained bytes on short editor buffers and long lines against the current LSP implementation; no performance result is established by this proposal.

## Limitations and Open Questions

The prefix API and recovered-success result/error contract are implemented.
Position-map exported names and exact conversion error types still need review.
Prefix parsing deliberately returns local coordinates; callers that need
absolute positions must translate them.

## Implementation Status

The named-prefix API is implemented independently of the position map. It uses
an invocation-local runtime entry, records the successful cursor before returning
the rule value, and does not change the parser's configured start rule. Normal
code-point parses reuse the byte-offset table. Recognition and malformed input
may require a second, work-limited decoding pass over the consumed prefix. The
cursor-to-byte conversion uses the original source bytes, so a valid U+FFFD
occupies three bytes while each malformed byte remains one byte. The guide
[Parsing a Named Prefix](../guide/prefix-parsing.md) covers its public contract.

The position-map API remains a proposal. Its adoption requires extraction of the
private LSP index without changing protocol clamping, plus conversion and
retention measurements. The prefix entry has no dependency on that API.
