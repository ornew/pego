# Lexical Structure

This chapter defines how grammar-file text is divided into characters, line
terminators, white space, comments, identifiers, keywords, literals, escape
sequences, and punctuation. The structure built from these tokens is described
in [Grammar Files](grammar-files.md); the complete syntax is summarized in
[Grammer](syntax.md).

## Source Text

A grammar file is Unicode text encoded as UTF-8. A character outside a
comment, string literal, or character class that is neither white space nor
the start of a token is an error. The diagnostic includes following characters
up to the next white space or token. Grammar errors use 1-based line and
character-column positions; a tab counts as one character, not its byte width.
Positions in parsed input are defined separately in [Positions](parsing.md#positions).

### Line Terminators

A line ends at LF (U+000A), CRLF, or CR (U+000D). A line terminator ends a
line comment and determines diagnostic line numbers. LF MUST NOT occur inside
a string literal or character class. Line terminators otherwise count as white
space.

### White Space

White space is any Unicode character with the `White_Space` property,
including line terminators. It separates tokens and is insignificant except
where this chapter requires adjacency.

### Comments

`//` outside a string or character data starts a line comment through, but not
including, the line terminator. `/*` starts a nested block comment, which ends
at its matching `*/`. Comment markers inside strings or character data are
ordinary content. The parser records comment trivia and may accept it as
syntactic trivia; semantic analysis reports a positioned error when a comment
occurs in a declaration or Operators Rule statement header. Comments are
allowed at top-level statement boundaries, at statement and field boundaries
inside braces, and freely inside PEG and action expressions, including
predicates and function arguments.

```pego
rule digits = [0-9]+ // one or more digits
/* outer comment /* nested comment */ continues */
```

## Tokens

Tokens are identifiers, string literals, character classes, integer literals,
capture references, and punctuation. They are formed from left to right using
the longest token available at each position.

### Identifiers

An identifier matches `[a-zA-Z_][a-zA-Z0-9_]*`. Identifiers are
case-sensitive and have no length limit. Identifier syntax is ASCII-only:
non-ASCII letters and digits do not continue an identifier. Unicode remains
available in string literals, character classes, comments, and parsed input.
Names beginning with `_` are reserved and MUST NOT be used as identifiers,
including declaration names, references, captures, variables, level names,
attribute names, or named arguments. Parsing preserves these names in the
source AST; semantic validation rejects their use before compilation. The
standalone PEG atom `_` is the
[Top expression](parser-expressions.md#top), not an available name.
Identifiers name rules, types, fields, captures, variables, levels,
attributes, and lambda parameters. The initial ASCII letter's case matters
for type and field names; see [Names](types.md#names).

<a id="keywords"></a>

### Contextual Words

The words `infix`, `level`, `operand`, `operators`, `package`, `postfix`,
`prefix`, `rule`, `skip`, `struct`, `terminal`, and `type` are contextual
words, not globally reserved identifiers. They are interpreted as syntax only
where the grammar expects a declaration or Operators Rule item. In a name
position, rule reference, capture, field, variable, or expression, they remain
ordinary identifiers. No declaration name is excluded solely because it is
one of these words.

The following identifiers are also contextual:

| Identifier | Meaning | Context |
|:--|:--|:--|
| `_` | The [top](parser-expressions.md#top) expression | Parsing expression |
| `left`, `right`, `none` | Associativity | After `infix` in an [Operators Rule](operators-rule.md) |
| `true`, `false`, `nil` | Literal values | Actions and predicates |
| `len`, `text`, `foldl`, `foldr`, `map`, `list`, `concat` | Built-in functions | Actions and predicates |
| `startPos`, `endPos`, `children` | Built-in fields | Field access |

### String Literals

A string literal is enclosed in double quotes and matches exactly its decoded
characters. It may not contain a line terminator. The quote and backslash are
written as `\"` and `\\` respectively. String literals may also be values in
actions.

```pego
"if" "\n" "say \"hi\"" "😀"
```

### Character Classes

A character class matches one Unicode character selected by its
contents. Its matching rules are defined in
[Character Classes](parser-expressions.md#character-classes). Literal
characters, ranges, negation and escapes are defined there. `[` begins a character class.

```pego
[a-z_] [^"\\] [\-()?]
```

### Integer Literals

An integer literal contains one or more ASCII decimal digits (`0`–`9`). It is
used for repetition bounds and in actions and predicates. A value outside the
range of the implementation's `int` is an error. Write negative action values
with unary `-`. The signed minimum `int` is also accepted as a single value:
its positive magnitude cannot be a separate integer token. This preserves the
full native integer range in generic attribute arguments.

```pego
"a" #repeat(2, 3) {len($s) > 4}
```

### Capture References

`$` immediately followed by an identifier or ASCII decimal digits forms a
capture reference (`$label`, `$1`) in actions and predicates. `$` followed by
any other character is the end-of-line anchor; `$$` is the end-of-input anchor.
A positional index uses the same digit and implementation-`int` range as an
integer literal. Leading zeroes are allowed, and `$0` selects all valued
captures. Non-ASCII digits and overflowing indices are errors, not alternate
indices or anchors.

### Punctuation

Punctuation tokens include:

```
_|_  ^^  $$  --  =>  ==  !=  <=  >=  &&  ||  :=
:  =  /  (  )  {  }  [  ]  ,  ;  .  *  +  ?  &  !  @  -  ^  $  #  |  <  >  %  //  /*  */
```

| Token | Use |
|:--|:--|
| `_|_`, `^^`, `$$`, `^`, `$`, `.`, `--` | Bottom, anchors, any character, and cut in parsing expressions |
| `/`, `*`, `+`, `?`, `&`, `!`, `@`, `-`, `:`, `(`, `)`, `{`, `}`, `,`, `;` | PEG operators, repetition, captures, annotations, and declaration separators |
| `{`, `}` | Predicate delimiters when a PEG primary is expected; they also delimit source action blocks in their respective contexts |
| `//`, `/*`, `*/` | Line and nested block comments |
| `=>` | Whole-rule or Operators Rule action in those declaration positions; lambda arrow inside an action |
| `:=` | Predicate variable definition |
| `==`, `!=`, `<`, `<=`, `>`, `>=`, `&&`, `||`, `!`, `+`, `-`, `*`, `/`, `%` | Action and predicate operators |
| `=`, `|`, `[]`, `*` | Type definitions and type forms (`type A = B | C`, `[]T`, `*T`) |
| `.` | Action field access |
| `@` | Postfix expression attribute |
| `#` | Behavioral pragma |

## Escape Sequences

Strings and character classes accept these escapes:

| Escape | Character |
|:--|:--|
| `\n` | LF (U+000A) |
| `\t` | Horizontal tab (U+0009) |
| `\r` | CR (U+000D) |
| `\f` | Form feed (U+000C) |
| `\v` | Vertical tab (U+000B) |
| `\a` | Alert (U+0007) |
| `\b` | Backspace (U+0008) |
| `\0` | NUL (U+0000) |
| `\u{h...}` | Unicode code point given by hexadecimal digits, e.g. `\u{1F600}` |
| `\uhhhh` | Unicode code point given by exactly four hexadecimal digits |
| `\c` | For a character `c` that is not an ASCII letter or digit, the character itself; other letter or digit escapes are errors |

A hexadecimal escape MUST denote a Unicode scalar value from U+0000 through
U+10FFFF, excluding U+D800–U+DFFF. Invalid hexadecimal text, surrogates, and
values above U+10FFFF are errors. Adjacent surrogate escapes do not combine;
write the scalar directly, such as `\u{1F600}`. Unicode noncharacters such as
U+FFFF are valid scalar values.

## Adjacency

An attribute name MUST immediately follow `@`, and an attribute argument
list MUST immediately follow its name: `@name` or `@name(...)`. White space
or comments between `@` and the name are syntax errors. A separate group after
`@name (expr)` is a following PEG expression, not an argument list.

These forms require adjacency; intervening white space or comments change the
parse:

| Construct | Adjacent form | Separated form |
|:--|:--|:--|
| Capture `label:expr` | `x:expr` | `x :expr` is invalid |
| Action function call | `len($s)` | `len ($s)` is a variable followed by a grouped expression |
| Operators Rule level call | `level(expr, assignment)` | `level (expr, assignment)` is not a level call |

The lexer takes the longest punctuation token. Thus `--` is one cut token,
`$$` is one end-of-input anchor, and `$` followed by a letter, `_`, or digit
begins a capture reference. A `{` begins a Predicate Action when a PEG primary
is expected; `[]` remains a character class in PEG expressions and a type-list
prefix in type syntax. `@` attributes, `#` pragmas and comment markers are
recognized outside shielded string and character data.

## Limits

An implementation MAY limit source nesting depth and reported errors. The
reference implementation reports at most 100 errors, with the last indicating
that more exist, and rejects parenthesis nesting deeper than about 500 levels.
