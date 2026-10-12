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
// Grammar-file syntax. Each statement starts with multiline whitespace disabled.
// Expression-owned delimiters enable it only until their matching close.
rule file = {ml := false} all (package_clause file_end)? (definition file_end / ";" all)* $$
rule package_clause = "package" word_end ident
rule definition = type_def / rule_def
rule file_end = horizontal (";" all / line_trivia all / &$$)
rule item_end = horizontal (";" all / line_trivia all / &"}")
rule field_end = horizontal ("," all / line_trivia all / &"}")

rule type_def = "type" word_end ident type_spec
rule type_spec = struct_spec / terminal_spec / alias_spec
rule struct_spec = "struct" word_end "{" {ml := false} all (field field_end)* "}"
rule field = ident type_expr
rule terminal_spec = "terminal" word_end
rule alias_spec = "=" ws type_expr
rule type_expr = type_term ("|" ws type_term)*
rule type_term = "[]" ws type_term / "*" ws type_term / type_group ws / ident
rule type_group = "(" {ml := true} ws type_expr ")"

rule rule_def = "rule" word_end ident type_expr? "=" ws rule_body
rule rule_body = operators_rule / body
rule operators_rule = "operators" !ident_char ws &"{" -- operators_body ws ("=>" ws action)?
rule body = choice ("=>" ws action)?
rule choice = sequence ("/" ws sequence)*
rule sequence = prefixed+
rule prefixed = capture / lookahead / discard / suffixed
rule capture = raw_ident ":" ws prefixed
rule lookahead = ("&" / "!") ws prefixed
rule discard = "-" !("-" / ">") ws prefixed
rule suffixed = primary ws ((repeat_op / pragma / attribute) ws)*
rule repeat_op = "*" / "+" / "?"
rule primary = string / char_class / "_|_" / "_" !ident_char / "--" / "^^" / "^" / "$$" / "$" !(ident_start / digit) / "." / peg_group / predicate / level_ref / rule_ref
rule peg_group = "(" {ml := true} ws choice ("=>" ws action)? ")"
rule rule_ref = raw_ident no_paren
rule level_ref = "level" level_args
rule level_args = "(" {ml := true} ws raw_ident ws "," ws raw_ident ws ")"
rule no_paren = !"("

// Pragmas use their own payload grammar. Attributes use generic argument lists.
rule pragma = atomic_pragma / stream_pragma / repeat_pragma / recover_pragma
rule atomic_pragma = "#atomic" !ident_char (empty_args / no_paren)
rule stream_pragma = "#stream" !ident_char (empty_args / no_paren)
rule empty_args = "(" {ml := true} ws ")"
rule repeat_pragma = "#repeat" !ident_char ws repeat_args
rule repeat_args = "(" {ml := true} ws (repeat_count ("," ws repeat_count?)? / "," ws repeat_count) ")"
rule repeat_count = "-"? digits ws
rule recover_pragma = "#recover" !ident_char ws recover_args
rule recover_args = "(" {ml := true} ws "skip" word_end "=" ws choice ")"
rule attribute = "@" ws raw_ident (attr_args / no_paren)
rule attr_args = "(" {ml := true} ws (attr_named_list / attr_positional_list ("," ws attr_named_list)?)? ")"
rule attr_named_list = attr_named ("," ws attr_named)* ("," ws)?
rule attr_named = raw_ident "=" ws attr_value
rule attr_positional_list = !(raw_ident "=") attr_value? ("," ws !(raw_ident "=") attr_value?)*
rule attr_value = "nil" word_end / "true" word_end / "false" word_end / ("-"? digits) ws / string ws / choice

rule operators_body = "{" {ml := false} all (operators_item item_end / ";" all)* "}"
rule operators_item = operators_skip / operators_operand / operators_level
rule operators_skip = "skip" word_end choice
rule operators_operand = "operand" word_end choice ("=>" ws action)?
rule operators_level = "level" word_end ident? "{" all (operators_operator item_end / ";" all)* "}"
rule operators_operator = prefix_op / postfix_op / infix_op
rule prefix_op = "prefix" word_end choice ("=>" ws action)?
rule postfix_op = "postfix" word_end choice ("=>" ws action)?
rule infix_op = "infix" word_end ("left" / "right" / "none") word_end choice ("=>" ws action)?

// Both braced forms are statement lists; execution support is checked separately.
rule predicate = action_block
rule action = "..." / action_block / term
rule action_block = "{" {ml := false} all (action_statement item_end)* "}"
rule action_statement = assign / term
rule assign = ident ":=" ws term
rule term = lambda / or_term
rule lambda = lambda_params ws "=>" ws term
rule lambda_params = "(" {ml := true} ws (ident ("," ws ident)*)? ")"
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
rule term_primary = digits ws / string ws / capture_ref / "true" word_end / "false" word_end / "nil" word_end / constructor / call / term_group ws / raw_ident no_paren ws
rule capture_ref = "$" (raw_ident / digits) ws
rule constructor = raw_ident ws constructor_fields ws
rule constructor_fields = "{" {ml := true} ws (field_init ("," ws field_init)* ("," ws)?)? "}"
rule field_init = ident ":" ws term
rule call = raw_ident call_args ws
rule call_args = "(" {ml := true} ws (term ("," ws term)*)? ")"
rule term_group = "(" {ml := true} ws term ")"

// Horizontal trivia may contain comments, but not a physical line terminator.
rule ws = {ml} all / horizontal
rule horizontal = (space_no_line / line_comment / block_no_line)*
rule all = (space / comment)*
rule line_trivia = newline / block_with_line
rule newline = "\r\n" / [\r\n]
rule space = [ \t\n\u{b}\u{c}\r\u{85}\u{a0}\u{1680}\u{2000}-\u{200a}\u{2028}\u{2029}\u{202f}\u{205f}\u{3000}]
rule space_no_line = !newline space
rule comment = line_comment / block_comment
rule line_comment = "//" [^\n\r]*
rule block_comment = "/*" (block_comment / !("/*" / "*/") .)* "*/"
rule block_no_line = "/*" (block_no_line / !("/*" / "*/" / newline) .)* "*/"
rule block_with_line = "/*" (block_no_line / !("/*" / "*/" / newline) .)* (newline / block_with_line) (block_comment / !("/*" / "*/") .)* "*/"
rule word_end = !ident_char ws
rule ident = raw_ident ws
rule raw_ident = !"_" ident_start ident_char*
rule ident_start = letter / "_"
rule ident_char = letter / digit / "_"
// Unicode letter categories are approximated here; the lexer checks them exactly.
rule letter = [a-zA-ZªµºÀ-ÖØ-öø-˿Ͱ-\u{10ffff}]
rule digit = [0-9]
rule digits = digit+
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
- **Statement boundaries.** Newlines and semicolons separate declarations and
  Operators Rule items. Struct fields use commas or newlines. An expression may
  cross a newline only within its own open delimiter; statement-list braces do
  not enable continuation of every nested expression. Indentation is not syntax.
- **Attribute arguments.** Positional arguments follow the generic value
  forms and precede named arguments; repeated named keys are rejected. The
  grammar models argument shape and ordering, not key uniqueness.
- **Contextual words.** Declaration and item introducers have their syntactic
  meaning only at those statement positions. They remain identifiers elsewhere.
- **Where constructs may appear.** An Operators Rule MUST be a complete rule
  declaration; a lambda MUST be an argument of `foldl`, `foldr` or `map`; and
  `#stream` MUST be at the top level of a rule body. A comment in a declaration
  or Operators Rule statement header is rejected by semantic analysis with a
  positioned diagnostic. These checks follow parsing.
- **Action blocks.** Multiple-statement Predicate Actions and every braced
  Value Action are retained in the source AST but compilation rejects them.
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
