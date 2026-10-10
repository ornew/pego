# Lexical Structure

This chapter defines how the text of a grammar file is divided into tokens:
characters, line terminators, white space, comments, identifiers, keywords,
literals, escape sequences and punctuation. The structure of the file built from
the tokens is the subject of [Grammar Files](grammar-files.md), and the whole
syntax is summarized in [Syntax Summary](syntax.md).

## Source text

A grammar file is a sequence of Unicode characters encoded in UTF-8. A character
outside a comment, a string literal and a character class that is not white
space and does not start a token is an error, reported together with the
characters that follow it up to the next white space or token.

Errors in a grammar are reported with a 1-based line and a 1-based column. The
column counts characters (a tab is one character), not bytes.

### Line terminators

A line ends at a line feed (U+000A), at a carriage return followed by a line
feed, or at a carriage return alone. A line terminator matters only where this
specification says so: it ends a comment, it numbers the lines of error
positions, and a line feed MUST NOT appear in a string literal or in a character
class.

(Positions in the *input* being parsed count lines differently: see
[Positions](parsing.md#positions).)

### White space

White space is any Unicode white space character (the Unicode property
`White_Space`, which includes the line terminators). It separates tokens and is
otherwise insignificant, except where tokens MUST be adjacent (see
[Adjacency](#adjacency)).

### Comments

A comment starts with `//` and extends to the end of the line, up to but not
including the line terminator. A comment is white space. There are no block
comments. The characters `//` inside a string literal or a character class do
not start a comment.

```pego
def digits = (?0-9)+ // one or more digits
```

## Tokens

The tokens are identifiers, string literals, character classes, integer
literals, capture references and punctuation. The tokens of a file are formed
left to right, and the longest possible token is taken at each position
(see [Adjacency](#adjacency)).

### Identifiers

An identifier starts with a letter or `_` and continues with letters, digits and
`_`. Letters and digits are Unicode letters and digits. Identifiers are
case-sensitive and there is no length limit.

```
identifier = ( letter | "_" ) { letter | digit | "_" } .
```

Identifiers name rules, types, fields, captures, variables, levels, attributes
and the parameters of lambdas in actions. The case of the first letter matters
for type and field names; see [Names](types.md#names).

### Keywords

The following identifiers are keywords. They MUST NOT be used as rule names or
be referred to as rules:

```
def  infix  level  new  operand  package  postfix  pratt  prefix  skip  struct  terminal  type
```

A keyword in a position where it cannot appear is an error. The keywords
`def`, `infix`, `level`, `operand`, `package`, `postfix`, `prefix`, `skip` and
`type` also end a sequence of parsing expressions, because each of them starts
the next definition or item; for that reason they cannot be used as capture
labels either. The reference implementation accepts a definition whose name is a
keyword, but no other rule can call it.

Some identifiers have a special meaning only in certain contexts and are not
keywords:

| Identifier | Meaning | Where |
|:--|:--|:--|
| `_` | The [top](parser-expressions.md#top) expression | In a parsing expression |
| `left`, `right`, `none` | Associativity | After `infix` in a [Pratt expression](pratt.md) |
| `true`, `false`, `nil` | Literal values | In actions and predicates |
| `len`, `text`, `foldl`, `foldr`, `map`, `list`, `concat` | Built-in functions | In actions and predicates |
| `startPos`, `endPos`, `children` | Built-in fields | In [field access](actions.md#field-access) |

### String literals

A string literal is a sequence of characters between double quotes. It matches
exactly that sequence of characters (see [Literals](parser-expressions.md#literals)),
and it is used as a value in [actions](actions.md#values). A line terminator
MUST NOT appear inside it. The character `"` and the character `\` are written
as escape sequences (`\"` and `\\`).

```pego
"if"   "\n"   "say \"hi\""   "\u{1F600}"
```

### Character classes

A character class starts with `(?`, ends with `)` and lists characters and
ranges between them. Its syntax and meaning are defined in
[Character classes](parser-expressions.md#character-classes). A character class
MUST contain at least one item and MUST NOT contain a line feed. The
sequence `(?` always starts a character class.

```pego
(?a-z_)   (?^"\\)   (?\-(?\))
```

### Integer literals

An integer literal is a sequence of ASCII decimal digits (`0`–`9`). It is used
as a repetition count in `a{n,m}` and in actions and predicates. A number that
does not fit in the target's implementation `int` is an error: the range is
signed 32-bit on a 32-bit Go target and signed 64-bit on a 64-bit target. A
negative number in an action is written with the unary `-` operator.

```pego
"a"{2,3}   [len($s) > 4]
```

### Capture references

`$` immediately followed by an identifier (`$label`) or by ASCII decimal
digits (`$1`) is a capture reference, used in
[actions](actions.md#capture-references) and [predicates](predicates.md#predicates).
`$` followed by anything else is the
[end-of-line anchor](parser-expressions.md#anchors), and `$$` is the end-of-input
anchor.

A positional index uses the same decimal-digit and target `int` range as an
integer literal. Leading zeros are allowed; `$0` selects all values, as
defined in [capture references](actions.md#capture-references). Non-ASCII
decimal digits and overflowing numbers are errors, including after `$`;
they are not interpreted as a different index or as a line anchor.

### Punctuation

The following are the punctuation tokens. Each is a single token, whatever
follows it.

```
_|_  ^^  $$  --  ->  =>  ==  !=  <=  >=  &&  ||  []
:  =  /  (  )  {  }  [  ]  ,  .  *  +  ?  &  !  @  -  ^  $  #  |  <  >  %
```

| Token | Used for |
|:--|:--|
| `_\|_`, `^^`, `$$`, `^`, `$`, `.`, `--` | [Bottom, anchors, any character and cut](parser-expressions.md) |
| `/`, `*`, `+`, `?`, `&`, `!`, `@`, `-`, `:`, `(`, `)`, `{`, `}`, `,` | Combinators, repetition and capture in [parsing expressions](parser-expressions.md#operator-summary) |
| `[`, `]` | [Predicates](predicates.md) |
| `#` | [Attributes](attributes.md) |
| `->` | The start of an [action](actions.md) |
| `=>` | A lambda in actions |
| `==`, `!=`, `<`, `<=`, `>`, `>=`, `&&`, `\|\|`, `!`, `+`, `-`, `*`, `/`, `%` | [Operators in actions and predicates](actions.md#operators) |
| `=`, `\|`, `[]`, `*` | Definitions and [types](types.md) (`def a = ...`, `type A = B \| C`, `[]T`, `*T`) |
| `.` | [Field access](actions.md#field-access) |

## Escape sequences

String literals and character classes accept the following escape sequences.

| Escape | Character |
|:--|:--|
| `\n` | line feed (U+000A) |
| `\t` | horizontal tab (U+0009) |
| `\r` | carriage return (U+000D) |
| `\f` | form feed (U+000C) |
| `\v` | vertical tab (U+000B) |
| `\a` | alert (U+0007) |
| `\b` | backspace (U+0008) |
| `\0` | NUL (U+0000) |
| `\u{h...}` | the code point with the hexadecimal value `h...` (for example `\u{1F600}`) |
| `\uhhhh` | the code point with the four-digit hexadecimal value `hhhh` |
| `\c` | for any other character `c` that is not an ASCII letter or digit, the character itself (for example `\\`, `\"`, `\-`, `\)`). Any other escape of an ASCII letter or digit is an error. |

A hexadecimal escape MUST denote a Unicode scalar value: U+0000 through
U+10FFFF, excluding the surrogate interval U+D800 through U+DFFF. Invalid
hexadecimal text, surrogate values and values above U+10FFFF are errors.
Adjacent surrogate escapes do not form a supplementary character; write its
scalar value, such as `\u{1F600}`. Noncharacters such as U+FFFF are valid
scalar values.

## Adjacency

The following constructs require their tokens to be written without intervening
white space or comments. When white space is present, the tokens are read as
separate constructs.

| Construct | Example | With white space |
|:--|:--|:--|
| Capture `label:a` | `x:expr` | `x :expr` is a syntax error |
| Bounded repetition `a{n,m}` | `a{2,3}` | `a {2,3}` is a syntax error |
| Level-restricted call `rule(level)` | `expr(assignment)` | `expr (assignment)` is a call of `expr` followed by a group |
| Attribute arguments `#name(...)` | `#error(message="x")` | `#error (...)` is an attribute without arguments followed by a group |
| Function call `f(...)` in actions | `len($s)` | `len ($s)` is a variable reference followed by a parenthesized expression |

Tokens are formed by taking the longest possible match. In particular, `--` is
always the cut operator (`--a` is a cut followed by `a`, not two discards), `$$`
is always the end-of-input anchor, `->` is always the start of an action, and
`$` followed by a letter, `_` or a digit is a capture reference (`$label`, `$1`)
rather than the end-of-line anchor. `[]` is one token, so a predicate cannot be
empty and a list type is written `[]T` with no space.

## Limits

An implementation MAY limit the nesting depth of a grammar source and the number
of errors it reports for one source. The reference implementation reports at
most 100 errors (the last one says that there are more) and rejects with
`nesting too deep` a source that nests parentheses deeper than about 500 levels.
