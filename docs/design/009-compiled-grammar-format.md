# 009. Saving and Loading Compiled Grammars (.pegoc)

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-07

> **Note (2026-10)**: Version 2 of the format, introduced by [010](010-bytecode-vm.md), stores the bytecode module and makes the AST optional. Version 1, described below, can still be loaded. The current format is defined in [bytecode.md](../../spec/bytecode.md#file-format).

## Summary

Save a compiled grammar to a file, and later load it and run the parser without compiling again (`pego compile`, `Parser.MarshalBinary`, `LoadParser`).
This is useful for distributing grammars, or when the grammar should not be compiled on every start-up.

## What to save

The engine assembles parser expressions into Go functions (closures) and executes those, so the executable form itself cannot be saved as bytecode.
Instead, the cost of each stage from source to a ready-to-run parser was measured, and the results of the expensive stages are saved (measured with `examples/minilang`).

| Stage | Time | When loading saved data |
|:--|--:|:--|
| Parsing (source → AST) | about 0.31 ms | Skipped (the AST is saved) |
| Static analysis (empty matches, left recursion, variable and position dependencies) | about 0.05 ms | Skipped (the results are saved per rule) |
| Type inference and type checking | about 0.39 ms | Skipped (checked when saving) |
| Assembling functions | about 0.07 ms | Performed |

Preparing from source takes about 0.78 ms, while loading saved data takes about 0.11 ms.

## Format

```
magic    "PEGOC\x00"
version  1 byte
payload  string table, package name, default start rule, statements (type and rule definitions)
crc32    CRC-32 (IEEE, little-endian) of everything from magic through payload
```

- Integers are variable-length, and strings are represented by indices into the string table. Expressions, value expressions and types are written as one byte for the kind followed by their elements in pre-order.
- Each rule definition stores, in addition to the AST, whether the rule is memoized, whether it is a left-recursion leader, and whether it depends on positions (the static-analysis results).
- Source positions are not saved.
- The same grammar always produces the same bytes (loading and saving again does not change them).

### Alternatives considered

- **The grammar JSON format with the analysis results added**: readable, but large and slow. The grammar JSON remains the format for two-way conversion, and this format is binary.
- **gob**: tightly bound to Go types, which makes it hard to manage format compatibility ourselves.

## Safety and compatibility

- Data in a different format version is not loaded. The version is increased whenever the format changes.
- Corruption is detected by the checksum. Even with a valid checksum, malformed structure (out-of-range indices, excessive nesting, trailing bytes) is reported as an error rather than causing a panic (confirmed with randomly corrupted data and fuzz tests).
- Loading performs structural checks such as name resolution, but no type checking. Data crafted to contain type errors can cause run-time errors in actions during parsing. Only data from trusted sources is assumed to be loaded.

## Verification

For every grammar in `examples/` and in the engine tests, the tests check that a parser that was saved and loaded returns the same results (the node tree, including positions, and the errors) as the original parser, and that saving a loaded parser again produces the same bytes (`internal/engine/compiled_test.go`).
