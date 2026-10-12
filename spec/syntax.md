# Syntax Summary

This chapter collects the syntax of a grammar file in one place. The grammar
below is written in PEGO itself and summarizes token structure and expression
forms. The chapters that define each construct remain normative for validation
and meaning.

The grammar is checked by a test (`spec/syntax_test.go`), which runs it on every
`.pego` file of the repository and on every `pego` code block of the
documentation and requires it to agree with the parser of the reference
implementation on whether the text is a valid grammar.

## Grammar

```pego
// The syntax of PEGO grammar files, written in PEGO. The start rule is file.
//
// Conventions: ws is matched where whitespace and comments are permitted.
// A keyword is followed by word_end so it cannot prefix a longer identifier.

// --- Grammar files ---

rule file = ws package_clause? (definition / ";" ws)* ws $$

rule package_clause = "package" word_end ident

rule definition = type_def / rule_def

// --- Type definitions ---

rule type_def = "type" word_end ident type_spec

rule type_spec = struct_spec / terminal_spec / alias_spec

rule struct_spec = "struct" word_end "{" ws field* "}" ws

rule field = ident type_expr ("," ws)?

rule terminal_spec = "terminal" word_end

rule alias_spec = "=" ws type_expr

rule type_expr = type_term ("|" ws type_term)*

rule type_term =
    "[]" ws type_term
    / "*" ws type_term
    / "(" ws type_expr ")" ws
    / ident

// --- Rule definitions ---

rule rule_def = "rule" word_end ident type_expr? "=" ws rule_body

rule rule_body = "operators" word_end operators_body / body

rule body = choice ("=>" ws action)?

// --- Parsing expressions ---

rule choice = sequence ("/" ws sequence)*

rule sequence = (!stop prefixed)+

rule prefixed = capture / lookahead / discard / suffixed

rule capture = raw_ident ":" ws prefixed
rule lookahead = ("&" / "!") ws prefixed
rule discard = "-" !("-" / ">") ws prefixed

// Whitespace and comments may separate an expression from any postfix suffix.
rule suffixed = primary ws ((repeat_op / pragma / attribute) ws)*

rule repeat_op = "*" / "+" / "?"

// Pragmas have directive-specific payloads and targets.
rule pragma = atomic_pragma / stream_pragma / repeat_pragma / recovery_pragma

rule atomic_pragma = "#atomic" !ident_char ("(" ws ")")?
rule stream_pragma = "#stream" !ident_char ("(" ws ")")?
rule repeat_pragma = "#repeat" !ident_char ws "(" ws repeat_args ")"
rule recovery_pragma = "#recover" !ident_char ws "(" ws "skip" word_end "=" ws choice ")"

rule repeat_args = repeat_count ("," ws repeat_count?)? / "," ws repeat_count
rule repeat_count = "-"? digits

// Attributes have generic positional and named values. Omitted positional
// values are represented by empty slots; positional values precede named ones.
rule attribute = "@" ws raw_ident (attr_args / no_paren)
rule attr_args = "(" ws (attr_named_list / attr_positional_list ("," ws attr_named_list)?)? ")"
rule attr_named_list = attr_named ("," ws attr_named)* ("," ws)?
rule attr_named = raw_ident "=" ws attr_value
rule attr_positional_list = !(raw_ident "=") attr_value? ("," ws !(raw_ident "=") attr_value?)*
rule attr_value = "nil" word_end / "true" word_end / "false" word_end / ("-"? digits) ws / string ws / choice

rule primary =
    string
    / char_class
    / "_|_"
    / "--"
    / "^^"
    / "^"
    / "$$"
    / "$" !(ident_start / digit)
    / "."
    / "(" ws inline_expr ")"
    / predicate
    / rule_ref

rule inline_expr = choice ("=>" ws action)?

rule rule_ref = !keyword raw_ident (level_call / no_paren)

// A call of a rule level or an action function requires adjacent parentheses.
rule level_call = "(" !"?" ws raw_ident ws ")"
rule no_paren = !("(" !"?")

rule predicate = "(?" ws (assign / term) ")"

rule assign = ident ":=" ws term

// --- Operators Rules ---

// Items may be separated by a line boundary or an explicit semicolon.
rule operators_body = "{" ws (operators_item / ";")* "}" ws

rule operators_item = operators_skip / operators_operand / operators_level

rule operators_skip = "skip" word_end choice

rule operators_operand = "operand" word_end choice ("=>" ws action)?

rule operators_level = "level" word_end ident? "{" ws (operators_operator / ";")* "}" ws

rule operators_operator = prefix_op / postfix_op / infix_op

rule prefix_op = "prefix" word_end choice ("=>" ws action)?
rule postfix_op = "postfix" word_end choice ("=>" ws action)?
rule infix_op = "infix" word_end ("left" / "right" / "none") word_end choice ("=>" ws action)?

// --- Actions and predicates ---

rule action = "..." / term

rule term = lambda / or_term

rule lambda = "(" ws (ident ("," ws ident)* ("," ws)?)? ")" ws "=>" ws term

rule or_term = and_term ("||" ws and_term)*
rule and_term = cmp_term ("&&" ws cmp_term)*
rule cmp_term = add_term (cmp_op ws add_term)?
rule cmp_op = "==" / "!=" / "<=" / ">=" / "<" / ">"
rule add_term = mul_term (add_op ws mul_term)*
rule add_op = "+" / "-" !("-" / ">")
rule mul_term = unary_term (mul_op ws unary_term)*
rule mul_op = "*" / "/" / "%"

rule unary_term = ("-" !"-" / "!" !"=") ws unary_term / member_term

rule member_term = term_primary ("." ws ident)*

rule term_primary =
    digits ws
    / string ws
    / capture_ref
    / "true" word_end
    / "false" word_end
    / "nil" word_end
    / constructor
    / call
    / "(" ws term ")" ws
    / raw_ident no_paren ws

rule capture_ref = "$" (raw_ident / digits) ws

rule constructor = raw_ident ws "{" ws (field_init ("," ws field_init)* ("," ws)?)? "}" ws

rule field_init = ident ":" ws term

rule call = raw_ident "(" !"?" ws (term ("," ws term)*)? ")" ws

// --- Lexical elements ---

rule ws = (space / comment)*

rule space = [ \t\n\u{b}\u{c}\r\u{85}\u{a0}\u{1680}\u{2000}-\u{200a}\u{2028}\u{2029}\u{202f}\u{205f}\u{3000}]

rule comment = line_comment / block_comment

rule line_comment = "//" [^\n\r]*

rule block_comment = "/*" (block_comment / !("/*" / "*/") .)* "*/"

rule word_end = !ident_char ws

rule ident = raw_ident ws

rule raw_ident = ident_start ident_char*

rule ident_start = letter / "_"

rule ident_char = letter / digit / "_"

// Letter and digit are Unicode categories; ranges below approximate them.
rule letter = [a-zA-ZªµºÀ-ÖØ-öø-˿Ͱ-\u{10ffff}]

rule digit = [0-9]

rule digits = digit+

rule stop = ("package" / "type" / "rule" / "operators" / "skip" / "operand" / "level" / "prefix" / "postfix" / "infix") !ident_char

rule keyword = ("rule" / "infix" / "level" / "operand" / "package" / "postfix" / "operators" / "prefix" / "skip" / "struct" / "terminal" / "type") !ident_char

rule string = "\"" string_char* "\""

rule string_char = "\\" escape_char / [^"\\\n\r]

rule char_class = "[" "^"? class_item+ "]"

rule class_item = class_char ("-" !"]" class_char)?

rule class_char = "\\" escape_char / [^\]\\\n\r]

rule escape_char = "u{" [0-9a-fA-F]+ "}" / "u" [0-9a-fA-F] #repeat(4) / [ntrfvab0] / [^a-zA-Z0-9]
```

## What the Grammar Does Not Express

The following rules apply in addition to the grammar. Each is an error that the
reference implementation reports when it reads the grammar source.

- **Letters and digits.** In an [identifier](lexical.md#identifiers), a letter is
  a Unicode letter and a digit a Unicode digit. The grammar approximates them
  with ranges of characters. An [integer literal](lexical.md#integer-literals)
  MUST be representable as an `int`.
- **Character class ranges.** In `lo-hi`, `hi` MUST NOT be less than `lo`, and a
  class MUST contain at least one item.
- **Repetition bounds.** In `a #repeat(n,m)`, `m` MUST NOT be less than `n`.
- **Declaration positions.** Declarations begin at column 1 or after a
  semicolon and its following white space. Indented lines continue an
  expression; open groups, type bodies, Operators Rule bodies and closing
  delimiters use their grammatical context. Operators Rule items and struct
  fields may use line boundaries as separators, while semicolons and commas
  are explicit separators. The summary grammar does not model these physical
  layout distinctions.
- **Attribute arguments.** Positional arguments follow the generic value
  forms and precede named arguments; repeated named keys are rejected. The
  grammar models argument shape and ordering, not key uniqueness.
- **Keywords.** A [keyword](lexical.md#keywords) cannot be the name of a rule
  that is called, and the keywords that start a definition or an Operators Rule entry
  cannot be a capture label.
- **Where constructs may appear.** An Operators Rule MUST be a complete rule
  declaration; a lambda MUST be an argument of `foldl`, `foldr` or `map`; and
  `#stream` MUST be at the top level of a rule body. A comment in a declaration
  or Operators Rule statement header is rejected by semantic analysis with a
  positioned diagnostic. These checks follow parsing.
- **Inline actions.** `(expression => action)` is recognized in the source
  grammar and represented in the AST, but compilation rejects it. Runtime
  execution is not implemented.
- **Declaration annotations.** Attributes are postfix expression annotations;
  declaration-prefix attributes are not supported.

## Operators

Parsing expressions bind in the order of the [precedence
table](parser-expressions.md#precedence). The expressions of actions and
predicates bind in the order of the [operator table](actions.md#operators-and-functions):
`||`, `&&`, comparison (`==`, `!=`, `<`, `<=`, `>`, `>=`, which do not chain),
`+` and `-`, `*`, `/` and `%`, and the unary `-` and `!`. Field access (`.`) and
function calls bind tighter than the operators. The grammar above builds these
levels by the order of its rules.
