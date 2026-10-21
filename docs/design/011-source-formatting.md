# 011. Source Formatting That Keeps Comments

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-07

## Summary

`pego fmt` formats PEGO source files like `gofmt`: it reads `.pego` files and prints them (or rewrites them with `-w`) in a canonical layout, keeping every comment.
Conversion between PEGO source, JSON and compiled grammars moved to `pego convert`.

Before this change the lexer discarded comments and `grammar.Format` printed only the AST, so formatting a file lost its comments.

## Design

### What the AST Records

The parser records layout in `grammar.LineBreak` values. A `LineBreak` belongs to a place where a line may start, and holds:

| Field | Meaning |
|:--|:--|
| `Comments` | Comments on their own lines before the node, in source order. Each `Comment` records whether a blank line precedes it |
| `Blank` | Whether a blank line directly precedes the node |
| `Trailing` | The comment at the end of the line that starts with the node |

Line breaks are recorded at:

- the package clause, every `type` and `rule`, and the end of the file (`Pkg.Break`, `TypeDef.Break`, `TypeAlias.Break`, `RuleDef.Break`, `File.EndBreak`);
- struct fields and the closing brace of a struct (`Field.Break`, `StructType.CloseBreak`);
- the `skip`, `operand` and `level` items of an Operators Rule, the operators of a level, and their closing braces;
- optional line breaks in a rule body: after the opening brace (`RuleDef.BodyBreak`), before `=>` (`RuleDef.ActionBreak`), before the alternatives of the top-level choice (`Choice.Breaks`), before the items of the top-level sequences (`Seq.Breaks`), and before the attributes of those items (`Attributed.Breaks`).
  These are recorded only when the source has a line break there, and `Format` keeps them.

`StructType.OneLine` and `OperatorsRuleLevel.OneLine` record that a struct or level was written on one line; `Format` keeps such blocks on one line.

All of these fields are tagged `json:"-"`, so `grammar.MarshalJSON` produces the same bytes as before, whether or not the grammar came from source with comments.
The compiled grammar format (`.pegoc`) does not store them either.

### Attaching Comments

The lexer attaches to each token the comments between it and the previous token, and separates a comment on the line of the previous token (an end-of-line comment) from comments on their own lines.
It also records whether a blank line precedes each comment and token.

The parser keeps the line break it recorded last.
When it records a new line break at a token:

- the token's end-of-line comment becomes the `Trailing` comment of the previous line break, because the formatter ends that line with the token before;
- the token's other comments become the `Comments` of the new line break.

When the parser consumes a token that does not start a recorded line break, the comments before it cannot stay in place: `Format` writes the token on the same line as the tokens before it, and a line comment cannot appear in the middle of a line.
The parser adds them to the `Comments` of the line break recorded last, so `Format` writes them on their own lines at the nearest preceding line break.
Parenthesized type and action expressions retain source-only group nodes so comments within those delimiters can keep their owner path when formatting.
No comment is lost; tests check that every comment the lexer finds is in the AST and in the formatted output.

Attaching trailing comments to the line they end, rather than to the line break after them, keeps them right when `Format` reorders items: it writes `skip` before `operand` and `operand` before `level`, whatever the source order.

### Printing

`Format` collects output lines and then aligns:

- the types of struct fields on consecutive lines (a blank line or a comment line ends the run);
- trailing comments on consecutive lines with the same indentation.

Blank lines are kept but collapsed to one, and dropped at the start of a block, before a closing brace and at the end of the file.
A grammar without layout information (for example, one decoded from JSON) is printed as before: a blank line between definitions and one item per line in blocks.
The comments such a grammar owns (`SourceInfo.LeadingComments`, `LeadingDetachedComments` and `TrailingComments`, which JSON carries) are written for the package clause, the declarations, the fields of a struct and the lines of an operators rule, as the same comment lines and trailing comments that layout would hold.
A node with a `LineBreak` keeps its layout comments, so a parsed grammar never writes a comment twice.
A string that ends in a line break becomes `//` lines and any other string a block comment.

A comma-separated list in brackets follows one rule, as in other formatters: it is written on one line unless it ends in a trailing comma or holds a comment that needs its own line, and then each element has a line of its own and a comma.
The parser records the trailing comma in `TrailingComma` instead of the source lines, so the layout depends on what the author wrote on purpose, not on where the line breaks happened to fall.
The comma that a one-element tuple needs does not count.
A block comment between two tokens of one line does not break the list: its owner is decided by that line (see [Comment Grouping and Ownership](../../spec/grammar-files.md#comment-grouping-and-ownership)).
After an opening delimiter or a comma it leads the next element, and before the closing delimiter it trails the last one, so writing the list on lines keeps the owner: the comment takes a line before the element, or goes after the comma of the last element.
A block comment that ends its line does break it, and goes after the comma of its element, where it still trails the element.
Type lists, attribute arguments and lambda parameters follow the same rules as the lists of terms.
A block comment after an opening delimiter touches it: `f(/* a */ 1)`.
`#repeat` is not a list: its comma is part of its syntax, so its bounds are written in one canonical spelling (`*`, `+`, `#repeat(n)`, `#repeat(n,)`, `#repeat(,m)`, `#repeat(n, m)`), and each comment between its tokens stays before the same token, or moves to the next one the canonical spelling has.
A repetition with such a comment keeps the pragma form, since `*` and `+` have no place for it.
The comma of a list is a token of its own, like a statement separator: a comment on its line trails the element before it, as `gofmt` places it, so `2, // c` keeps `// c` with `2`.
Two line comments around one comma cannot share a line, so the comma stays on the line after the first.
A comment that is not on the line of the comma never trails the element before it.
A blank line before the first comment of a list is kept, because it decides whether the comment is detached.
Tests format random lists, types and attributes with comments in every position and check that the result parses to the same AST with each comment owned by the same node.
A construct written on one line, such as a group, a predicate block or a level, that holds a list on lines is written on lines itself, because the next pass would read it that way.

A block comment between two tokens of a rule expression, such as the one in `"a" @custom /* c */ "b"`, is kept in the `SourceLayout` of the expression next to it: `Before` for a comment before its first token, `After` for one before the `/` or `=>` that follows, `Operator` for one before a suffix or a pragma, and `Close` for one before the parenthesis of `#recover`.
The parser takes such a comment only when no line break stands between the two tokens and the comment but the lines inside a block comment, so the formatter writes it back where it was and the owner does not change.
Inside delimiters a comment may also stand on a line of its own, between a binary operator and an operand, between `/` and the alternative after it, after a label or a prefix operator, before a `*`, `+` or `?`, before the `=>` of a lambda, or inside the parentheses of `#recover`. Those gaps are kept as a `Gap`: the comments with the lines of the tokens around them (`OpGap` before a binary operator, a suffix or the arrow of a lambda, `Lead` before the first token of an alternative, of an operand of a label or a prefix operator, or of the skip expression, and `Close` before the parenthesis), so the formatter writes each comment on the line it was on and the owner does not change.
Comments in the other gaps still go to the line break nearest before them.
An action block keeps the comment that ends the line of its `{` in `OpenBreak`, the comments around a `;` in the layout of the statements, and a statement that follows a `;` and a comment on its line stays on that line.
An index keeps the layout of its brackets in `OpenBreak`, `BodyBreak` and `CloseBreak`, as a `TermGroup` does.

Formatting is idempotent: formatting the output again gives the same text, because each recorded line break reproduces itself.
Tests check this, and that every grammar in `examples/` reparses to the same AST (compared through JSON) with the same comments.

### Considered Alternatives

- **A comment list ordered by position, merged while printing** (as in `go/printer`): it needs the source position of every place the printer writes, including the end of nodes, which the AST does not have. Attaching comments in the parser needs no positions.
- **Keeping comment tokens in the JSON form**: JSON is the interchange format for tools that build grammars; layout belongs to the source. Leaving it out keeps the JSON stable. The owned comments it does carry are enough to write the comments of the nodes on their own lines again.

## Limitations

- A rule body keeps line breaks at its top-level choice and sequences. Source-only PEG, type and action group nodes preserve layout and comment ownership inside their explicit delimiters. `TypeGroup` and `TermGroup` keep raw line placement in `OpenBreak`, `BodyBreak`, `CloseBreak` and `OneLine`, separate from normalized `SourceInfo` ownership. Other internal line breaks, including those inside Operators Rule operands and operators, are joined, and comments there move to the nearest preceding line.
- Alignment that `Format` does not produce itself (for example, aligned columns in Operators Rule levels) is not kept.
