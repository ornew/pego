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
raw comment tokens remain available to the in-memory formatter.

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
}
```

Embed this value in source-bearing declarations, fields, expressions, actions,
attributes, pragmas, Operators Rule entries and explicit source grouping nodes.
`End` is exclusive; both positions use the existing line/column units and cover
syntax tokens, not comments. Leading/trailing values are normalized strings;
each detached entry is one comment paragraph. Normalized comment text preserves
newlines and conventional whitespace after comment markers. Empty values are
zero values and may be omitted in JSON. Clone detached slices rather than
sharing mutable backing storage.

Preserve diagnostic token positions separately: migrating a node's existing
`Pos` to its source-span start must not change which token an error reports. The
implementation must choose and document a dedicated diagnostic-position field.
Package syntax also needs real source metadata; the current package name and
`PackagePos` alone are insufficient. Decide whether to add a package AST node or
another public source-bearing representation during migration.

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
focused matrix covers 33 boundary cases across three backends and two position
units, plus direct package/rule construction and a generated typed Go consumer. Detached values use typed `[]CommentText` records; an adapter copies
`Text` into normalized strings. This validates grammar-level ownership without
semantic attachment, but does not implement public `SourceInfo`, JSON, or the
canonical root grammar migration. General typed-list construction is separate
and is not required for this design.

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
the delimiters, retain first-line content, and on later lines strip indentation
and the first `*` marker. Preserve encountered newlines. Raw trivia retains exact
markers and original layout separately.

### JSON and Formatting

Serialize normalized `SourceInfo` in an explicit nested `source` object; do not
rely on Go embedding to flatten fields. Keep AST type tags and position
units. Older JSON without `source` decodes to zero metadata. Example:

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
owners. Existing relocations—including comments around type and action groups—must
remain within ownership-equivalent boundaries. Parse/format/parse must preserve owner paths for
supported nodes; this guarantee does not apply to JSON-decoded ASTs without
serialized layout.

## Compatibility and Migration

Embedding `SourceInfo` preserves selectors but changes Go composite literals
that set `Pos` directly. Migrate AST builders, parser and generated-source
adapter, strict JSON schemas and fixtures, cloning, formatter tests, API docs and
documentation consumers together. Exact raw trivia remains an in-memory
formatting concern. Keep bytecode and runtime parsing behavior unchanged.

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

- Cover collector transitions, package/rule/field/list/expression/group owners,
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
optimization. Source spans exclude comments; diagnostic token positions remain
separate. Package metadata representation and the dedicated diagnostic-position
field must be settled during implementation. Raw markers and comments with no
normalized owner remain formatter trivia and are not preserved by JSON.

This record is Proposed. The focused fixture validates grammar-level boundary
and selected owner construction only; public Go metadata, JSON serialization,
formatter migration and the canonical root grammar remain unimplemented.
