# Attributes

An `@` attribute carries diagnostic or metadata information. It does not change
which input an expression matches. Attributes are postfix annotations on the
immediately preceding expression and apply from left to right in written
order. Parenthesize a sequence when an attribute should apply to the complete
sequence rather than its final item. Behavioral and structural `#` pragmas are
defined separately in [Pragmas](pragmas.md).

Attribute arguments consist of positional arguments followed by named
arguments. A positional argument may be `nil`, so an omitted slot remains
distinct from an absent argument. Duplicate named arguments are errors. Only
`@error` is defined by this specification; unknown attribute names and invalid
arguments or targets are grammar errors. No user-defined attribute extension
mechanism is specified.

```pego
rule stmt = (let / expr) @error("expected a statement")
```

| Attribute | Description |
|:--|:--|
| `@error("...")` | Changes failure diagnostics without changing matching or recovery |

## `@error`

When an expression fails, `@error("...")` changes the diagnostic at the
farthest position reached inside it. It does not alter matching, backtracking,
recovery, captures, values, or spans. A named form such as
`@error(message="...")` is equivalent. When several messages apply at one
position, they are joined with `; `. If the expression succeeds, expectations
recorded inside it are kept as usual.

```pego
rule let = "let" " " name @error("expected a name") ";"
```
