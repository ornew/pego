# 057. Source Metadata and Declaration Comments

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-14

## Summary

Add common source spans and normalized comment ownership to source-bearing
public grammar nodes and serialize that metadata in a nested JSON `source`
object. Follow the comment-group ownership transitions of the [Protocol Buffers
v32 tokenizer](https://github.com/protocolbuffers/protobuf/blob/v32.0/src/google/protobuf/io/tokenizer.cc)
([descriptor contract](https://github.com/protocolbuffers/protobuf/blob/v32.0/src/google/protobuf/descriptor.proto#L1316-L1362),
[tokenizer tests](https://github.com/protocolbuffers/protobuf/blob/v32.0/src/google/protobuf/io/tokenizer_unittest.cc)),
while retaining PEGO's lexical rules. The concrete source grammar must construct
ownership directly; the manual parser must agree. No semantic-analysis or
post-parse attachment pass is involved.

## Motivation and Goals

The public grammar AST should let documentation and tooling clients read a
declaration's comments without inferring ownership from positions or formatter
layout. A shared value keeps spans and normalized documentation together while
raw comment markers and layout trivia remain available to the in-memory
formatter.

- Add a common `SourceInfo` value to source-bearing grammar nodes and preserve
  source metadata through Go AST construction, JSON conversion and cloning.
- Make normalized leading, trailing and detached comments directly available
  to API and documentation consumers.
- Keep raw formatter trivia separate from normalized ownership.
- Preserve existing diagnostic locations and grammar behavior.

## Proposed Design

### Public Source Information

```go
type SourceInfo struct {
    Pos                     Pos
    End                     Pos
    LeadingComments         string
    TrailingComments        string
    LeadingDetachedComments []string
    DiagnosticPos           Pos
}
```

Embed this value in source-bearing declarations, fields, expressions, actions,
attributes, pragmas, Operators Rule entries and explicit source grouping nodes.
`End` is exclusive; both positions use the existing line/column units. A span
is one continuous source interval: it excludes leading and trailing comments
owned at its boundaries, but includes comments inside the interval. Leading/
trailing values are normalized strings;
each detached entry is one comment paragraph. Normalized comment text preserves
newlines and conventional whitespace after comment markers. Empty values are
zero values and may be omitted in JSON. Clone detached slices rather than
sharing mutable backing storage.

Each source-bearing node exposes `Source() *SourceInfo` and satisfies
`SourceNode`. `Diagnostic()` returns `DiagnosticPos` when valid and otherwise
falls back to the span's `Pos`. This keeps operator-token diagnostics stable
when `Pos` becomes the source-span start. `Grammar.Clone()` is the grammar-level
deep-copy operation and copies all source metadata, including detached-comment
slices. `SourceInfo` has no clone method. The clone contract covers the
supported grammar AST and attribute argument value domain; it does not cover
arbitrary application values or cyclic maps and slices embedded in attribute
arguments. Shared AST subtrees stay shared in the clone, and AST pointer cycles
are preserved for `Validate` to report.
Preserve the `Grammar.Package` string and use `Grammar.PackageSource` for the
package clause span and owned comments. Use `OperatorsRule.SkipSource` for the
whole skip clause, from the `skip` keyword through its expression. Do not
retain the old `PackagePos` field.

### Comment Collection and Ownership

The collector groups PEGO comment tokens and returns a boundary record with
trailing text for the previous token, detached groups, leading text for the next
token, and `HasNext` to indicate whether a next owner exists. Keep pending state
local to a parse production or transactionally restore it during backtracking;
do not use mutable global state. Apply the following transitions:

| Event | Collector action |
| --- | --- |
| File start | Disable previous-token attachment; no prior token exists. |
| Inline comment | Flush immediately as provisional trailing and disable further trailing attachment. A following comment-only run starts a new group; it never extends the inline comment. |
| Consecutive `//` comments | Combine into one group. |
| Block comment | Flush any pending group, then start a distinct group. Switching from block to line comments also flushes. Empty tokens such as `/**/` still count as groups. |
| Blank line | Flush pending group, then disable previous-token attachment. |
| Flush with attachment enabled | The first eligible group trails the previous token and disables further trailing attachment; later flushed groups detach. |
| Flush without attachment | Record the group as detached. Without a blank-line flush, the final buffered group leads the next token. |
| Same-line ambiguity | Apply `MaybeDetachComment` when the next token is on the previous token's line, or a provisional trailing block ends on the next token's line. If exactly one group exists across trailing, detached and pending buffers, move it to detached rather than guessing. With multiple groups, retain the first eligible trailing group, detach intervening groups, and make the final pending group leading. |
| Closing delimiter or EOF | Flush by the same eligibility rule. Return detached groups even when `HasNext` is false; the owner constructor leaves them unattached and preserves original tokens as raw formatter trivia. |

The ownership mapping is part of the grammar's typed construction. Leading and
detached declaration comments belong to the declaration node, not its name or
body; trailing comments after its final token belong to that declaration.
Comments before a list element belong to the element, not a separator. Nested
expression boundaries belong to the expression production that returns them.
Explicit Group nodes own delimiter-adjacent comments. Wrappers such as Capture,
Atomic and Attributed follow their production's return path and must not
duplicate ownership merely because spans share a boundary. The root grammar and
manual parser must produce the same owner paths.

The executable fixture [`spec/testdata/comment-boundaries.pego`](../../spec/testdata/comment-boundaries.pego)
and focused test [`spec/comment_boundary_test.go`](../../spec/comment_boundary_test.go)
exercise the boundary record and package/rule owner construction directly. The
focused matrix covers 34 boundary cases across three backends and two position
units, plus direct package/rule construction and a generated typed Go consumer.
Detached values use typed `[]CommentText` records; an adapter copies `Text`
into normalized strings. This fixture covers selected ownership construction,
not end-to-end public metadata or formatter migration. General typed-list
construction is separate and is not required for this design.

### Reference Cases and Normalization

A standalone run after `baz` with no inline comment trails it when a closing
scope flushes the group. Without a blank line, the same run before another
statement leads that statement. An inline package comment is flushed
immediately; a later comment-only run is distinct and may be detached. A
following leading run belongs to the next declaration. At EOF, a final
ineligible group remains raw-only when there is no next owner.

Adjacent block groups can split across tokens: in `corge /* trailing */ /*
leading */ grault`, the first group trails `corge` and the second leads
`grault`. A single ambiguous same-line group is detached. A multiline block
ending on the next token's line follows the same ambiguity check. These are
ownership transitions, not a claim that PEGO adopts every protobuf lexical
rule: PEGO's CR-only newlines, Unicode whitespace and nested block comments
remain governed by its own lexer.

For normalized text, remove only the `//` marker from line comments, retaining
following whitespace and the terminating newline. For block comments, remove
the outer delimiters, retain first-line content, and on later lines strip
indentation and the first `*` marker. Preserve encountered newlines and nested
comment delimiters as content. Raw comment markers and formatter layout remain
separate from normalized ownership; they are not a byte-for-byte copy of every
source whitespace detail.

### JSON and Formatting

Serialize embedded node metadata in an explicit nested `source` object; package
and skip metadata use `packageSource` and `skipSource`. Do not rely on Go
embedding to flatten fields. Keep AST type tags and position units. Older JSON
without these optional metadata objects decodes to zero values. Example:

```json
{
  "@type": "RuleDef",
  "name": "B",
  "source": {
    "pos": {"line": 10, "col": 1},
    "end": {"line": 10, "col": 18},
    "leadingComments": " leading for B\n",
    "trailingComments": " B trailing\n",
    "leadingDetachedComments": [" detached for the next declaration\n"]
  }
}
```

Raw formatter trivia is not serialized in `source`. Therefore AST JSON does not
promise enough layout information for lossless formatting after decode; a
future lossless archive needs a separate layout representation. On an in-memory
parsed AST, `grammar.Format` uses raw comments and layout metadata, does not
reconstruct ownership from positions, and must not move comments between
owners. Explicit `TypeGroup` and `TermGroup` source nodes keep comments and
boundary trivia inside parenthesized type and action expressions, distinguishing
the grouped node from its enclosed child. The formatter must preserve those owner paths while printing
parentheses. Parse/format/parse must preserve owner paths for supported nodes;
this guarantee does not apply to JSON-decoded ASTs without serialized layout.

Direct grammar JSON preserves normalized source metadata. The current `.pegoc`
AST-bearing codec does not encode source positions or comments; loading or
saving that AST preserves the compiled grammar tree, not its source-document
ownership. This distinction does not change the `.pegoc` bytecode format.

## Compatibility and Migration

Embedding `SourceInfo` preserves selectors but changes Go composite literals
that set `Pos` directly. Migrate AST builders, parser and generated-source
adapter, strict JSON schemas and fixtures, cloning, formatter tests, API docs and
documentation consumers together. Exact raw trivia remains an in-memory
formatting concern. Keep bytecode and runtime parsing behavior unchanged.

The first increment adds source spans and normalized ownership to the public AST,
nested JSON and manual parser while preserving diagnostics through
`DiagnosticPos`. The next increment must preserve parenthesized source structure
with source-only `TypeGroup` and `TermGroup` nodes. `TypeGroup` implements
`TypeExpr`; the `TermGroup.Term` field has type `Term`, and `TermGroup`
implements `Term`. Their source spans include the opening and closing
delimiters, while enclosed comments and layout stay owned within the group. Raw
layout is stored separately in `OpenBreak`, `BodyBreak`, `CloseBreak` and
`OneLine`; these fields preserve line placement around delimiters without
changing normalized source ownership. The compiler and generators lower these
wrappers transparently before semantic execution, so grouping adds no matching,
value-frame, type, or bytecode semantics. The canonical `pego.pego` parser must construct these nodes directly.
Then complete formatter owner-path preservation through both group types. The
boundary fixture is a focused oracle, not a substitute for grammar-parser parity
and formatter gates.

## Alternatives Considered

- **Duplicate comment fields on every node:** rejected in favor of one shared
  metadata contract.
- **Keep only formatter trivia:** rejected because clients would have to infer
  ownership from positions and layout.
- **Attach every group to the next node:** rejected because it loses trailing
  and detached distinctions.
- **Attach by physical order after parsing:** rejected because blank lines and
  ambiguous same-line groups affect ownership; construction must capture the
  boundary directly.
- **Use a node-identity side table:** rejected because copying, JSON and source
  adapters can lose the association.
- **Flatten JSON fields:** rejected in favor of an explicit nested `source`
  object.

## Testing

- Cover collector transitions, package/rule/field/list/expression owners, and
  nested `TypeGroup`/`TermGroup` owners; verify transparent lowering and retained
  source parentheses/comments in the parsed AST,
  CR/LF/CRLF, blank-line boundaries, adjacent blocks, empty comments, EOF and
  closing delimiters with and without a next owner.
- Assert exact normalized text/newlines, block indentation and marker handling,
  spans, diagnostic token locations, and no duplicate ownership.
- Round-trip JSON with and without `source`; verify strict and permissive
  decoders, zero-value omission and source metadata on every supported node.
- Parse/format/parse in-memory ASTs and compare owner paths while checking raw
  comment placement. Test relocation around grouping nodes.
- Test generated grammar construction, manual-parser parity, adapters, cloning,
  programmatic ASTs and documentation extraction.

## Performance and Limitations

Measure parser allocations/bytes and JSON size/marshal cost on comment-free,
comment-heavy and detached-comment inputs. Do not present this contract as an
optimization. The paired harness called `ParseGrammar`; the permanent
`BenchmarkSourceMetadata` uses the same inputs with the underlying source
parser. The baseline binary was built from 54551c2 before comparison, and the
candidate binary used the current source. The repository self-grammar
input has SHA-256
`5d66963a879cffe2de44165b4d47cef2487361e07f265807eb1f440c958d0b56`. Five
alternating paired samples used Go 1.27.1 on darwin/arm64, Apple M3 Max,
`GOMAXPROCS=16`, with 500 ms per sample. The medians and pretty-formatted JSON
sizes were:

| Input | Source bytes | Parse ns/op before → after | B/op before → after | Allocs/op before → after | JSON bytes before → after |
|:--|--:|--:|--:|--:|--:|
| Small grammar | 26 | 1,290 → 1,401 | 2,488 → 3,080 | 19 → 19 | 423 → 1,408 |
| Root `pego.pego` | 22,964 | 964,262 → 1,204,372 | 2,919,964 → 3,304,963 | 8,338 → 8,409 | 287,710 → 1,050,989 |
| Commented grammar (64 rules) | 5,848 | 79,332 → 103,200 | 138,608 → 175,768 | 1,172 → 2,068 | 8,527 → 44,612 |

These descriptive medians quantify source-parser and serialized-size costs for
the tested inputs; they are not a statistical-significance claim. They do not
measure parsing user input, JSON marshal latency, or runtime speed. Source spans
exclude leading/trailing boundary comments but include comments inside their
continuous interval; `DiagnosticPos` preserves a diagnostic token when it
differs from the span start. The package clause uses `PackageSource` while
retaining `Package` as its existing string field; the Operators Rule skip
clause uses `SkipSource`. Raw markers and comments with no normalized owner
remain formatter trivia and are not preserved by JSON.

The common public metadata shape, manual-parser ownership and spans, normalized
JSON fields, and `Grammar.Clone()` are implemented and validated. The full root
suite, dedicated metadata tests, parser generation/tests, site tests/build and
independent review pass. The boundary fixture validates grammar-level boundary
and selected owner construction only. The transparent Type/Action grouping increment is also implemented and
validated, including owned boundary comments, strict and permissive JSON,
structural validation and equivalent generated code and parse results.
Construction of common metadata in the canonical `pego.pego` grammar and
complete interior formatter owner-path migration remain follow-up scope. Keep the
overall record Proposed until those remaining parts of the contract are
implemented and validated.

### Parenthesized Source Groups: Incremental Cost

A focused comparison against the first source-metadata increment isolates the
additional source-group retention, using Go 1.27.1 on an Apple M3 Max
(darwin/arm64), `GOMAXPROCS=16`, and five alternating 500 ms samples per
workload. Both binaries read identical inputs. The self-grammar fixture is
22,868 bytes with SHA-256
`01aa2f4cab8192701792ddc6eadb8d38a2cff8fe9149bdfc7209f0b4ce5238d9`;
it predates the pragma-adjacency change. These are source parsing and pretty
AST JSON measurements, not input parsing with a compiled grammar.

| Workload | Median Before → After (ns/op) | B/op Before → After | Allocations Before → After |
|:--|--:|--:|--:|
| Small | 1,573 → 1,517 | 3,080 → 3,080 | 19 → 19 |
| Self grammar | 1,270,874 → 1,336,694 | 3,304,893 → 3,305,511 | 8,409 → 8,417 |
| Comment-heavy | 119,783 → 105,870 | 175,768 → 175,768 | 2,068 → 2,068 |

The self-grammar gains 618 B/op, eight allocations, and 1,604 bytes in pretty
JSON (1,050,885 → 1,052,489 bytes). Small and comment-heavy allocation and JSON
sizes are unchanged. The unaffected time controls drift by -3.6% and -11.6%,
so the self-grammar's +5.2% median timing difference does not establish an
attributable speed change. Runtime matching and bytecode semantics are
unchanged; grouped and ungrouped grammars have equal generated Go/TypeScript
code and parsing results in the focused parity checks. Raw measurements remain
outside Git.
