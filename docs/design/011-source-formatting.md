# 011. Source formatting that keeps comments

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-07

## Summary

`pego fmt` formats PEGO source files like `gofmt`: it reads `.pego` files and prints them (or rewrites them with `-w`) in a canonical layout, keeping every comment.
Conversion between PEGO source, JSON and compiled grammars moved to `pego convert`.

Before this change the lexer discarded comments and `grammar.Format` printed only the AST, so formatting a file lost its comments.

## Design

### What the AST records

The parser records layout in `grammar.LineBreak` values. A `LineBreak` belongs to a place where a line may start, and holds:

| Field | Meaning |
|:--|:--|
| `Comments` | Comments on their own lines before the node, in source order. Each `Comment` records whether a blank line precedes it |
| `Blank` | Whether a blank line directly precedes the node |
| `Trailing` | The comment at the end of the line that starts with the node |

Line breaks are recorded at:

- the package clause, every `type` and `def`, and the end of the file (`Grammar.PackageBreak`, `TypeDef.Break`, `RuleDef.Break`, `Grammar.EndBreak`);
- struct fields and the closing brace of a struct (`Field.Break`, `StructSpec.CloseBreak`);
- the `skip`, `operand` and `level` items of a pratt expression, the operators of a level, and their closing braces;
- optional line breaks in a rule body: after `=` (`RuleDef.BodyBreak`), before `->` (`RuleDef.ActionBreak`), before the alternatives of the top-level choice (`Choice.Breaks`), before the items of the top-level sequences (`Seq.Breaks`), and before the attributes of those items (`Attributed.Breaks`).
  These are recorded only when the source has a line break there, and `Format` keeps them.

`StructSpec.OneLine` and `PrattLevel.OneLine` record that a struct or level was written on one line; `Format` keeps such blocks on one line.

All of these fields are tagged `json:"-"`, so `grammar.MarshalJSON` produces the same bytes as before, whether or not the grammar came from source with comments.
The compiled grammar format (`.pegoc`) does not store them either.

### Attaching comments

The lexer attaches to each token the comments between it and the previous token, and separates a comment on the line of the previous token (an end-of-line comment) from comments on their own lines.
It also records whether a blank line precedes each comment and token.

The parser keeps the line break it recorded last.
When it records a new line break at a token:

- the token's end-of-line comment becomes the `Trailing` comment of the previous line break, because the formatter ends that line with the token before;
- the token's other comments become the `Comments` of the new line break.

When the parser consumes a token that does not start a recorded line break, the comments before it cannot stay in place: `Format` writes the token on the same line as the tokens before it, and a line comment cannot appear in the middle of a line.
The parser adds them to the `Comments` of the line break recorded last, so `Format` writes them on their own lines at the nearest preceding line break.
No comment is lost; a test checks that every comment the lexer finds is in the AST and in the formatted output.

Attaching trailing comments to the line they end, rather than to the line break after them, keeps them right when `Format` reorders items: it writes `skip` before `operand` and `operand` before `level`, whatever the source order.

### Printing

`Format` collects output lines and then aligns:

- the types of struct fields on consecutive lines (a blank line or a comment line ends the run);
- trailing comments on consecutive lines with the same indentation.

Blank lines are kept but collapsed to one, and dropped at the start of a block, before a closing brace and at the end of the file.
A grammar without layout information (for example, one decoded from JSON) is printed as before: a blank line between definitions and one item per line in blocks.

Formatting is idempotent: formatting the output again gives the same text, because each recorded line break reproduces itself.
Tests check this, and that every grammar in `examples/` reparses to the same AST (compared through JSON) with the same comments.

### Considered alternatives

- **A comment list ordered by position, merged while printing** (as in `go/printer`): it needs the source position of every place the printer writes, including the end of nodes, which the AST does not have. Attaching comments in the parser needs no positions.
- **Keeping comments in the JSON form**: JSON is the interchange format for tools that build grammars; layout belongs to the source. Leaving it out keeps the JSON stable.

## Limitations

- A rule body keeps its line breaks only in its top-level choice and sequences. Line breaks inside parentheses, predicates, actions, and pratt operands and operators are joined, and comments there move to the nearest preceding line.
- Alignment that `Format` does not produce itself (for example, aligned columns in pratt levels) is not kept.
