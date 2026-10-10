# 1. Value-free rule bodies, value-free twins, transient rules (0b2c903)

- **Value-free bodies.** The body of a terminal-type rule, or of a rule whose action does not use `$n`, is compiled
  without building values. Captures still build theirs.
- **Twins.** A CST rule (no action, no terminal type) called where its value is discarded (`@`, `-`, `!`, value-free
  bodies) gets a value-free twin with its own memo entries. Rules in left-recursive cycles never get twins, because
  mixing a valued and a value-free version inside one cycle would change seed-growing semantics.
- **Transient rules.** Rules that call no other rule, or that are referenced only once in the grammar, are not memoized
  in normal parses: their entries can never be reused (a single-reference rule is re-invoked at a position only when its
  unique caller is, and the caller chain ends at a memoized rule or the start rule). `Document` still memoizes them so
  edits can reuse results.
- **Shared empty frame** for capture-free scopes.
- Effect: JSON 442 → 144 ms, 183 → 76 MB.
