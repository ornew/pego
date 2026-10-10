# 9. Value-free Pratt lines; discarding trivia in example grammars (936857c)

- A Pratt operator line with an action never uses the line's value (`$op` is built from the matched range and `$n` is
  not allowed there), and neither does an operand line whose action does not use `$n`. Such lines are now compiled
  without values (closure `leanLine`; bytecode uses the same predicate).
- The example grammars captured repetitions such as `rest:(ws "," ws m:member)*`. A captured value keeps its whole CST
  (it can be inspected with `.children` or `text`), so every whitespace character became a `Match` node. They now
  discard trivia with `-ws` (see the authoring guideline below). The resulting ASTs are identical.
- Effect: JSON 50.5 → 42.8 MB, minilang 32.8 → 23.0 MB per parse.
