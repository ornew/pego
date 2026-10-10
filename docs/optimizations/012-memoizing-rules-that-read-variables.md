# 12. Memoizing rules that read variables

- Rules that read variables, directly or through callees, were never memoized, because their results depend on the
  environment. Context-sensitive grammars (indentation-based ones in particular) could then take exponential time,
  and the Python example had to be written in a parse-once-then-fold style to avoid it.
- The memo key now includes the values of the variables the rule may read (`rule.vars`, computed statically and
  sorted); definitions made inside a rule are undone when it returns, so these values determine the result. Generated
  parsers do the same (the generator emits each rule's variable list).
- Effect: removes the exponential worst case for variable-reading rules; no change for grammars without variables.
