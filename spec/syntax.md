# Syntax Summary

This chapter collects the syntax of a grammar file in one place. The grammar
below is written in PEGO itself, so it can be run: with the block saved as
`syntax.pego`, `pego parse -g syntax.pego -s file -check < my.pego` prints `ok`
if `my.pego` is syntactically valid. The chapters that define each
construct remain normative; this summary does not replace their rules about
meaning.

The grammar is checked by a test (`spec/syntax_test.go`), which runs it on every
`.pego` file of the repository and on every `pego` code block of the
documentation and requires it to agree with the parser of the reference
implementation on whether the text is a valid grammar.

## Grammar

```pego
// The syntax of PEGO grammar files, written in PEGO. The start rule is file.
//
// Conventions: ws (white space and comments) is matched after a token that
// may be followed by white space and is left out where tokens must be adjacent
// (see "Adjacency" in Lexical Structure). A keyword is followed by word_end, so
// that it is not the beginning of a longer identifier.

// --- Grammar files ---

def file = ws package_clause? definition* $$

def package_clause = "package" word_end ident

def definition = type_def / rule_def

// --- Type definitions ---

def type_def = "type" word_end ident type_spec

def type_spec = struct_spec / terminal_spec / alias_spec

def struct_spec = "struct" word_end "{" ws field* "}" ws

def field = ident type_expr ("," ws)?

def terminal_spec = "terminal" word_end

def alias_spec = "=" ws type_expr

def type_expr = type_term ("|" ws type_term)*

def type_term =
    "[]" ws type_term
    / "*" ws type_term
    / "(" ws type_expr ")" ws
    / ident

// --- Rule definitions ---

def rule_def = "def" word_end ident (":" ws type_expr)? "=" ws (pratt_body / body)

def body = choice ("->" ws term)?

// --- Parsing expressions ---

def choice = sequence ("/" ws sequence)*

def sequence = (!stop prefixed)+

def prefixed = capture / lookahead / atomic / discard / suffixed

def capture = raw_ident ":" ws prefixed
def lookahead = ("&" / "!") ws prefixed
def atomic = "@" ws prefixed
def discard = "-" !("-" / ">") ws prefixed

def suffixed = primary (bounds / ws (repeat_op / attribute))* ws

def repeat_op = "*" / "+" / "?"

def bounds = "{" ws (digits ws)? ("," ws (digits ws)?)? "}"

def attribute = "#" ws raw_ident (attr_args / no_paren)

def attr_args = "(" !"?" ws (attr_arg ("," ws attr_arg)* ("," ws)?)? ")"

def attr_arg = ident "=" ws choice

def primary =
    string
    / char_class
    / "_|_"
    / "--"
    / "^^"
    / "^"
    / "$$"
    / "$" !(ident_start / digit)
    / "."
    / "(" ws choice ")"
    / predicate
    / rule_ref

def rule_ref = !keyword raw_ident (level_call / no_paren)

def level_call = "(" !"?" ws raw_ident ws ")"

// "(" that is not the start of a character class
def no_paren = !("(" !"?")

def predicate = "[" ws (assign / term) "]"

def assign = ident "=" !("=" / ">") ws term

// --- Pratt expressions ---

def pratt_body = "pratt" word_end "{" ws pratt_item* "}" ws

def pratt_item = pratt_skip / pratt_operand / pratt_level

def pratt_skip = "skip" word_end choice

def pratt_operand = "operand" word_end choice ("->" ws term)?

def pratt_level = "level" word_end ident? "{" ws pratt_operator* "}" ws

def pratt_operator = prefix_op / postfix_op / infix_op

def prefix_op = "prefix" word_end choice ("->" ws term)?
def postfix_op = "postfix" word_end choice ("->" ws term)?
def infix_op = "infix" word_end ("left" / "right" / "none") word_end choice ("->" ws term)?

// --- Actions and predicates ---

def term = lambda / or_term

def lambda = "(" ws (ident ("," ws ident)*)? ")" ws "=>" ws term

def or_term = and_term ("||" ws and_term)*
def and_term = cmp_term ("&&" ws cmp_term)*
def cmp_term = add_term (cmp_op ws add_term)?
def cmp_op = "==" / "!=" / "<=" / ">=" / "<" / ">"
def add_term = mul_term (add_op ws mul_term)*
def add_op = "+" / "-" !("-" / ">")
def mul_term = unary_term (mul_op ws unary_term)*
def mul_op = "*" / "/" / "%"

def unary_term = ("-" !"-" / "!" !"=") ws unary_term / member_term

def member_term = term_primary ("." ws ident)*

def term_primary =
    digits ws
    / string ws
    / capture_ref
    / new_term
    / call
    / "(" ws term ")" ws
    / raw_ident no_paren ws

def capture_ref = "$" (raw_ident / digits) ws

def new_term = "new" word_end ident "{" ws (field_init ("," ws field_init)* ("," ws)?)? "}" ws

def field_init = ident ":" ws term

def call = raw_ident "(" !"?" ws (term ("," ws term)* ("," ws)?)? ")" ws

// --- Lexical elements ---

def ws = (space / comment)*

def space = (? \t\n\v\f\r\u{85}\u{A0}\u{1680}\u{2000}-\u{200A}\u{2028}\u{2029}\u{202F}\u{205F}\u{3000})

def comment = "//" (?^\n\r)*

def word_end = !ident_char ws

def ident = raw_ident ws

def raw_ident = ident_start ident_char*

def ident_start = letter / "_"

def ident_char = letter / digit / "_"

// letter and digit are Unicode letters and digits. Here they are approximated:
// every non-ASCII character from U+00C0 on is taken to be a letter.
def letter = (?a-zA-Z\u{AA}\u{B5}\u{BA}\u{C0}-\u{D6}\u{D8}-\u{F6}\u{F8}-\u{2FF}\u{370}-\u{10FFFF})

def digit = (?0-9)

def digits = digit+

def stop = ("package" / "type" / "def" / "skip" / "operand" / "level" / "prefix" / "postfix" / "infix") !ident_char

def keyword = (
    "def" / "infix" / "level" / "new" / "operand" / "package" / "postfix" / "pratt"
    / "prefix" / "skip" / "struct" / "terminal" / "type"
) !ident_char

def string = "\"" string_char* "\""

def string_char = "\\" escape_char / (?^"\\\n)

def char_class = "(?" "^"? class_item+ ")"

def class_item = class_char ("-" !")" class_char)?

def class_char = "\\" escape_char / (?^\)\\\n)

def escape_char = "u{" (?0-9a-fA-F)+ "}" / "u" (?0-9a-fA-F){4} / (?ntrfvab0) / (?^a-zA-Z0-9)
```

## What the grammar does not express

The following rules apply in addition to the grammar. Each is an error that the
reference implementation reports when it reads the grammar source.

- **Letters and digits.** In an [identifier](lexical.md#identifiers), a letter is
  a Unicode letter and a digit a Unicode digit. The grammar approximates them
  with ranges of characters. An [integer literal](lexical.md#integer-literals)
  MUST be representable as an `int`.
- **Character class ranges.** In `lo-hi`, `hi` MUST NOT be less than `lo`, and a
  class MUST contain at least one item.
- **Repetition bounds.** In `a{n,m}`, `m` MUST NOT be less than `n`.
- **Keywords.** A [keyword](lexical.md#keywords) cannot be the name of a rule
  that is called, and the keywords that start a definition or a Pratt item
  cannot be a capture label.
- **Where constructs may appear.** `pratt` MUST be the whole right-hand side of a
  rule; a lambda MUST be an argument of `foldl`, `foldr` or `map`; `#stream`
  MUST be at the top level of a rule body. These are checked when the grammar is
  compiled, not when it is read.

## Operators

Parsing expressions bind in the order of the [precedence
table](parser-expressions.md#precedence). The expressions of actions and
predicates bind in the order of the [operator table](actions.md#operators):
`||`, `&&`, comparison (`==`, `!=`, `<`, `<=`, `>`, `>=`, which do not chain),
`+` and `-`, `*`, `/` and `%`, and the unary `-` and `!`. Field access (`.`) and
function calls bind tighter than the operators. The grammar above builds these
levels by the order of its rules.
