# Stop a Stream When the Caller Gives Up

**Problem.** A stream is read from a connection or a pipe, and the producer goes quiet. You want the parse to end when a
deadline passes or the caller cancels, instead of waiting in `Read` for ever.

`pego.WithContext(ctx)` makes the engine check the context before and after each read and each callback, but the parser
still blocks inside `Read`, and a context cannot make an arbitrary reader return. Combine the context with the reader:
when the context ends, make the reader fail from outside, and the parse returns that error. The parser never closes your
reader or starts a goroutine of its own.

```go
// main.go
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/ornew/pego"
)

const grammar = `rule main =  line* #stream $$
rule line =  [a-z0-9]+ #atomic "\n" `

// parseContext streams r into emit and gives up when ctx is cancelled. The parser blocks inside r.Read, so
// cancelling needs the reader to fail: cancelRead must make a blocked Read return the error (closing the
// connection, or the write end of a pipe, does).
func parseContext(ctx context.Context, p *pego.Parser, r io.Reader, cancelRead func(error), emit func(*pego.Node) error) error {
	stop := context.AfterFunc(ctx, func() { cancelRead(ctx.Err()) })
	defer stop()
	return p.ParseStream(r, emit, pego.WithContext(ctx))
}

func main() {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		log.Fatal(err)
	}
	pr, pw := io.Pipe()
	go func() {
		io.WriteString(pw, "one\ntwo\n")
		// ... and then the producer goes quiet.
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err = parseContext(ctx, p, pr, func(err error) { pw.CloseWithError(err) }, func(n *pego.Node) error {
		fmt.Println("line", n.Children[0].Text)
		return nil
	})
	fmt.Println("err:", err, errors.Is(err, context.DeadlineExceeded))
}
```

## Expected Output

```text
line one
line two
err: context deadline exceeded true
```

## How It Works

- `context.AfterFunc` runs `cancelRead` when the context ends. For a pipe, `CloseWithError` makes the blocked `Read` return
  the error; for a connection, close it or set a read deadline.
- `WithContext(ctx)` covers the work between reads: the engine stops at the next check with the context's error.
- The records delivered before the deadline are caller-owned and remain valid.

## Variations and Limits

- To bound the work instead of the time, use `pego.WithWorkLimits`; exhaustion matches `pego.ErrWorkLimit`. See
  [Cancellation and Work Limits](../guide/cancellation-and-limits.md) and [Streaming](../guide/streaming.md#cancelling-a-stream).
- Restarting requires replaying the input yourself: the reader offset is not a continuation token.
