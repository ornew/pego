# Parsing a Named Prefix

`Parser.Parse` requires its start rule to consume the complete supplied input.
`Parser.ParsePrefix` selects a rule for one call and returns its successful
endpoint without imposing an additional end-of-input check. This is useful
when a larger document contains a contiguous fragment described by another
rule.

## Values and Consumption

```go
package main

import (
    "fmt"
    "log"

    "github.com/ornew/pego"
)

func main() {
    parser, err := pego.CompileSource(`
rule main = value $$
rule value = x:"é😀" "!" => $x
`, "main")
    if err != nil {
        log.Fatal(err)
    }

    source := "before:é😀!remaining"
    start := len("before:") // Byte offset of a contiguous fragment.
    result, err := parser.ParsePrefix("value", source[start:])
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(result.Consumed, result.ConsumedBytes) // 3 7
    fmt.Println(source[start+result.ConsumedBytes:]) // remaining
}
```

`PrefixResult.Node` is the selected rule's value. `Consumed` is the successful
cursor in the selected parser `Unit`, defaulting to code points.
`ConsumedBytes` is the endpoint in the original UTF-8 bytes of the supplied
input. Malformed UTF-8 bytes each count as one replacement code point, while
the byte endpoint still refers to the original source.

The example's action returns only `é😀`, but the rule consumes the following
`!` too. The consumption fields come from the parser cursor, independently of
`Node.Start` and `Node.End`. Actions can return nil, and recognition builds no
node. Neither implies that parsing failed or consumed zero input. A nullable
rule can succeed with zero consumption.

All node spans, action positions and syntax-error positions remain local to the
supplied input. Add the fragment's starting byte offset to `ConsumedBytes` to
obtain the consumed endpoint in the complete source. To translate a code-point
node span, first convert that local span to source bytes; adding a byte base
directly to a code-point span gives incorrect coordinates for multibyte text.
With `WithUnit(pego.Bytes)`, node spans and both consumption fields use bytes.

## Recovery and Failure

Recovered success returns both the result and the existing `SyntaxErrors`
error, just as whole-input parsing returns a node alongside recovered errors.
Inspect the error type before discarding a recovered result:

```go
result, err := parser.ParsePrefix("value", input)
var recovered pego.SyntaxErrors
switch {
case err == nil:
    // Successful prefix; use result and its consumption fields.
case errors.As(err, &recovered):
    // Recovered success; use result and report the recovered diagnostics.
    for _, diagnostic := range recovered {
        fmt.Println(diagnostic)
    }
default:
    // Unrecovered syntax failure or fatal error; result is the zero value.
}
```

This snippet needs the standard `errors` package. A failure after an earlier
recovery still returns a zero result. No speculative endpoint is exposed on
failure. A zero result by itself cannot distinguish failure from a successful
zero-width rule returning nil; use the error.

## Options and Boundaries

`ParsePrefix` supports position units, backends, recognition, tracing, nesting
limits, context and work limits through `ParseOption`. Positive
`WithMissingTokens` limits are rejected: insertion recovery is restricted to
Closure whole-input parsing and recognition. This restriction applies even
when the grammar contains no `#missing` annotation. Grammars containing
`#missing` are themselves unsupported by `ParsePrefix`, even when the option
is omitted or zero. Zero preserves strict prefix matching for grammars without
`#missing`; options still resolve in order.

`ParsePrefix` does not change the parser's configured start rule and can run
concurrently or reentrantly on the same parser. Stream diagnostic callbacks
are invalid for this batch operation, as they are for `Parse`.

An explicit `$$` assertion still sees the actual end of the entire supplied
string. To confine it to a known fragment, slice both the beginning and end of
that fragment before calling `ParsePrefix`. Cuts and left recursion retain
their existing semantics. The API does not share memo entries with a containing
`Document`, shift positions to absolute coordinates, or map escaped or
noncontiguous fragments.

Compiled parsers use the same named-prefix entry through the bytecode backends.
Recognition retains the compiled grammar's existing recognition-bundle
requirements. Generated standalone parsers keep their existing APIs; this
method belongs to the runtime `Parser`.
