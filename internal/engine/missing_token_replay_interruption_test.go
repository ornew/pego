//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Cancel only after a real grammar has produced a successful LR seed and a
// subsequent call has installed some, but not all, of its missing events. This
// avoids cancelling during setup, initial synthesis, or an enclosing trace call.
type missingReplayCancelContext struct {
	context.Context
	cancel                              context.CancelFunc
	p                                   *parser
	literalCount                        int
	interrupted                         bool
	installedAtCancel, attemptsAtCancel int
}

func (c *missingReplayCancelContext) Err() error {
	if p := c.p; p != nil && p.missing != nil && p.activeSeed != nil {
		s := p.missing
		events := s.seeds[p.activeSeed]
		if len(events) == c.literalCount && s.attempts > c.literalCount && len(s.path) > 0 && len(s.path) < len(events) {
			c.interrupted = true
			c.installedAtCancel, c.attemptsAtCancel = len(s.path), s.attempts
			c.cancel()
		}
	}
	return c.Context.Err()
}

func TestMissingTokensCancellationDuringLRSeedReplay(t *testing.T) {
	// More events than the cancellation polling interval ensure a checkpoint
	// occurs inside replay, without relying on an exact absolute step count.
	literalCount := int(2 * workCheckStepInterval)
	source := `rule main = e $$
rule e = e "x" / (` + strings.Repeat(`";" #missing `, literalCount) + `)`
	prog := mustCompile(t, source)
	for _, recognize := range []bool{false, true} {
		base, cancel := context.WithCancel(context.Background())
		ctx := &missingReplayCancelContext{Context: base, cancel: cancel, literalCount: literalCount}
		n, err := prog.ParseWith("main", "", ParseOptions{
			MissingTokens: 8 * literalCount,
			Recognize:     recognize,
			Context:       ctx,
			Trace:         func(e TraceEvent) { ctx.p = traceParser(e) },
		})
		cancel()
		if !ctx.interrupted || ctx.installedAtCancel <= 0 || ctx.installedAtCancel >= literalCount || ctx.attemptsAtCancel <= literalCount {
			t.Fatalf("did not reach partial LR replay: interrupted=%v, installed=%d, attempts=%d", ctx.interrupted, ctx.installedAtCancel, ctx.attemptsAtCancel)
		}
		if n != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("interrupted replay returned tree=%v, error=%v", n, err)
		}
		if ctx.p == nil || !ctx.p.aborted || ctx.p.missing != nil || ctx.p.activeSeed != nil {
			t.Fatal("aborted traced invocation retained provisional insertion/seed state")
		}
		// A retry must start with a new ledger and budget rather than import
		// the interrupted candidate, even when pooled scratch is reused.
		retry, recovered := requireInsertions(t, prog, "", ParseOptions{MissingTokens: 8 * literalCount, Recognize: recognize, Context: context.Background()}, literalCount)
		if len(recovered) != literalCount || !recognize && len(missingNodes(t, retry)) != literalCount {
			t.Fatal("retry lost or duplicated insertion events after interrupted replay")
		}
		if n, err := prog.ParseWith("main", "", ParseOptions{Recognize: recognize, Context: context.Background()}); n != nil || err == nil {
			t.Fatal("interrupted invocation changed strict retry", n, err)
		}
	}
}
