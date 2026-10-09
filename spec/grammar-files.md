# Grammar Files

A grammar is a set of type definitions and rule definitions. It is normally
written in a file with the extension `.pego`, built from the tokens of
[Lexical Structure](lexical.md). This chapter defines what a grammar file may
contain, the rules for names and for the order of definitions, and the errors
that make a grammar invalid.

## Structure

A grammar file consists of an optional package clause followed by type
definitions and rule definitions in any order.

```pego
package <package name>

type <type name> <type specification>
def <rule name>[: <type>] = <parsing expression> [-> <action>]
def <rule name>[: <type>] = pratt { <Pratt items> }
```

Definitions need no separator. A definition ends where the next `def`, `type`
or the end of the file begins, which is why those keywords cannot appear inside
a parsing expression.

An empty file is a valid grammar with no rules, but it cannot be used to parse
anything, because there is no start rule to select.

### Package clause

`package <name>` names the package of the grammar, with an identifier. If
present, it MUST be the first item of the file. The package name does not
affect parsing. The reference implementation records it, writes it back when it
formats the grammar and stores it in compiled grammars; the name of the package
of generated Go code is given when the code is generated.

### Type definitions

A type definition (`type`) defines a node type: a struct type, a union or alias
of types, or a terminal type. The forms and their meaning are defined in the
[Type System](types.md#type-definitions).

### Rule definitions

A rule definition (`def`) defines a rule: a name for a parsing expression. When
the rule matches, it produces a single value.

- `: <type>` declares the type of the value the rule produces. If it is
  omitted, the type is inferred (see [Type inference](type-checking.md#type-inference)).
- The right-hand side of `=` is either a
  [parsing expression](parser-expressions.md) or a
  [Pratt expression](pratt.md). A Pratt expression, which starts with `pratt`,
  MUST be the whole right-hand side.
- The optional `-> <action>` is an [action](actions.md) that builds the value
  of the rule from what the parsing expression matched. It applies to the
  whole right-hand side; an action cannot be attached to an individual
  alternative of a choice. A rule whose body is a Pratt expression has no
  rule-level action: its operands and operators have their own.

A rule is used by writing its name in a parsing expression, which calls it at
the current position (see [Rule calls](parser-expressions.md#rule-calls)).

## Names

Rule names and type names live in separate namespaces. Each rule name MUST be
defined at most once and each type name MUST be defined at most once. A rule
MAY refer to rules defined later in the file, and a type MAY refer to types
defined later: **the order of definitions has no effect on the meaning of the
grammar**. Every rule that a parsing expression names MUST be defined.

The names that a definition can use are constrained as follows:

| Kind of name | Constraint |
|:--|:--|
| Rule | An [identifier](lexical.md#identifiers) that is not a [keyword](lexical.md#keywords). The examples begin rule names with a lowercase letter, which is a convention. |
| Type, field | An identifier that begins with an uppercase letter. The built-in and reserved type names MUST NOT be defined. See [Names](types.md#names). |
| Capture label, variable, level, lambda parameter | An identifier. Captures and variables have their own scopes; see [Capture](parser-expressions.md#capture) and [Variables](predicates.md#variables). A level name MUST be unique within its rule. |

## Start rule

Parsing begins with a start rule, which is not named in the grammar file: it is
selected when the grammar is compiled or when parsing is started. See
[Start rule](parsing.md#start-rule).

## Errors in a grammar

A grammar with an error cannot be used to parse. Each error is reported with the
line and column of the construct it concerns in the grammar source (see
[Source text](lexical.md#source-text)). The classes of error are:

| Class | Examples | Defined in |
|:--|:--|:--|
| Lexical and syntax errors | An unterminated string, an unknown escape, a character that starts no token, a missing `)`, a definition that is neither `def` nor `type`, a repetition whose maximum is less than its minimum | [Lexical Structure](lexical.md) and the constructs below |
| Definition and reference errors | A name that is defined twice, a reserved or lowercase type name, an undefined rule, a call with a level that does not exist, a capture of an expression without a value, an unknown attribute, a `#stream` that is not at the top level of a rule body | The chapter of the construct |
| Type errors | A field that the struct does not declare, a value that is not assignable to the declared type, `$n` out of range | [Type Checking](type-checking.md#checked-errors) |

The reference implementation reports all lexical and syntax errors of a file
(at most 100), then, if there are none, all definition and reference errors, and
then, if there are none, the type errors. This file has three definition and
reference errors:

```pego
def main = item $$
def item = k:"a" -> $v
def other = unknown
def main = "b"
```

Parsing with it (`pego parse -g bad.pego -i a`) reports:

```
pego: bad.pego:4:1: rule main is already defined
bad.pego:2:21: undefined capture $v
bad.pego:3:13: undefined rule unknown
```

## Grammar representations

Besides `.pego` source files, a grammar can be represented as JSON, which is
the abstract syntax tree of the grammar (the public package `grammar`), and as a
compiled grammar file (`.pegoc`), which holds the bytecode of the grammar and,
optionally, the tree (see [File format](bytecode.md#file-format)). All
representations describe the same language. The `pego convert` command
converts between them, and `pego fmt` formats `.pego` source files, keeping
comments.
