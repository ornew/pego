# Cancellation and Work Limits

A parse of untrusted or very large input should be able to stop: when a caller gives up, when a deadline passes, or when
the work exceeds a budget. This guide shows the per-call options that do that and what a stopped parse returns. The
nesting limit, which bounds rule calls, is described with the [backends](backends.md#deep-nesting-and-withmaxdepth).

The examples use a parser `p` compiled as in [Running Parsers](running-parsers.md). The repository's security policy,
`SECURITY.md` at the root of the repository, lists what a parse bounds and what the caller must bound.

## Cancelling a Parse

`Parse` accepts per-invocation `WithContext` and `WithLimits` options. A zero
limit is unlimited; the counter resets for the next call. Generated Go parsers offer them only when generated with
`-work-controls`; see [the generated Go API](generated-go-api.md#cancellation-and-work-limits). Generated TypeScript parsers offer
the `signal` and `maxWork` options only with `-work-controls` too; see [TypeScript](typescript.md#cancellation-and-work-limits).

```go
ctx, cancel := context.WithTimeout(context.Background(), time.Second)
defer cancel()
node, err := p.Parse(input,
    pego.WithContext(ctx),
    pego.WithLimits(pego.Limits{MaxWork: 5_000_000}),
)
switch {
case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
    // The invocation was cancelled.
case errors.Is(err, pego.ErrLimit):
    // A work limit was reached; use errors.As for *pego.LimitError details.
case err != nil:
    // Syntax or another parse error.
case node != nil:
    // The parse completed.
}
```

`MaxWork` counts inspected input elements and backend execution work in one counter, one unit each; it is neither a
time nor a memory limit, and backtracking never refunds it. Scanning input, such as a repeated character class,
charges it in proportion to the elements examined, so one limit bounds the work on large input, rule calls and
values that grow. An interruption is fatal to the parse and `#recover` cannot consume it,
so a batch parse returns no partial result. Cancellation is cooperative: a parser cannot preempt a blocked reader or
a callback of yours. The exact rules are in
[Cancellation and Work Limits](../../spec/runtime/parsing.md#cancellation-and-work-limits); the
[Streaming](streaming.md#cancelling-a-stream) guide shows how to cancel caller-owned I/O.

## Related Topics

- [Running Parsers](running-parsers.md): results, errors and concurrency.
- [Streaming](streaming.md): cancelling a stream whose input is caller-owned I/O.
- [Cancellation and Work Limits](../../spec/runtime/parsing.md#cancellation-and-work-limits): the exact rules.
- [Time and Memory](../../spec/runtime/parsing.md#time-and-memory): which grammars do work that grows faster than the
  input, and what each work limit bounds.
