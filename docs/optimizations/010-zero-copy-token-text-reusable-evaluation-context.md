# 10. Zero-copy token text; reusable evaluation context

- **Token text.** `Match.Text` and terminal texts were built by converting `[]rune` (code points) or `[]byte` back to a
  string. The input now keeps the source string, plus a rune-to-byte offset table in code-point mode, and token text
  is a substring. Inputs with invalid UTF-8 in code-point mode keep copying, so U+FFFD replacement is unchanged; stream
  inputs also keep copying. Trade-off: token strings keep the whole input alive, like any Go substring.
- **Evaluation context.** Each action and predicate allocated an `evalCtx`. Evaluations never nest and no reference to
  the context survives (function values cannot be stored in fields or variables), so one context per parser is reused.
- Effect: allocations per parse JSON 230 k → 153 k, minilang 176 k → 137 k, CSV 166 k → 131 k.
