# 006. Syntax Error Reporting and Error Recovery

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-07

## Summary

This record describes the design of syntax error reporting (positions and expectations, `#error`) and of the `#recover` attribute, which recovers from an error and continues parsing.
For the specification, see [spec/attributes.md](../../spec/attributes.md).

## Error reporting

In PEG, failures are absorbed by backtracking, so "where the parse failed" is not unique.
Following common practice, the parser reports **the farthest failure position** and the set of terminals expected at that position.

- Failures inside a lookahead are not reported, because `x` failing inside `!x` is the expected outcome.
- Failures inside a Pratt `skip` are not reported either, because they would be noise (such as whitespace).
- When a memoized result is used outside a lookahead but was computed inside one, it is recomputed, because no expectations were recorded for it.

### `#error`

A list of expectations exposes the grammar's internal structure and can be hard to read. `#error(message=...)` records the expectations inside its expression separately and, if the expression fails, replaces them with the message.
The reported position is the farthest position reached inside the expression, not the start of the expression. When `name` in `"let" name` is partially read, this points to a more accurate position.

## Error recovery

### Approaches considered

| Approach | Overview | Decision |
|:--|:--|:--|
| Labeled failures with recovery rules | Attach labels to failures and define a recovery rule per label outside the grammar | Expressive, but the mapping between labels and recovery rules must be maintained separately |
| Synchronization tokens | On failure, skip to a specific token | Simple, but does not fit PEGO, which has no tokens (no scanner) |
| **`#recover(skip=expr)` on an expression** | On failure, skip the input described by a parser expression | Adopted. Where to recover and how to skip are written in the same place. |

### Recovered errors and backtracking

Recovery turns a failure into a success, so it can also happen on a path that is later backtracked. Errors recovered on such a path must not be reported.
Recovered errors are therefore kept as an undoable record, like captures, and are undone when the parser switches alternatives (`mark`/`reset`).
The result of a memoized rule also stores the errors recovered during that call, and they are recorded again when the memo entry is used. For Pratt operator candidates, only the errors of the selected candidate are kept.

### Preventing infinite loops

If `skip` consumes no input, no recovery takes place. This prevents a `#recover` inside a repetition from producing empty `Error` nodes forever at the end of the input.

### When the whole parse fails

If the input as a whole does not match despite recovery, the recovered errors belong to a path that was ultimately not taken. In that case only the last syntax error is returned.
