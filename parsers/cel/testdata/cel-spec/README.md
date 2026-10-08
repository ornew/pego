# cel-spec conformance tests

The `*.textproto` files and `LICENSE` are copied from the directory `tests/simple/testdata` and the root of
[google/cel-spec](https://github.com/google/cel-spec), commit 40a3c9007a9305d1f2638d5f538a4e011732cace (the `master`
branch of 2026-09-16, Apache License 2.0). They are the data files of the `simple` conformance suite: tests of the
CEL language in the protobuf text format (`cel.expr.conformance.test.SimpleTestFile`), each with an `expr` field that is
the expression to evaluate.

The tests of this module read the `expr` of each test (2,527 expressions, 2,263 different ones) and require
every one to parse. The suite does not mark tests that must fail to parse, so the rejection of invalid expressions
is checked against cel-go instead (`../ref`).
