# Grammar Files

A grammar file uses the tokens defined in [Lexical Structure](lexical.md). This
chapter defines its structure, the rules for names and definition order, and
the errors that make a grammar invalid.

## Structure

A grammar file, normally written as a `.pego` file, consists of an optional
package clause followed by a list of Declaration Statements in any order: Type
Declaration Statements `type ...`, Variable Declaration Statements `var ...` and
Rule Declaration Statements `rule ...`.
Type Declaration Statements use Type Expressions; Rule Definitions combine a
Rule Expression with an optional Value Action containing an Action Expression.
An Operators Rule is a specialized Rule Definition.

```pego
package example
type Number = node[string]
rule main Number = ([0-9]+) #atomic
rule expr Expr = operators { ... }
```

A hard newline separates top-level Declaration Statements; a semicolon may also
separate them. Definition position and indentation do not affect parsing. The 13
contextual grammar words are interpreted by position, so the same spellings can
be identifiers where a name or Rule Expression is expected.

A multiline Rule Expression must be parenthesized. A delimiter opened by the
expression, such as grouping parentheses or braces around a Predicate Action
or Value Action block, permits its contents to span lines. An outer
Operators Rule brace does not by itself make an operand or operator expression
multiline. Semicolons and statement newlines separate Declaration Statements,
Operators Rule items, struct fields and action-block statements. A newline ends a
statement, so a semicolon on a later line than the statement it follows, or a second
semicolon, is an empty statement, which is an error everywhere. The `{` of a type
definition stays on the line of its `=`, and an action or action block stays on the
line of its `=>`, unless a delimiter encloses both. In an action, a newline before a
binary operator, `.`, `=>`, a constructor `{` or an index `[` ends the expression
unless a delimiter encloses it, and the `[` of an index follows its operand directly,
without white space or a comment.

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

The package clause is `File.Pkg`: `File.Pkg.Name` is the package name, and
`File.Pkg.SourceInfo` carries the clause metadata. `OperatorsRule.SkipSource` carries metadata for the
whole skip clause, from the `skip` keyword through its expression.
`Grammar.Clone()` deep-copies source metadata, including
detached-comment storage; `SourceInfo` itself does not expose a clone method.

Normalized ownership is serialized in explicit `source` and `skipSource` objects. Exact comment tokens and formatter layout are not part of
those objects. JSON-decoded grammar ASTs therefore do not guarantee lossless
source formatting. Normalized comment strings preserve content whitespace and
line endings; each detached entry is one paragraph. Older JSON may omit these
metadata objects. Comment grouping and node ownership follow
[Comment Grouping and Ownership](#comment-grouping-and-ownership).

The canonical `pego.pego` grammar and its source adapter assign the same spans
and owners as the hand-written frontend to every source-bearing node: the
package clause, type and rule declarations, struct fields, operators lines and
constructor field initializers, and the operands, member markers and closing
delimiters of rule expressions, value expressions and types. Each statement or
list element records its own trailing group and the lead (leading group,
detached groups and whether an owner follows) of the next element; a list's
first element takes its lead from the boundary after the opening delimiter. The
grammar keeps the groups of every comment-bearing gap between two tokens, and
the adapter gives them to the outermost node that starts (leading and detached
groups) or ends (trailing group) at that token. `SourceInfo.Layout` is not
constructed by the canonical grammar.

`SourceInfo.Layout` is optional raw formatter state. Its `Before` field retains
comment tokens at a syntax boundary, while `PreviousLine` and `NextLine` retain
the surrounding line relationship. Its `After` field retains the comments
between a list element and the comma that follows it. A nil layout uses canonical formatting.
`grammar.Format` uses stored layout tokens when present, so changing normalized
comment strings alone does not change their formatted spelling or placement.
`Grammar.Clone()` copies the layout and its comment tokens. JSON omits layout;
layout alone does not create a `source` object. Current layout preservation
covers selected Action Expression operand boundaries, including unary operands
and confirmed lambda expressions, plus constructor field names and the
elements of calls, constructors, list constructors and tuples. The delimiters of
those forms keep their comments in the node's `OpenBreak`, `BodyBreak` and
`CloseBreak` fields. Other source forms remain outside this increment's coverage.

#### Comment Grouping and Ownership

A comment is a `//` line comment or a `/* */` block comment. The comments between
two tokens are grouped, and each group is assigned to at most one owner: it trails
the previous token, leads the next token, or is detached. The following rules
decide it.

| Event between two tokens | Effect |
|:--|:--|
| Start of the file | No previous token exists, so no group can trail |
| A comment on the line of the previous token | It forms a group at once and is the only group that can trail that token. A comment-only run that follows is a new group; it never extends the inline comment |
| Consecutive `//` comments | One group |
| A block comment | It starts a distinct group and ends any pending group. Switching between block and line comments also ends the pending group. An empty block comment such as `/**/` is a group |
| A blank line | Ends the pending group, and no later group can trail the previous token |
| A group ends while trailing is possible | The first such group trails the previous token and later groups are detached |
| A group ends while trailing is not possible | It is detached; the last group that no blank line ended leads the next token |
| The next token is on the line of the previous token, or a block comment that could trail ends on the line of the next token | If exactly one group exists, it is detached and not guessed to trail or lead. With several groups, the first trails the previous token, the groups in between are detached and the last leads the next token |
| A closing delimiter or the end of the file | The same rules apply. Groups are detached even when no next token exists; they have no owner, and the formatter keeps their original tokens |

Ownership then follows the construct:

- The leading and detached comments of a declaration belong to the declaration, not
  to its name or its body. A trailing comment after its last token belongs to the
  declaration.
- A comment before a list element belongs to the element, not to the separator that
  precedes it.
- A comment at the boundary of a nested expression belongs to the expression that
  contains the boundary.
- A parenthesized group owns the comments adjacent to its delimiters.
- A capture, an atomic expression or an attributed expression does not duplicate
  the ownership of its inner expression only because their spans share a boundary.
- The grammar parser and the AST built by hand-written code have the same owners.

Normalized comment text removes only the `//` marker of a line comment and keeps the
following white space and the final newline. For a block comment it removes the outer
delimiters and keeps the content of the first line; on later lines it removes the
indentation and the first `*`. Newlines and nested comment delimiters stay in the
text. The text is not a copy of every white space detail of the source; exact
comment tokens and layout belong to `SourceInfo.Layout`.

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

#### Comma-Separated Lists

`grammar.Format` writes these comma-separated lists in brackets in one of two
forms: the arguments of a call, the elements of a struct constructor, node
constructor, list constructor, tuple value and tuple type, the arguments of a
type application `T[A, B]`, and the arguments of an attribute `@name(k=v, ...)`
that ends in a keyword argument.

- On one line, `f(1, 2)`, when the list has no trailing comma and no comment that
  needs its own line.
- One element per line, each followed by a comma, the last one included, when
  the list has a trailing comma after its last element, or a comment that needs
  its own line: a line comment, a comment on a line of its own, or a block
  comment over several lines. The elements are indented one level inside the
  delimiters.

```text
f(1, 2)
f(
    1,
    2,
)
```

The comma that a tuple of one element requires, as in `(x,)`, is not a trailing
comma: a tuple needs a trailing comma and two or more elements to be written on
lines. An element that is written over several lines, such as a nested list with
a trailing comma, puts the list around it on lines too. A block comment on the
line of the token before it, as in `f(/* a */ 1, 2)` or `f(1 /* a */, 2)`, stays
there and keeps the list on one line, because moving it would change which
element owns it; the comments of a list on lines keep their place relative to
the elements and commas. Lambda parameters, `#repeat` bounds and `#recover` take
no trailing comma and keep their form.

A trailing comma is not part of the normalized grammar: it is kept in the
`TrailingComma` field of `TupleType`, `TypeApp`, `TupleLit`, `New`,
`ListConstructor`, `Call` and `Attribute`, which JSON omits, so a grammar decoded
from JSON formats every list on one line.

`pego.ParseGrammar` returns this source-preserving AST. Compilation removes
source-only type and term groups from the executable grammar; consequently,
`Parser.Grammar()` and the AST embedded in a compiled grammar expose the
normalized executable form rather than those grouping nodes.

### Type Declaration Statements

A Type Declaration Statement `type ...` declares a type name. It is a Type
Definition `type T = S`, which defines a new type from a struct type `{...}` or a
node type `node[U]`, or a Type Alias `type T ~ U`, which gives another name to a type.
Both are defined in the [Type System](types.md#type-declaration-statements).

### Variable Declaration Statements

A Variable Declaration Statement `var ...` declares a parser variable: `var
<name> [<type>] [= <value>]`. At least one of the type and the value is
required.

```pego
var depth int = 0
var mode string
var seen = false
```

- The type is `int`, `string` or `bool`; any other type is an error.
- The value is an int, string or bool literal (an int literal may be negated);
  `nil` is not allowed. `=` defines the initial value and is not a
  binding. Without a value the variable starts as the zero value of its
  type: `0`, `""` or `false`. Without a type, the type is the type of the
  value. A value of another type than the declared type is an error.
- Every parse, through every start rule and backend, starts with each declared
  variable bound to its initial value.

Actions bind and read declared variables; see
[Variables](predicates.md#variables). A name that is not declared cannot be
read or bound.

### Rule Declaration Statements

A Rule Declaration Statement `rule ...` declares a rule. Its form is a Rule
Definition `rule <name> [<Type Expression>] = <Rule Expression> [=> <Action
Expression>]`, which defines the name for a Rule Expression.
When the rule matches, it produces a single value. The 13 grammar words may be
used as names when the surrounding syntax expects a name.

- The optional Type Expression after the rule name declares the type of the
  value the rule produces. If it is omitted, the type is inferred (see
  [Type Inference](type-checking.md#type-inference)).
- The Rule Expression after `=` is defined in
  [Rule Expressions](parser-expressions.md). An optional Value Action (`=>`)
  uses its Action Expression to build the value from the whole match; it cannot
  be attached to an individual alternative of a choice.
- An Operators Rule is a specialized Rule Definition written
  `rule <name> [<Type>] = operators { ... }`. It defines operands and operator
  entries rather than an ordinary Rule Expression; entries may have Value
  Actions as described in [Operators Rule](operators-rule.md). A Value Action
  after the closing brace may be omitted or use the explicit `=> ...`
  default form; any other such action is rejected at compile time.

Comments may occur at top-level Declaration Statement boundaries and at statement
or field boundaries inside braces. They are also permitted inside Rule
Expressions and Action Expressions, including predicates and function arguments.
The parser
records comment trivia and may accept it syntactically. Semantic analysis
rejects comments in Declaration Statement and Operators Rule statement headers with a
positioned diagnostic; affected headers include package, type, rule, level,
infix-kind and associativity headers.

A rule is called by writing its name in a Rule Expression at the current
position (see [Rule Calls](parser-expressions.md#rule-calls)).

## Names

Rule names, type names and variable names live in separate namespaces. Each
rule name MUST be defined at most once, each type name MUST be defined at most
once and each variable MUST be declared at most once. A rule
MAY refer to rules defined later in the file, and a type MAY refer to types
defined later: **the order of definitions has no effect on the meaning of the
grammar**. Every rule named by a Rule Expression MUST be defined.

The names that a definition can use are constrained as follows:

| Kind of Name | Constraint |
|:--|:--|
| Rule | An [identifier](lexical.md#identifiers). Contextual words are allowed when the grammar expects a name. Rule names commonly begin with a lowercase ASCII letter. |
| Type, field | An identifier that begins with an uppercase ASCII letter. Built-in and reserved type names MUST NOT be defined. See [Names](types.md#names). |
| Variable | An identifier, declared by a Variable Declaration Statement. See [Variables](predicates.md#variables). |
| Capture label, level, lambda parameter | An identifier. Captures have their own scope; see [Capture](parser-expressions.md#capture). Variables are the only names a binding `name := expr` may bind. Lambda parameters are variables of the lambda body; see [Built-In Functions](actions.md#built-in-functions). A level name MUST be unique within its Operators Rule declaration. |

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
| Definition and reference errors | A name defined twice, a variable that is read or bound but not declared, a variable declared with an unsupported type or an initial value of another type, a reserved or non-uppercase-ASCII type name, an undefined rule, a call with an unknown level, a capture of an expression without a value, an unknown attribute or pragma, a header comment, a `#stream` outside its permitted position | The chapter of the construct |
| Type errors | A field that the struct does not declare, a value that is not assignable to the declared type | [Type Checking](type-checking.md#checked-errors) |

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

```text
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
compilation. `grammar.ValidateContext(ctx, g)` checks the same structure and
error order, but may return `ctx.Err()` without a partial diagnostic when its
context is canceled. Neither function changes the AST. Unknown JSON fields
remain ignored.

### Validation of a Grammar AST

`grammar.Validate(g)` checks the structure of a grammar AST before compilation or
formatting and returns the first `*grammar.ValidationError` in a deterministic
traversal, without changing the AST. `grammar.ValidateContext(ctx, g)` performs the
same checks in the same order and returns `ctx.Err()`, without a partial
diagnostic, if it observes cancellation. Cancellation is cooperative and is not a
bound on latency. Both functions walk iteratively, impose no size or depth limit,
and MUST NOT run concurrently with a mutation of the AST.

A `ValidationError` has a `Path` that uses JSON field names and zero-based indexes
(`$.file.decls[0].expr.expr`), the closest known diagnostic position `Pos`, and a
message `Msg`.

| Rejected | Allowed |
|:--|:--|
| A nil grammar; a missing required child; a nil entry in a node collection | Shared subtrees; empty collections; genuinely nil optional children |
| An interface that holds a typed nil pointer; an unsupported embedded node type | |
| A pointer cycle | |
| A negative positional reference | |
| A repetition whose minimum is negative, or whose finite maximum is smaller than the minimum | Any negative maximum, which means unbounded |
| A character range whose `Lo` or `Hi` is not a Unicode scalar value (0 through 0x10FFFF except 0xD800 to 0xDFFF), or with `Lo > Hi` | A range that spans the surrogate interval; overlapping ranges; an empty class |

Validation is structural. It does not resolve names, check the types of actions,
validate attributes or enforce the semantics of an Operators Rule; compilation does.
It does not validate comments or layout. Compilation, the generators and the
reconstruction of an AST from a compiled grammar run structural validation
themselves. `grammar.Format` and `grammar.MarshalJSON` expect a structurally valid
AST.

Both bytecode VMs and generated parsers treat any negative maximum as unbounded.
Saving a compiled grammar preserves the original maximum in its AST and writes the
standard unbounded representation to the bytecode.

### Grammar JSON

`grammar.MarshalJSON` serializes the AST using the `json` tags of its fields. Each
interface node has an `"@type"` member such as `"RuleDef"` or `"Literal"`. Parsed
metadata is written under `source` or `skipSource`; formatter layout
and exact comment tokens are omitted.

| Function | Behavior |
|:--|:--|
| `grammar.UnmarshalJSON(data)` | Accepts exactly one complete JSON value, optionally followed by white space, and runs structural validation. It rejects `null`, trailing values or other data, and numbers that overflow their Go destination. Unknown fields are ignored; duplicate keys follow `encoding/json` |
| `grammar.DecodeJSON(data)` | As `UnmarshalJSON`, and also rejects unknown fields and duplicate keys at every object. A `*grammar.JSONDecodeError` reports the JSON path and the zero-based byte offset of such an error; malformed JSON wraps the standard syntax error |

A null field is reported by path (for example `$.file.decls[0].type.fields[0]`)
and does not cause a panic during compilation.


An attribute (`Attributed`, `Attribute`) holds `args`, an ordered list of integers,
strings, booleans, null slots and expression nodes, and `kwArgs`, which maps names to
the same forms. `@custom(2,)` yields `[2, null]` and `@custom(2)` yields `[2]`.
Keyword arguments follow positional arguments in source, and strict decoding rejects
a repeated keyword. Integer constants use the native `int` range; the minimum
integer is formatted as `-MaxInt - 1`, with parentheses where required, because its
absolute value cannot be a positive source token, and parsing the result preserves
the value.

### Source-Only AST Nodes

Source-only syntax has dedicated nodes. An explicit Rule Expression group is
`Group{Expr}`; formatting and JSON preserve it and compilation removes it. Source
`#` pragmas use `Repeat` (`#repeat`), `Atomic` (`#atomic`), `Recovery` (`#recover`)
and `Stream` (`#stream`); compilation lowers recovery and streaming to their
executable representation without changing the source AST. A braced action sequence
is `Block{Statements}`: a single statement stays in `Predicate.Term`, and a nonempty
multi-statement block executes its statements in order like adjacent Predicate
Actions. Empty predicate blocks, braced Value Actions and `InlineAction` nodes can be
parsed, formatted and serialized, but semantic checking or compilation rejects them
as unsupported. The AST of a compiled grammar and `Parser.Grammar()` return the
normalized grammar, which MAY share unchanged nodes with the source AST and is not a
detached copy. Normalization does not mutate the caller's AST; the source and
normalized ASTs are treated as immutable while a parser uses them, and
`Grammar.Clone()` returns an independent copy.

### Literal Matching Values

A `grammar.Literal.Value` matches as its decoded sequence of runes, in which each
malformed UTF-8 byte becomes U+FFFD. A value that contains `"\xc3"` matches a
replacement character and cannot match the leading byte of `"é"`. Both position
units, saved modules and generated parsers share this rule. Compilation and
generation derive the matching data without rewriting the caller's AST, binary
serialization preserves the original value, and JSON strings follow the replacement
behavior of `encoding/json`. String constants in actions are separate from
matching literals. Matched text follows the input: with byte positions it keeps the
consumed bytes, and with code-point positions it is decoded; so in byte mode
`text($capture)` distinguishes a valid U+FFFD from an invalid input byte that the
same literal matched.

### Context-Aware Formatting and Collection

`grammar.FormatContext`, `FormatExprContext`, `FormatTermContext` and
`FormatTypeContext` produce the same text as `Format`, `FormatExpr`, `FormatTerm` and
`FormatType`. On cancellation they return an empty string and the context error, never
partial text. `Grammar.RulesContext`, `Grammar.AllCommentsContext`,
`Grammar.TypesContext` and `Attribute.ExpressionArgsContext` return the contents of
their ordinary counterparts in the same order (declaration order for rules and types;
positional arguments followed by keyword arguments in sorted order; the traversal of
`AllComments`), or a nil slice and the context error on cancellation. Cancellation is
cooperative in every case, and the AST MUST NOT be mutated while these functions run.

### Syntax Projection

`grammar.FormatSyntax(g)` (command line: `pego fmt -syntax`) prints the recognition
syntax of a grammar and leaves the AST unchanged. It omits Type Declaration
Statements, result types, Value Actions, metadata attributes, captures and `#recover`
wrappers; it removes groups whose parentheses are not required for precedence or
layout; and it keeps Predicate Actions and the pragmas that affect recognition,
including `#repeat`, `#atomic` and `#stream`. A predicate that refers to captures or to
values produced by omitted syntax MAY not work in the projection. Comments on retained
forms follow the layout rules of the formatter; comments attached only to omitted
forms MAY be omitted. `-syntax` cannot be combined with `-w`, `-l` or `-check`.
