# 007. Stream Parsing and Incremental Parsing

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-07

## Summary

This record describes the design of stream parsing, which starts parsing before the whole input is available, and of incremental parsing, which reuses the results for unchanged parts of the input after an edit.

## Stream parsing

### Committing elements

Because PEG backtracks, whether the result for a range of input will be part of the final result is, in general, not known until the end of the input.
The design therefore lets the grammar author mark where the input may be split, with the `#stream` attribute (see [spec/attributes.md](../../spec/attributes.md)).

- `#stream` can be attached only to a repetition at the top level of the start rule's body. No choice encloses the top level of the start rule's body, so once an element of the repetition has matched, parsing never returns to a point before it.
- After an element has been passed to the caller, the input and memo entries before it are discarded. Positions remain absolute offsets from the start of the input; line and column numbers are computed by counting the lines in the discarded part.
- The preceding character is kept so that line-start checks (`^`) still work.

An alternative considered was to commit when a cut is passed. A cut commits a choice, but its effect does not extend to choices outside the rule, so it is not a sufficient condition for discarding input. This alternative was not adopted.

### Reading input

Input is read up to the position that is needed, and after that only what is already in the read buffer is taken in. Because the parser never waits for more input than it needs, elements can be delivered as soon as their input arrives, even when input trickles in, as over a network.

## Incremental parsing

As in Dubroy and Warth's "Incremental Packrat Parsing", each memo entry records **the range of input it examined**, and entries that do not overlap the edit are reused.

| Relation between the examined range and the edit [start, end) | Treatment |
|:--|:--|
| The examined range ends at or before start | Reused as is |
| The examined range starts at or after end | Reused with positions shifted by the change in length |
| Otherwise | Discarded |

- The examined range includes lookahead and checks of the next character (such as the check that ends a `*`). A line-start check (`^`) examines the preceding character, so the examined range can begin before the start of the match.
- Nodes in shifted entries are copied with shifted positions when they are used, which avoids copying entries that are never used.
- Entries of rules whose results may contain position values (`startPos`, `endPos`) are not shifted; this covers rules whose actions or predicates refer to them and the rules that call those rules. Positions stored as integers cannot be shifted the way node positions can.
- Entries that contain recovered errors are not shifted either, because error messages include positions.
- Rules that refer to variables are never memoized in the first place.
- So that expectations for syntax errors can be recorded again when a memo entry is used, each entry also stores the farthest failure position and the expectations recorded during the call. A parse that reuses entries therefore reports the same syntax errors as a fresh parse.

### Verification

A test applies random edits repeatedly and checks that the result of incremental parsing (the node tree, including positions, and the syntax errors) equals the result of parsing the edited text from scratch (`internal/engine/document_test.go`).
