# Cancellation and Budget Limits

A parse of untrusted or very large input should be able to stop: when a caller gives up, when a deadline passes, or when
the cost exceeds a budget. This guide shows the per-call options that do that and what a stopped parse returns. The
nesting limit, which bounds rule calls, is described with the [backends](backends.md#deep-nesting-and-withmaxdepth).

The examples use a parser `p` compiled as in [Running Parsers](running-parsers.md). The repository's security policy,
`SECURITY.md` at the root of the repository, lists what a parse bounds and what the caller must bound.

## Cancelling a Parse

`Parse` accepts per-invocation `WithContext` and `WithLimits` options. A zero
limit selects the default described [below](#the-default-budget); `pego.Unlimited` disables the limit. The counter
resets for the next call. Generated Go parsers offer them only when generated with
`-budget-controls`; see [the generated Go API](generated-go-api.md#cancellation-and-budget-limits). Generated TypeScript parsers offer
the `signal`, `budget` and `maxInput` options only with `-budget-controls` too; see [TypeScript](typescript.md#cancellation-and-budget-limits).

```go
ctx, cancel := context.WithTimeout(context.Background(), time.Second)
defer cancel()
node, err := p.Parse(input,
    pego.WithContext(ctx),
    pego.WithLimits(pego.Limits{Budget: 5_000_000}),
)
switch {
case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
    // The invocation was cancelled.
case errors.Is(err, pego.ErrLimit):
    // A budget limit was reached; use errors.As for *pego.LimitError details.
case err != nil:
    // Syntax or another parse error.
case node != nil:
    // The parse completed.
}
```

`Budget` counts inspected input elements and backend execution steps in one counter, one unit each; it is neither a
time nor a memory limit, and backtracking never refunds it. Scanning input, such as a repeated character class,
charges it in proportion to the elements examined, so one limit bounds the cost of large input, rule calls and
values that grow. The memory a parse allocates, long values included, is at most proportional to the budget it
spends plus the input, so a smaller budget also bounds memory; there is no separate memory limit. An interruption is fatal to the parse and `#recover` cannot consume it,
so a batch parse returns no partial result. Cancellation is cooperative: a parser cannot preempt a blocked reader or
a callback of yours. The exact rules are in
[Cancellation and Budget Limits](../../spec/runtime/parsing.md#cancellation-and-budget-limits); the
[Streaming](streaming.md#cancelling-a-stream) guide shows how to cancel caller-owned I/O.

## The Default Budget

A parse with a zero `Budget` is not unbounded. It gets a budget of `max(2^20, 1024 * n)`, where `n` is the length of
the input in the position unit, so a parse of untrusted input stops without your choosing a limit. Input whose
cost per element is higher than 1024 on average, such as a grammar that backtracks heavily, fails with a
`*pego.LimitError`, whose `Limit` is the resolved budget. Pass a larger `Budget` when you expect that cost, or
`pego.Unlimited` when the input is trusted:

```go
node, err := p.Parse(input, pego.WithLimits(pego.Limits{Budget: pego.Unlimited}))
```

A parse with `Unlimited` and no context runs without any control, which is the fastest form. A stream does not know
its length in advance: its default is `max(2^20, 1024 * d)`, where `d` is the furthest position the parser has asked the stream for, so it does not
depend on how your reader splits the input. A `Document` resolves the
default from the text at each `Parse`. Generated Go and TypeScript parsers apply the default when they are generated
with `-budget-controls`; a parser generated without it spends no budget and accepts only a zero limit and
`Unlimited`.

## Bounding the Input Length

`Limits.MaxInput` bounds the length of the input in the position unit. Its default is `pego.Unlimited`: the default
budget already grows with the input, so a long input costs in proportion to its length. Set it when you want to
refuse input above a size before spending any work on it:

```go
node, err := p.Parse(input, pego.WithLimits(pego.Limits{MaxInput: 1 << 20}))
var le *pego.LimitError
if errors.As(err, &le) && le.Kind == pego.LimitInput {
    // le.Limit is MaxInput and le.Requested is the length of the input.
}
```

An input of exactly `MaxInput` units is accepted; a longer one fails at once with `LimitInput`, before the parser
runs and even before a canceled context is noticed. A `Document` checks the text it holds at each `Parse`, so an edit
that makes the text too long fails the next `Parse` and a later edit that shortens it makes the `Parse` succeed. A
stream fails when the parser asks for a unit beyond the limit and the stream has one, however your reader chunks the
input, and the error reports the number of units the parser asked for. A finite `MaxInput` needs no budget: with
`Budget: pego.Unlimited` and no context the parse still runs without a control.

## Limits on the Command Line

The `pego` commands that run a parse (`parse`, `test`, `trace`, `profile`, `explain` and `edit-fuzz`) take `-budget` and
`-max-input`, which set `Limits.Budget` and `Limits.MaxInput`. Each takes a number, `0` for the default, or `unlimited`:

```bash
pego parse -g grammar.pego -budget unlimited -max-input 1048576 -i "$input"
```

A parse that reaches a limit prints the `*pego.LimitError` message and exits with status 1. `pego test` reports it as
the runtime error of the case. A saved `edit-fuzz` sequence does not record the limits, so `edit-fuzz -replay` takes
`-budget` and `-max-input` again. A `Document` reuses earlier work and spends less than a fresh parse, so `edit-fuzz`
compares nothing at a check where either parse exhausts its budget.

## Related Topics

- [Running Parsers](running-parsers.md): results, errors and concurrency.
- [Streaming](streaming.md): cancelling a stream whose input is caller-owned I/O.
- [Cancellation and Budget Limits](../../spec/runtime/parsing.md#cancellation-and-budget-limits): the exact rules.
- [Time and Memory](../../spec/runtime/parsing.md#time-and-memory): which grammars have a cost that grows faster than the
  input, and what each budget limit bounds.
