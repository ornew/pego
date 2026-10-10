# 23. Splicing the text of a Document in place

- After change 20, most of an edit's cost was the text: `Edit` copied the code points into a new slice, converted them
  back to a string, and rebuilt the code-point-to-byte offset table by decoding the whole string. It now replaces the
  edited range in the code points (or bytes) in place, builds the new string by concatenating substrings of the old
  one, and splices the offset table, adding the byte difference to the entries after the edit
  (`input.replace`). Nodes keep referring to the old string, which does not change. `TestDocumentEditText` checks
  after random edits with multi-byte characters and invalid UTF-8, in both units, that the text and offsets equal
  those of the edited text read from scratch.
- Effect (the program in the streaming guide, 100,000 lines, Apple M3 Max): `Edit` 12.3 → 2.7 ms (one rule per line),
  16.5 → 3.4 ms (Pratt lines), 9.2 → 0.3 ms (no memo entries); JSON document of 283 KB 6.6 → 2.7 ms per edit.
