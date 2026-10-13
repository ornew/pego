# Building Grammars as ASTs and JSON

Use package `grammar` when a tool builds a grammar in Go or loads its JSON
representation. `pego.Compile` checks it and returns a parser; Go and
TypeScript generation use the same compiler.

## Validate a Hand-Built Grammar

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
`Path` uses JSON field names and zero-based indexes. `Pos` is the closest known
diagnostic position. Source-parsed grammars carry source spans and comment
ownership in `SourceInfo`; hand-built nodes may leave those fields zero-valued.
`Msg` describes the problem.

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
validate attributes or enforce Operators Rule semantics; compile the grammar for those
checks. It also does not validate source comments/layout or impose resource
limits. `grammar.Format` and `grammar.MarshalJSON` expect structurally valid
input; call `Validate` first for ASTs your tool constructs. The validator walks
iteratively, though later formatting, serialization and compilation have
their own nesting costs. Do not mutate the AST concurrently with these calls.

Integer constants use Go's native `int` range. Formatting the minimum integer
uses `-MaxInt - 1`, with parentheses where required: its absolute value cannot
be a positive source token. Parsing the formatted grammar preserves the value,
though that constant becomes a subtraction expression in the source AST.

## Print a Simplified Syntax View

Use `grammar.FormatSyntax(g)` for documentation that shows parsing syntax.
It leaves the input AST unchanged and omits type declarations, result types,
Value Actions, metadata attributes and recovery wrappers. Predicate Actions,
captures and other matching pragmas remain, including `#repeat`, `#atomic`
and `#stream`. `grammar.Format(g)` continues to print the full grammar.

The command-line form writes to standard output:

```bash
pego fmt -syntax grammar.pego
```

`-syntax` cannot be combined with `-w` or `-l`. The view is not a replacement
parser: predicates that depend on values constructed by omitted actions or
types may require the full grammar. Returned values and recovery diagnostics
are not preserved. Comments on retained forms follow the formatter's layout
rules; comments attached only to omitted forms may be omitted.

## Load JSON

`grammar.UnmarshalJSON(data)` accepts exactly one complete JSON value,
followed by optional whitespace, and runs structural validation before
returning a grammar. It rejects JSON `null` as a grammar, trailing values or
garbage, and numeric fields that overflow their Go destination type.
For example, a null struct field reports
`$.statements[0].spec.fields[0]` rather than panicking during compilation.

The representation uses the AST fields' `json` tags. Each interface node has
an `"@type"` member, such as `"RuleDef"` or `"Literal"`. Parsed source metadata
is serialized under `source`, `packageSource` or `skipSource`; formatter layout
and exact comment tokens are omitted. Unknown fields are ignored and duplicate object keys
follow `encoding/json`'s behavior. Use `grammar.DecodeJSON(data)` when the
input should reject unknown fields and duplicate keys at every object. Its
`*grammar.JSONDecodeError` reports a JSON path and zero-based byte offset for
those strictness errors; malformed JSON wraps the standard syntax error. Both
entry points check one complete value and run structural validation, while
the legacy `UnmarshalJSON` signature and permissive behavior remain unchanged.

Source `@` attributes use `Attributed` and `Attribute`: `args` is an ordered list
of integers, strings, booleans, null slots or expression nodes, and `kwArgs`
maps argument names to the same value forms. For example, `@custom(2,)`
preserves `[2, null]`, whereas `@custom(2)` preserves `[2]`. Integer argument
values, including the native minimum, survive source formatting and parsing. Keyword arguments
follow positional arguments in source. Strict JSON decoding also rejects
repeated keyword keys.

## Read Source Metadata

Source-bearing grammar nodes embed `grammar.SourceInfo` and satisfy
`grammar.SourceNode`, so tools can inspect metadata without a type switch:

```go
func report(n any) {
    if sourced, ok := n.(grammar.SourceNode); ok {
        source := sourced.Source()
        fmt.Println(source.Pos, source.End, source.LeadingComments)
        fmt.Println("diagnostic token:", source.Diagnostic())
    }
}
```

`Pos` and `End` are the node's half-open source span; `End` is exclusive. The
span excludes leading and trailing comments owned at its boundaries, but
includes comments inside that continuous source interval.
`LeadingComments` and `TrailingComments` hold normalized text, and each entry
in `LeadingDetachedComments` is one paragraph. Comment markers are removed;
line endings and content whitespace are retained according to the grammar
comment-normalization contract. `DiagnosticPos` preserves a diagnostic token
that differs from the span start; `Diagnostic()` returns it when valid and
otherwise returns `Pos`.

The grammar root keeps its package name in `Package` and the clause metadata in
`PackageSource`. An Operators Rule's `SkipSource` holds metadata for the whole
skip clause, from its `skip` keyword through the expression. `Grammar.Clone()`
returns an independent copy of the supported grammar AST and attribute
argument values, including detached-comment slices.
Shared AST subtrees remain shared within the clone, and AST pointer cycles are
preserved for `grammar.Validate` to report. Arbitrary application objects or
cyclic attribute maps and slices are outside the clone contract.

`pego.ParseGrammar` returns the source AST with its source metadata. By
contrast, `Parser.Grammar()` exposes the normalized grammar retained by the
compiled parser. Normalization can lower groups and pragmas, so that grammar is
not a complete record of source documentation ownership.

JSON stores normalized source ownership, not raw formatter trivia or layout.
An AST decoded from JSON therefore cannot promise lossless source formatting.
Keep the original parsed AST when a tool needs to preserve exact comment
placement.

Direct grammar JSON includes normalized source metadata. The current `.pegoc`
AST codec does not serialize source positions or comments, so an AST-bearing
compiled grammar preserves the executable tree but not source-documentation
ownership. This behavior does not change the `.pegoc` format.

Source-only syntax has dedicated AST nodes. An explicit PEG grouping is `Group{Expr}`; formatting and JSON preserve the group, while compilation normalizes it away before matching. The source-aware AST retains the group body, closing-delimiter comments and layout. JSON retains normalized source positions and ownership but omits formatter layout and exact raw comment tokens. A braced action sequence is `Block{Statements}`. Predicate Actions with one statement continue to use the existing `Predicate.Term` node and retain their existing executable semantics. Empty or multi-statement Predicate Action blocks and all braced Value Actions can be parsed, formatted and serialized, but semantic checking rejects them as unsupported; their statements do not execute.

Source `#` pragmas use dedicated expression nodes: `#repeat` is `Repeat`,
`#atomic` is `Atomic`, `#recover` is `Recovery`, and `#stream` is `Stream`.
Their payloads follow each pragma's own syntax. Compilation lowers recovery
and streaming into its executable representation without changing the source
AST. An `InlineAction` node can be formatted and serialized, but compilation
rejects it until inline action execution is implemented.

After loading, use `pego.Compile(g, "main")` to perform semantic checking.
Compile and the generators also run structural validation themselves, including
the reconstruction of an AST-bearing compiled grammar. A compiled file's
existing trust and bytecode validation rules still apply; see
[compiled grammars](compiled-grammars.md).
