# Parsing Context-Sensitive Input

A PEG matches what the grammar says, regardless of what came before. Some languages need memory: a closing tag must
repeat the name of the opening tag, and a name may be limited in length. This lesson adds checks and memory to a grammar
with predicates and variables.

## What You Will Build

A grammar for nested tags such as `<a><b>x</b></a>` that rejects a mismatched closing tag with a clear message.

## Before You Start

Complete [Reporting Errors](errors.md). This lesson uses `@error` messages.

## Checking a Condition

A **predicate** is a Go-like expression in braces. It consumes no input, and the parse continues only if it is true.
Captured values are read with `$name`, and `text($name)` gives the matched text. Here a name may have at most eight
letters. Save as `names.pego`:

```pego
rule main = name ("," name)* $$

rule name = n:[a-z]+ #atomic {len(text($n)) <= 8} @error(message="a name has at most 8 letters")
```

```bash
pego parse -g names.pego -f sexpr -i 'alpha,beta'
pego parse -g names.pego -f sexpr -i 'alpha,beta,verylongname'
```

```text
(("alpha") [("," ("beta"))])
pego: 1:24: a name has at most 8 letters
```

A predicate has no value, so each `name` is a tuple with a single element, the text of the name. The predicate is evaluated after `[a-z]+` matched, so the error is at the position after the name. A predicate that
fails records no expected item of its own, which is why the grammar attaches `@error` to it; without the label the
message would be a bare `syntax error`.

## Remembering a Value

A **variable** is defined in a predicate with `:=` and can be read by every rule that is called afterwards. The tags
grammar defines `tag` after the opening tag and compares it in the rule that matches the closing tag. Each `element`
defines its own `tag`, and the definition disappears when the rule returns, so nesting works without further code. Save
as `tags.pego`:

```pego
type Name = node[string]
type Chars = node[string]
type Element = { Name Name; Children []Content }
type Content ~ Element | Chars

rule main Element = e:element $$ => $e

// The open tag defines `tag`; every rule called after that can read it.
rule element Element = "<" n:name ">" {tag := text($n)} cs:content* close_tag => Element{Name: $n, Children: $cs.Value}
rule content Content = element / chars
rule chars Chars = ([^<]+) #atomic

// The close tag is parsed by another rule: it finds the name in `tag`.
rule close_tag = "</" n:name {text($n) == tag} @error(message="mismatched end tag") ">"

rule name Name = ([a-z]+) #atomic
```

```bash
pego parse -g tags.pego -f sexpr -i '<a><b>x</b><c>y</c></a>'
pego parse -g tags.pego -f sexpr -i '<a><b>x</a></b>'
pego parse -g tags.pego -f sexpr -i '<a><a>x</a>'
```

```text
Element{Children: [Element{Children: ["x"], Name: "b"} Element{Children: ["y"], Name: "c"}], Name: "a"}
pego: 1:11: mismatched end tag
pego: 1:12: syntax error: expected "<", "</", [^<]
```

The mismatch is reported where the predicate ran, after the offending name. An unclosed element fails the ordinary way,
because `content*` stops at the end of the input and `close_tag` cannot match.

## Troubleshooting

- A predicate that is not a comparison, such as `{len($s)}`, succeeds for every value except `false`. Compare
  explicitly.
- `{ $a == $b }` on two captures compares nodes, not text. Compare `text($a) == text($b)`.
- A misspelled variable name makes the predicate fail silently, so check the names first when a predicate never
  succeeds.

## Next Steps

[Testing and Linting a Grammar](testing-and-linting.md) turns the checks you made by hand into a suite. The guide
[Context-Sensitive Parsing](../guide/context-sensitive.md) covers variables, lookahead and attributes, with recipes for
indentation blocks, here-documents and long brackets, and the cookbook has a recipe for
[indentation-based input](../cookbook/indentation.md).
