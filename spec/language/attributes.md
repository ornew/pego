# Attributes

An `@` attribute carries diagnostic or metadata information. It does not
change which input an expression matches. Attributes in Rule Expressions are
postfix annotations on the immediately preceding expression and apply from
left to right in written order. Parenthesize a sequence when an attribute should apply to the
complete sequence rather than its final item. Behavioral and structural
`#` pragmas are defined separately in [Pragmas](pragmas.md).

Attribute arguments consist of positional arguments followed by named
arguments. A positional argument may be `nil`, so an empty argument is written
explicitly and remains distinct from an absent argument. A comma after the last
argument is a trailing comma and adds no argument. Duplicate named arguments are errors. This
specification defines `@error` and `@diagnostic`; unknown attribute names and
invalid arguments or targets are grammar errors. No user-defined attribute
extension mechanism is specified.

```pego
rule stmt = (let / expr) @error("expected a statement")
```

| Attribute | Description |
|:--|:--|
| `@error("...")` | Reports an explicit message when the expression fails |
| `@diagnostic(name="...")` | Replaces ordinary expected-item descriptions from a failed expression with a concise name |

Declaration attributes, written on the lines before `rule`, annotate a rule
declaration instead of an expression; see
[Declaration Attributes](grammar-files.md#declaration-attributes).

## `@error`

When an expression fails, `@error("...")` changes the diagnostic at the
farthest position reached inside it. It does not alter matching, backtracking,
recovery, captures, values, or spans. A named form such as
`@error(message="...")` is equivalent. When several messages apply at one
position, they are joined with `; `. If the expression succeeds, expectations
recorded inside it are kept as usual. An opt-in `#missing` insertion has its own
recovered diagnostic and inherits the outermost enclosing explicit `@error` label.
This does not change labels on successful consuming `#recover`.

```pego
rule let = "let" " " name @error("expected a name") ";"
```

## `@diagnostic`

`@diagnostic` takes exactly one named argument, `name`, whose value must be
a non-empty string literal. The value cannot contain CR, LF, U+0085, U+2028 or
U+2029. Duplicate `@diagnostic` attributes on one expression chain are
invalid.

The attribute wraps the expression immediately before it. On failure, it
replaces ordinary expected-item descriptions contributed by that wrapped
failure at its farthest position with `name`. It preserves that position,
expectations recorded at farther positions, and explicit `@error` messages.
On success, expectations already recorded by the expression remain. Matching,
backtracking, captures and values do not change.

Attribute order is significant because attributes wrap from the inside out.
For example, a diagnostic name placed before `#recover` can label a failure
that recovery handles; a diagnostic name outside recovery sees the recovery
result instead. When both `@diagnostic` and `@error` apply to a failure,
the explicit `@error` message remains visible.

```pego
rule number Number = ([0-9]+ "." [0-9]+) @diagnostic(name="number")
```

For input `12x`, the ordinary expected descriptions at the farthest position
are reported as `number`; the failure position is unchanged.
