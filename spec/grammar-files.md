# Grammar Files

A grammar is a set of type definitions, ordinary rule definitions, and
Operators Rule declarations. It is normally written in a file with the
extension `.pego`, built from the tokens of [Lexical Structure](lexical.md).
This chapter defines what a grammar file may contain, the rules for names and
the order of definitions, and the errors that make a grammar invalid.

## Structure

A grammar file consists of an optional package clause followed by type
definitions, ordinary rules, and Operators Rule declarations in any order.

```pego
package example
type Number terminal
rule main Number = [0-9]+
rule expr Expr = operators { ... }
```

A hard newline separates top-level statements; a semicolon may also separate
them. Declaration position and indentation do not affect parsing. The 12
declaration and Operators Rule words are contextual, so the same spellings can
be identifiers where a name or expression is expected.

A multiline parsing expression must be parenthesized. A delimiter opened by
the expression, such as parentheses around a PEG expression or braces around a
Predicate Action or Value Action block, permits its contents to span lines. An outer
Operators Rule brace does not by itself make an operand or operator expression
multiline. Commas separate struct fields; semicolons and statement newlines
separate action-block statements.

An empty file is a valid grammar with no rules, but it cannot be used to parse
anything, because there is no start rule to select.

### Package Clause

`package <name>` names the package of the grammar, with an identifier. If
present, it MUST be the first item of the file. The package name does not
affect parsing. The reference implementation records it, writes it back when
it formats the grammar, and stores it in compiled grammars; the package name
of generated Go code is given when the code is generated.

### Grammar AST Source Metadata

Source-bearing public grammar nodes expose a `SourceInfo` with `Pos` and `End`
for one continuous half-open source interval. The interval excludes leading
and trailing comments owned at its boundaries but includes comments inside it.
The metadata also includes normalized `LeadingComments`,
`TrailingComments` and `LeadingDetachedComments`, and an optional
`DiagnosticPos`. `End` is exclusive. `Diagnostic()` returns `DiagnosticPos`
when present and otherwise `Pos`, preserving token-specific error locations
when the span begins earlier. Each such node provides `Source() *SourceInfo`
and implements `SourceNode` for generic inspection. Programmatically built AST
nodes may leave source metadata zero-valued.

`Grammar.Package` remains the package name; `Grammar.PackageSource` carries the
package clause metadata. `OperatorsRule.SkipSource` carries metadata for the
whole skip clause, from the `skip` keyword through its expression.
`Grammar.Clone()` deep-copies source metadata, including
detached-comment storage; `SourceInfo` itself does not expose a clone method.

Normalized ownership is serialized in explicit `source`, `packageSource` and
`skipSource` objects. Exact comment tokens and formatter layout are not part of
those objects. JSON-decoded grammar ASTs therefore do not guarantee lossless
source formatting. Normalized comment strings preserve content whitespace and
line endings; each detached entry is one paragraph. Older JSON may omit these
metadata objects. Comment grouping and node ownership follow the
[source-metadata contract](../docs/design/057-source-info-and-comment-ownership.md).

#### Parenthesized Groups in the Public AST

The public AST preserves explicit parentheses in type expressions as
`TypeGroup{Type: ...}` and in action and predicate expressions as
`TermGroup{Term: ...}`. Both nodes embed `SourceInfo`. A group is transparent
to type resolution, value construction and predicate evaluation; it preserves
the source form for inspection and formatting.

`grammar.MarshalJSON` serializes these nodes as interface objects tagged with
`"@type": "TypeGroup"` or `"@type": "TermGroup"`, including their `type` or
`term` child and `source` metadata. The formatter's `OpenBreak`, `BodyBreak`,
`CloseBreak` and `OneLine` fields retain comment and layout details in memory
but are excluded from JSON. Consequently, JSON preserves the explicit grouping
and normalized source metadata, while formatting after JSON decoding may
normalize layout and is not guaranteed to reproduce exact source text.

`pego.ParseGrammar` returns this source-preserving AST. Compilation removes
source-only type and term groups from the executable grammar; consequently,
`Parser.Grammar()` and the AST embedded in a compiled grammar expose the
normalized executable form rather than those grouping nodes.

### Type Definitions

A type definition (`type`) defines a node type: a struct type, a union or alias
of types, or a terminal type. The forms and their meaning are defined in the
[Type System](types.md#type-definitions).

### Rule Definitions

An ordinary rule definition starts with `rule` and defines a name for a
parsing expression. Its form is `rule name [Type] = expression [=> action]`.
When the rule matches, it produces a single value. The 12 grammar words may be
used as names when the surrounding syntax expects a name.

- The optional type after the rule name declares the type of the value the
  rule produces. If it is omitted, the type is inferred (see
  [Type Inference](type-checking.md#type-inference)).
- The expression after `=` is a [parsing expression](parser-expressions.md).
  An optional `=> action` builds the rule value from what the whole expression
  matched; it cannot be attached to an individual alternative of a choice.
- An Operators Rule is a distinct rule declaration written
  `rule name [Type] = operators { ... }`. It is not an ordinary PEG
  expression. Its operands and operator entries may have their own actions,
  as defined in [Operators Rule](operators-rule.md). A declaration-level
  action may be omitted or use the explicit `=> ...` default-CST form; any
  other declaration-level action is rejected at compile time.

Comments may occur at top-level statement boundaries and at statement or
field boundaries inside braces. They are also permitted inside parsing and
action expressions, including predicates and function arguments. The parser
records comment trivia and may accept it syntactically. Semantic analysis
rejects comments in declaration and Operators Rule statement headers with a
positioned diagnostic; affected headers include package, type, rule, level,
infix-kind and associativity headers.

A rule is called by writing its name in a parsing expression at the current
position (see [Rule Calls](parser-expressions.md#rule-calls)).

## Names

Rule names and type names live in separate namespaces. Each rule name MUST be
defined at most once and each type name MUST be defined at most once. A rule
MAY refer to rules defined later in the file, and a type MAY refer to types
defined later: **the order of definitions has no effect on the meaning of the
grammar**. Every rule that a parsing expression names MUST be defined.

The names that a definition can use are constrained as follows:

| Kind of Name | Constraint |
|:--|:--|
| Rule | An [identifier](lexical.md#identifiers). Contextual words are allowed when the grammar expects a name. Rule names commonly begin with a lowercase ASCII letter. |
| Type, field | An identifier that begins with an uppercase ASCII letter. Built-in and reserved type names MUST NOT be defined. See [Names](types.md#names). |
| Capture label, variable, level, lambda parameter | An identifier. Captures and variables have their own scopes; see [Capture](parser-expressions.md#capture) and [Variables](predicates.md#variables). A level name MUST be unique within its Operators Rule declaration. |

## Start Rule

Parsing begins with a start rule, which is not named in the grammar file: it
is selected when the grammar is compiled or when parsing is started. See
[Start Rule](parsing.md#start-rule).

## Errors in a Grammar

A grammar with an error cannot be used to parse. Each error is reported with
the line and column of the construct it concerns in the grammar source (see
[Source Text](lexical.md#source-text)). The classes of error are:

| Class | Examples | Defined In |
|:--|:--|:--|
| Lexical and syntax errors | An unterminated string, an unknown escape, a character that starts no token, a missing `)`, a malformed declaration, a repetition whose maximum is less than its minimum | [Lexical Structure](lexical.md), [Grammar Syntax](syntax.md) |
| Definition and reference errors | A name defined twice, a reserved or non-uppercase-ASCII type name, an undefined rule, a call with an unknown level, a capture of an expression without a value, an unknown attribute or pragma, a header comment, a `#stream` outside its permitted position | The chapter of the construct |
| Type errors | A field that the struct does not declare, a value that is not assignable to the declared type, `$n` out of range | [Type Checking](type-checking.md#checked-errors) |

The reference implementation reports all lexical and syntax errors of a file
(at most 100), then, if there are none, all definition and reference errors,
and then, if there are none, the type errors. This file has three definition
and reference errors:

```pego
rule main = item $$
rule item = k:"a" => $v
rule other = unknown
rule main = "b"
```

Parsing with it (`pego parse -g bad.pego -i a`) reports:

```
pego: bad.pego:4:1: rule main is already defined
bad.pego:2:22: undefined capture $v
bad.pego:3:14: undefined rule unknown
```

## Grammar Representations

Besides `.pego` source files, a grammar can be represented as JSON, which is
the abstract syntax tree of the grammar (the public package `grammar`), and as
a compiled grammar file (`.pegoc`), which holds the bytecode of the grammar
and, optionally, the tree (see [File Format](bytecode.md#file-format)). All
representations describe the same language. The `pego convert` command
converts between them, and `pego fmt` formats `.pego` source files, keeping
comments.

Direct grammar JSON can preserve normalized source positions and comment
ownership. The optional AST in the current `.pegoc` format does not encode that
metadata or formatter trivia; it preserves the compiled grammar tree only.
This distinction does not change the `.pegoc` format or version.

JSON decoding requires one complete document and rejects trailing
non-whitespace data. JSON and hand-built ASTs must have nonnil required
children and collection entries, nonnegative positional references, and valid
repetition bounds. Pointer cycles and typed nil interface nodes are rejected.
The public `grammar.Validate` function checks this structure before semantic
compilation; see [building grammars as ASTs and JSON](../docs/guide/grammar-asts.md)
for its diagnostics and compatibility rules. Unknown JSON fields remain
ignored.
