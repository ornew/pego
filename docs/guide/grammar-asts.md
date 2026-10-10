# Building grammars as ASTs and JSON

Use package `grammar` when a tool builds a grammar in Go or loads its JSON
representation. `pego.Compile` checks it and returns a parser; Go and
TypeScript generation use the same compiler.

## Validate a hand-built grammar

`grammar.Validate(g)` checks the structure before compilation or formatting.
It returns the first `*grammar.ValidationError` without changing the AST:

```go
g := &grammar.Grammar{Statements: []grammar.Statement{
    &grammar.RuleDef{Name: "main", Expr: &grammar.Optional{}},
}}
err := grammar.Validate(g)
var problem *grammar.ValidationError
if errors.As(err, &problem) {
    fmt.Println(problem.Path) // $.statements[0].expr.expr
}
```

Import `errors`, `fmt` and `github.com/ornew/pego/grammar` for this snippet.
`Path` uses JSON field names and zero-based indexes. `Pos` is the node's
known source position, or the closest enclosing position; JSON does not
serialize positions. `Msg` describes the problem.

Validation rejects a nil grammar, missing required children, nil entries in
node collections, interfaces holding typed nil pointers, unsupported embedded
node types, pointer cycles, negative positional references and malformed
repetition and character-range bounds. A repetition's minimum must be nonnegative; a finite
maximum must be at least the minimum. Any negative maximum means unbounded,
as documented by `grammar.Repeat`. Shared subtrees, empty collections and
genuinely nil optional children remain allowed.

A character range's `Lo` and `Hi` must be Unicode scalar values (0 through
0x10FFFF except 0xD800–0xDFFF), with `Lo <= Hi`. A valid range can span the
surrogate interval; overlapping ranges and empty AST classes retain their
existing meanings. Invalid endpoints are reported at paths such as
`$.statements[0].expr.ranges[0].lo`. This also applies to JSON intake,
compilation and generation. Remove unreachable surrogate exclusions from
older grammars and recompile AST-bearing saved parsers that contain them.

A matching `grammar.Literal.Value` uses its decoded Unicode rune sequence. Each malformed UTF-8 byte
becomes U+FFFD, so a value containing `"\xc3"` matches a replacement character and cannot match the
leading byte of `"é"`. Both position units, saved modules and generated Go/TypeScript share this
rule. Compile and generation derive matching data without rewriting the caller's AST; binary
serialization preserves the original literal value. JSON strings follow `encoding/json`'s UTF-8
replacement behavior. String constants in actions are separate from matching literals.

Match text follows the input: byte positions preserve consumed source bytes, while code-point
positions return decoded text. For example, the same raw-invalid-byte literal can match either
valid U+FFFD or an invalid input byte, but byte-mode `text($capture)` distinguishes their spellings.
Rebuild generated parsers from older invalid-byte ASTs to update their embedded matching data;
existing full and AST-omitted binary modules gain the corrected comparison when loaded.

Both VMs and generated parsers honor negative unbounded maxima. Saving a
compiled grammar preserves the original maximum in its AST and uses the
standard unbounded representation in its bytecode. Recompile older artifacts
created from noncanonical negative maxima to replace incorrectly narrowed
or rejected bytecode.

This is structural validation. It does not resolve names, check action types,
validate attributes or enforce Pratt semantics; compile the grammar for those
checks. It also does not validate source comments/layout or impose resource
limits. `grammar.Format` and `grammar.MarshalJSON` expect structurally valid
input; call `Validate` first for ASTs your tool constructs. The validator walks
iteratively, though later formatting, serialization and compilation have
their own nesting costs. Do not mutate the AST concurrently with these calls.

## Load JSON

`grammar.UnmarshalJSON(data)` accepts exactly one complete JSON value,
followed by optional whitespace, and runs structural validation before
returning a grammar. It rejects JSON `null` as a grammar, trailing values or
garbage, and numeric fields that overflow their Go destination type.
For example, a null struct field reports
`$.statements[0].spec.fields[0]` rather than panicking during compilation.

The representation uses the AST fields' `json` tags. Each interface node has
an `"@type"` member, such as `"RuleDef"` or `"Literal"`; source positions and
layout are omitted. Unknown fields are ignored and duplicate object keys
follow `encoding/json`'s behavior. There is no opt-in strict decoder yet.

After loading, use `pego.Compile(g, "main")` to perform semantic checking.
Compile and the generators also run structural validation themselves, including
the reconstruction of an AST-bearing compiled grammar. A compiled file's
existing trust and bytecode validation rules still apply; see
[compiled grammars](compiled-grammars.md).
