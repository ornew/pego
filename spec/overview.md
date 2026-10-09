# Overview and Grammar Files

This chapter describes the structure of a grammar file, its lexical elements,
the terminology used throughout the specification, and how positions in the
input are measured.

## Terminology

The following terms are used throughout the specification.

| Term | Meaning |
|:--|:--|
| grammar | A set of type definitions and rule definitions, normally written in a `.pego` file. |
| rule | A named parsing expression, defined with `def`. A rule produces a single value when it matches. |
| start rule | The rule with which parsing begins. It is chosen when the grammar is compiled or parsed, not in the grammar file. |
| parsing expression | An expression that matches input; the body of a rule. See [Parsing Expressions](parser-expressions.md). |
| node | A value in the tree that parsing produces. Every node has a type and a range in the input. |
| concrete syntax tree (CST) | The tree of nodes that parsing expressions produce when no action is involved. |
| terminal | A node that holds the matched input text and has no children: a `Match` node or a node of a terminal type. |
| capture | A parsing expression `label:a` that records the value of `a` under the name `label`. |
| action | An expression written after `->` that computes the value of a rule from its captures. See [Actions](actions.md). |
| predicate | A parsing expression `[...]` that evaluates an expression and succeeds or fails without consuming input. See [Predicates and Variables](predicates.md). |
| variable | A named `int`, `string` or `bool` value defined and read by predicates. |
| attribute | An annotation `#name(...)` on a parsing expression that changes how it is parsed. See [Attributes](attributes.md). |
| struct type | A user-defined node type with named fields. See [Type System](types.md). |
| union type | A type whose values are values of any one of its member types. |
| terminal type | A user-defined node type for terminals. |
| Pratt expression | A rule body that declares operators by precedence and associativity. See [Pratt Expressions](pratt.md). |
| binding level | One precedence level of a Pratt expression, declared with `level`. |
| position unit | The unit in which positions and lengths in the input are measured: code points or bytes. See [Positions](#positions). |
| code point | A Unicode code point; the unit in which the input is matched. |
| memoization | Caching the result of a rule at an input position so that the rule is not evaluated twice at that position (packrat parsing). |

## Grammar files

A grammar file (extension `.pego`) consists of an optional package clause
followed by type definitions and rule definitions in any order.

```pego
package <package name>

type <type name> <type specification>
def <rule name>[: <type>] = <parsing expression> [-> <action>]
```

- The **package clause** names the package of the grammar. If present, it MUST
  be the first item in the file.
- A **type definition** (`type`) defines a node type. See
  [Type System](types.md).
- A **rule definition** (`def`) defines a rule.
  - `: <type>` declares the type of the value the rule produces. If it is
    omitted, the type is inferred (see [Type inference](type-checking.md#type-inference)).
  - The right-hand side of `=` is a [parsing expression](parser-expressions.md)
    or a [Pratt expression](pratt.md).
  - The optional `-> <action>` is an [action](actions.md) that builds the value
    of the rule from what the parsing expression matched. It applies to the
    whole right-hand side; an action cannot be attached to an individual
    alternative of a choice.

Rule names and type names live in separate namespaces. Each rule name and each
type name MUST be defined at most once. A rule MAY refer to rules defined later
in the file.

### Start rule

Parsing begins with the start rule, which is selected outside the grammar file:
in the Go API with the `start` argument of `pego.Compile` and
`pego.CompileSource`, and on the command line with `pego parse -s <rule>`, which
defaults to `main`. The start rule MUST match the entire input; if it succeeds without
consuming all of the input, parsing fails with a syntax error that expects
`end of input` at the first unconsumed position.

## Lexical structure

A grammar file is a sequence of Unicode characters encoded in UTF-8.

### Whitespace and comments

Whitespace (any Unicode white space character, including newlines) separates
tokens and is otherwise insignificant, except where this specification requires
two tokens to be adjacent (see [Adjacency](#adjacency)). A comment starts with
`//` and extends to the end of the line. There are no block comments. In grammar
source, a line ends at a line feed (U+000A), a carriage return followed by a line
feed, or a carriage return alone; a comment ends before the line end. (Positions
in the input being parsed count lines differently: see
[Positions](#positions).)

Implementations MAY limit the nesting depth of grammar source and the number of
syntax errors reported for it; the reference implementation reports at most 100
errors and rejects nesting deeper than about 500 levels of parentheses.

### Identifiers and keywords

An identifier starts with a letter or `_` and continues with letters, digits and
`_`. Letters and digits are Unicode letters and digits.

The following identifiers are keywords and MUST NOT be used as rule names:

```
def  infix  level  new  operand  package  postfix  pratt  prefix  skip  struct  terminal  type
```

In addition, some identifiers have a special meaning only in certain contexts:
`_` (the top expression in a parsing expression), `left`, `right` and `none`
(after `infix`), and `true`, `false` and `nil` (literals in actions and
predicates).

User-defined type names and field names MUST begin with an uppercase letter.
Identifiers that begin with a lowercase letter are reserved for built-in types
(`int`, `string`, `bool`, `node`, `terminal`). The names of the reserved node
types (`Match`, `Seq`, `List`, `Operator`, `Error`) MUST NOT be defined.

### Literals

| Literal | Form | Example |
|:--|:--|:--|
| String literal | Characters between double quotes. It MUST NOT contain a newline. | `"if"`, `"\n"` |
| Character class | `(?` followed by characters and ranges, terminated by `)`. See [Character classes](parser-expressions.md#character-classes). | `(?a-z_)` |
| Integer literal | A sequence of decimal digits. Used in repetition counts and in actions. | `42` |

String literals and character classes accept the following escape sequences:

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

### Adjacency

The following constructs require their tokens to be written without intervening
whitespace or comments. When whitespace is present, the tokens are read as
separate constructs.

| Construct | Example | With whitespace |
|:--|:--|:--|
| Capture `label:a` | `x:expr` | `x :expr` is a syntax error |
| Bounded repetition `a{n,m}` | `a{2,3}` | `a {2,3}` is a syntax error |
| Level-restricted call `rule(level)` | `expr(assignment)` | `expr (assignment)` is a call of `expr` followed by a group |
| Attribute arguments `#name(...)` | `#error(message="x")` | `#error (...)` is an attribute without arguments followed by a group |
| Function call `f(...)` in actions | `len($s)` | `len ($s)` is a variable reference followed by a parenthesized expression |

Tokens are formed by taking the longest possible match. In particular, `--` is
always the cut operator, `$$` is always the end-of-input anchor, and `$`
followed by a letter, `_` or a digit is a capture reference (`$label`, `$1`)
rather than the end-of-line anchor.

## Example: a calculator

```pego
package calc

// --- Types ---
type Number terminal // a terminal type: the value is the matched text
type OpType terminal

type Op struct {
    Left  Node   // left operand
    Op    OpType // operator
    Right Node   // right operand
}

// A Node is either an Op or a Number.
type Node = Op | Number

// --- Rules ---

// Expression (addition and subtraction).
def expr: Node =
    // Capture the first term as l, and zero or more (operator, term) pairs as rest.
    l:term rest:(op:term_binary_op r:term)*
    // Fold the captured values from the left to build the tree.
    -> foldl($l, $rest, (acc, i) => new Op{Left: $acc, Op: $i.op, Right: $i.r})

// Term (multiplication and division).
def term: Node =
    l:factor rest:(op:factor_binary_op r:factor)*
    -> foldl($l, $rest, (acc, i) => new Op{Left: $acc, Op: $i.op, Right: $i.r})

def term_binary_op: OpType = @("+" / "-")
def factor_binary_op: OpType = @("*" / "/")

// Factor (a number or a parenthesized expression).
def factor: Node = group / number

def group: Node = "(" e:expr ")" -> $e

// A number is one or more digits.
def number: Number = @((?0-9)+)

// The whole input must be an expression.
def main = ^^ expr $$
```

For the input `1+2*3`, the value of `expr` is
`Op{Left: Number"1", Op: OpType"+", Right: Op{Left: Number"2", Op: OpType"*", Right: Number"3"}}`.
The value of `main` is a `Seq` node whose only child is that value, because the
anchors `^^` and `$$` have no value.

## Positions

Positions in the input are measured in a **position unit**, which is selected
for each parse. The unit applies to node ranges, the `startPos` and `endPos`
fields and the `len` function in actions, and the line and column of syntax
errors.

| Position unit | A position is the number of ... from the beginning of the input |
|:--|:--|
| Code points (default) | Unicode code points |
| Bytes | bytes of the UTF-8 encoding |

The unit affects only how positions are counted. Matching is always performed
one code point at a time: a character class or `.` matches exactly one code
point in either unit. When the input is measured in bytes, each byte that is not
part of a valid UTF-8 sequence is read as U+FFFD (REPLACEMENT CHARACTER)
occupying one byte.

The unit is selected with `pego.WithUnit(pego.Bytes)` in the Go API and with
`pego parse -unit bytes` on the command line.

Lines and columns in syntax errors are 1-based. A new line starts after each
line feed (U+000A); the column is counted in the position unit.

## Grammar representations

Besides `.pego` source files, a grammar can be represented as JSON and as a
compiled grammar file (`.pegoc`). All three describe the same language. The
`pego convert` command converts between them, and `pego fmt` formats `.pego`
source files.
