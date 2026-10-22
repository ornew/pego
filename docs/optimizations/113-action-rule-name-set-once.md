# 113. The Action's Rule Name Is Set, Not Restored

- **Status**: Implemented
- **Date**: 2026-10-22

## Problem and Mechanism

A direct rule with an action saved `p.Where` (the rule whose action is being
evaluated), set it to the rule's name, ran the action and wrote the saved value back:
three statements and a saved variable for every action: 422 actions (844 removed statements) in the duckdb parser.

`p.Where` is read in one place, the parse-level handler that turns an `evalError` into
the error text `action in <rule>: ...`. Actions never nest, predicates and bindings
recover their own `evalError`, and the errors of the result conversion are fatal
errors with their own text, so an `evalError` reaches the handler only from inside an
action, after that action set the name. Restoring the previous value therefore changes
nothing observable. The generated action now sets `p.Where` and leaves it.

`TestGeneratedDirectActionErrorContextAndParserReuse` compares the error text with the
engine, including a failing action that runs after another rule's action succeeded.

## Results

`parser.go` with `-typed-only`, bytes and lines before and after:

| Parser | Bytes before | Bytes after | Lines before | Lines after |
|:--|--:|--:|--:|--:|
| duckdb | 2,207,830 | 2,190,528 | 114,168 | 112,902 |
| typescript | 1,352,046 | 1,340,814 | 68,061 | 67,245 |
| python | 805,025 | 798,059 | 38,640 | 38,136 |
| golang | 706,034 | 698,572 | 36,536 | 35,990 |
| cue | 490,503 | 486,676 | 23,951 | 23,672 |
| cel | 146,061 | 145,241 | 7,349 | 7,289 |

The duckdb test binary shrinks from 17,078,738 to 17,029,202 bytes (-0.3%). Parse speed
(30 interleaved rounds, minimum, an identical copy as the control) is within noise:
python +0.40% (control -0.11%), cue -0.26% (+0.10%), duckdb -0.93% (-0.38%), cel
-0.85% (-0.08%).

## Decision and Limits

Adopted. A future action-like evaluation that nests parsing inside an action, or an
`evalError` raised outside an action and outside a predicate, must set or restore the
name itself.
