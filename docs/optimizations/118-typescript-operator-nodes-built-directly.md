# 118. TypeScript Operator Nodes Are Built Directly

- **Status**: Implemented
- **Date**: 2026-10-24

## Problem and Mechanism

An Operators Rule whose result is a named node type (`type Expr = node[_]`) makes every
operator node of that type. In the TypeScript runtime an operator line without an action
built the tuple node of `(lhs, op, rhs)` and then converted it: `convertNode` allocated a
`TypedNode` that copied the range, the children and the fields and left the tuple node as
garbage. The Go runtime builds the named kind directly.

`operatorsRuleBuild` now constructs the `TypedNode` of the rule's operator type from the
children and fields it has just made, with the same range, an empty rule name and the
same capture labels as the converted node. The constructor of `TypedNode` takes those
parts instead of a node to copy from; `convertNode` keeps the copy for values that come
from actions. The node that an action returns is still converted.

## Results

Generated TypeScript parser of a grammar with five operator levels and a named result
type, one input of 139 kB (79,036 nodes), Node 24.13.0 on darwin/arm64, eight paired runs
alternating the parent and the change, 150 parses each after warm-up:

| | parent | change |
|:--|--:|--:|
| time per parse (median) | 31.4 ms | 29.8 ms (0.95x) |
| garbage collections per parse | 0.74 | 0.60 |
| time in garbage collection per parse | 3.25 ms | 2.5 ms |

The change was faster in seven of the eight pairs; one pair was 0.6% slower. The trees hash
identically (node type, constructor, rule, range, text, field names, child count) for the
parent and the change.

## Tests

The Operators Rule named-type cases run through the TypeScript backend, together with the
typed conformance and replay tests, and `parsers/test.sh`, pass.
