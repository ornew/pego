package runtime

import (
	"context"
	"errors"
	"testing"
)

// pollCounter is a context that can be canceled and counts its observations.
type pollCounter struct {
	context.Context
	done     chan struct{}
	polls    int
	cancelAt int
}

func (c *pollCounter) Done() <-chan struct{} { return c.done }
func (c *pollCounter) Err() error {
	c.polls++
	if c.cancelAt > 0 && c.polls >= c.cancelAt {
		return context.Canceled
	}
	return nil
}

func TestBudgetControlExistsForContextWithoutLimits(t *testing.T) {
	if c := newBudgetControl(context.Background(), Limits{}); c != nil {
		t.Fatal("a context that cannot be canceled and no limit made a control")
	}
	if c := newBudgetControl(nil, Limits{}); c != nil {
		t.Fatal("no context and no limit made a control")
	}
	ctx := &pollCounter{Context: context.Background(), done: make(chan struct{}), cancelAt: 3}
	c := newBudgetControl(ctx, Limits{})
	if c == nil {
		t.Fatal("a cancelable context without limits made no control")
	}
	// Cancellation is observed by the charges of the parse alone: no limit is ever reached.
	var err error
	func() {
		defer func() {
			if x := recover(); x != nil {
				f, ok := x.(fatal)
				if !ok {
					panic(x)
				}
				err = f.err
			}
		}()
		for range 100 * budgetCheckInterval {
			c.chargeStep()
		}
	}()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not observed without limits: %v after %d polls", err, ctx.polls)
	}
	if used := c.budgetUsed(); used > 3*budgetCheckInterval {
		t.Fatalf("cancellation observed late: %d units used", used)
	}
}

func TestBudgetControlWithoutContextDoesNotPoll(t *testing.T) {
	c := newBudgetControl(nil, Limits{Budget: 1 << 20})
	if c == nil {
		t.Fatal("a limit made no control")
	}
	for range 1 << 19 {
		c.chargeStep()
	}
	if c.sinceCheck != 0 || c.ctx != nil {
		t.Fatalf("a control without context polls: %+v", c)
	}
	var err error
	func() {
		defer func() { err = recover().(fatal).err }()
		for range 1 << 20 {
			c.chargeStep()
		}
	}()
	var le *LimitError
	if !errors.As(err, &le) || le.Used != 1<<20 {
		t.Fatalf("limit: %v", err)
	}
}

func TestBudgetTakeAndClassChargeUnitByUnit(t *testing.T) {
	// Take charges n units; where the grant ends inside n it charges the way n steps do.
	for _, limit := range []uint64{1, 5, 9, 10, 11} {
		a := newBudgetControl(nil, Limits{Budget: limit})
		b := newBudgetControl(nil, Limits{Budget: limit})
		var ea, eb error
		run := func(f func()) (err error) {
			defer func() {
				if x := recover(); x != nil {
					err = x.(fatal).err
				}
			}()
			f()
			return nil
		}
		ea = run(func() { a.Take(10) })
		eb = run(func() {
			for range 10 {
				b.chargeStep()
			}
		})
		var la, lb *LimitError
		if errors.As(ea, &la) != errors.As(eb, &lb) || (la != nil && *la != *lb) {
			t.Fatalf("limit %d: Take %v, steps %v", limit, ea, eb)
		}
	}
}

func TestBudgetClassCharge(t *testing.T) {
	ranges := []BudgetRange{{Lo: 'a', Hi: 'c'}, {Lo: 'x', Hi: 'z'}, {Lo: 0x100, Hi: 0x1ff}}
	for _, negated := range []bool{false, true} {
		cls := NewBudgetClass(ranges, negated)
		for _, ch := range []rune{'a', 'c', 'x', 'm', 0, 127, 0x100, 0x200, 0x10ffff} {
			wantCost, wantReject := uint64(len(ranges)), !negated
			for i, r := range ranges {
				if ch >= r.Lo && ch <= r.Hi {
					wantCost, wantReject = uint64(i+1), negated
					break
				}
			}
			p := &Parser{Control: newBudgetControl(nil, Limits{Budget: 100})}
			if got := p.RejectBudgetClass(ch, cls); got != wantReject {
				t.Fatalf("negated %v %U: reject %v, want %v", negated, ch, got, wantReject)
			}
			if used := p.Control.budgetUsed(); used != wantCost {
				t.Fatalf("negated %v %U: charged %d, want %d", negated, ch, used, wantCost)
			}
		}
	}
}

// referenceSourceCharge charges the two passes of setControlledSource as the model defines them:
// each pass charges one unit per byte of the source, in chunks of 256 bytes taken when a
// character starts at or after the bytes charged so far.
func referenceSourceCharge(c *BudgetControl, s string) (err error) {
	defer func() {
		if x := recover(); x != nil {
			err = x.(fatal).err
		}
	}()
	for range 2 {
		for off, charged := 0, 0; off < len(s); {
			if off >= charged {
				k := min(256, len(s)-charged)
				c.Charge(uint64(k))
				charged += k
			}
			_, size := decodeRuneForTest(s[off:])
			off += size
		}
	}
	return nil
}

func decodeRuneForTest(s string) (rune, int) {
	for i, r := range s {
		if i == 0 {
			n := 1
			for n < len(s) && s[n]&0xc0 == 0x80 {
				n++
			}
			return r, n
		}
	}
	return 0, 1
}

func TestSetControlledSourceChargesOneUnitPerByte(t *testing.T) {
	long := ""
	for range 1300 {
		long += "abcdefghij"
	}
	for _, s := range []string{"", "abc", "abcdefgh", long, long + "é" + long, "é" + long + "日本" + long[:40], long[:20] + "\xff" + long[:20], long + "日本語é" + long} {
		for _, limit := range []uint64{0, 1, 7, 100, 255, 256, 257, 1000, 4096, 4097, 8192, 13000, 26000, 26001, 100000} {
			for _, ctx := range []bool{false, true} {
				var cx context.Context
				if ctx {
					cx = &pollCounter{Context: context.Background(), done: make(chan struct{})}
				}
				wantCtx, gotCtx := cx, cx
				if ctx {
					wantCtx = &pollCounter{Context: context.Background(), done: make(chan struct{})}
					gotCtx = &pollCounter{Context: context.Background(), done: make(chan struct{})}
				}
				want := newBudgetControl(wantCtx, Limits{Budget: limit})
				if want == nil {
					want = &BudgetControl{}
					want.refill()
				}
				wantErr := referenceSourceCharge(want, s)
				got := newBudgetControl(gotCtx, Limits{Budget: limit})
				if got == nil {
					got = &BudgetControl{}
					got.refill()
				}
				p := &Parser{Control: got}
				var gotErr error
				func() {
					defer func() {
						if x := recover(); x != nil {
							gotErr = x.(fatal).err
						}
					}()
					p.setControlledSource(s)
				}()
				var lw, lg *LimitError
				if errors.As(wantErr, &lw) != errors.As(gotErr, &lg) || (lw != nil && *lw != *lg) {
					t.Fatalf("len %d limit %d ctx %v: error %v, reference %v", len(s), limit, ctx, gotErr, wantErr)
				}
				if got.budgetUsed() != want.budgetUsed() {
					t.Fatalf("len %d limit %d ctx %v: used %d, reference %d", len(s), limit, ctx, got.budgetUsed(), want.budgetUsed())
				}
				if ctx && gotCtx.(*pollCounter).polls != wantCtx.(*pollCounter).polls {
					t.Fatalf("len %d limit %d: %d polls, reference %d", len(s), limit, gotCtx.(*pollCounter).polls, wantCtx.(*pollCounter).polls)
				}
				if ctx && len(s) > 8192 && limit == 0 && wantCtx.(*pollCounter).polls < 2 {
					t.Fatalf("len %d: the reference crosses no poll boundary", len(s))
				}
			}
		}
	}
}

// pollsOf runs f against a control with a counting context and returns the context polls and the
// units used, with the error of a fatal interruption.
func pollsOf(limit uint64, f func(*BudgetControl)) (polls int, used uint64, err error) {
	ctx := &pollCounter{Context: context.Background(), done: make(chan struct{})}
	c := newBudgetControl(ctx, Limits{Budget: limit})
	defer func() {
		if x := recover(); x != nil {
			err = x.(fatal).err
		}
		polls, used = ctx.polls, c.budgetUsed()
	}()
	f(c)
	return
}

func TestTakeAndClassPollLikeOneByOne(t *testing.T) {
	// Charges that start at every offset in a poll interval and cross one or more boundaries.
	for _, prefix := range []uint64{0, 1, 100, 250, 255, 256, 257, 300} {
		for _, n := range []uint64{1, 2, 7, 255, 256, 257, 600} {
			for _, limit := range []uint64{0, prefix + n, prefix + n - 1, prefix + n + 300} {
				steps := func(c *BudgetControl, k uint64) {
					for ; k > 0; k-- {
						c.chargeStep()
					}
				}
				wp, wu, we := pollsOf(limit, func(c *BudgetControl) { steps(c, prefix); steps(c, n) })
				gp, gu, ge := pollsOf(limit, func(c *BudgetControl) { steps(c, prefix); c.Take(n) })
				if gp != wp || gu != wu || (we == nil) != (ge == nil) {
					t.Fatalf("prefix %d n %d limit %d: Take polls %d used %d err %v, reference %d %d %v", prefix, n, limit, gp, gu, ge, wp, wu, we)
				}
				if prefix+n >= 2*budgetCheckInterval && limit == 0 && wp < 2 {
					t.Fatalf("prefix %d n %d: no boundary crossed", prefix, n)
				}
			}
		}
	}
	// A class of 300 ranges charges 300 units for a character in the last one and for none.
	var ranges []BudgetRange
	for i := range 300 {
		ranges = append(ranges, BudgetRange{Lo: rune(0x200 + 2*i), Hi: rune(0x200 + 2*i)})
	}
	ranges[299] = BudgetRange{Lo: 'q', Hi: 'q'}
	for _, negated := range []bool{false, true} {
		cls := NewBudgetClass(ranges, negated)
		for _, ch := range []rune{'q', 'r', 0x200 + 2*10, 0x10ffff} {
			for _, prefix := range []uint64{0, 100, 250, 300} {
				for _, limit := range []uint64{0, prefix + 300, prefix + 299} {
					var gotReject bool
					wp, wu, we := pollsOf(limit, func(c *BudgetControl) {
						for range prefix {
							c.chargeStep()
						}
						gotReject = !negated
						for _, r := range ranges {
							c.chargeStep()
							if ch >= r.Lo && ch <= r.Hi {
								gotReject = negated
								break
							}
						}
					})
					wantReject := gotReject
					gp, gu, ge := pollsOf(limit, func(c *BudgetControl) {
						for range prefix {
							c.chargeStep()
						}
						p := &Parser{Control: c}
						gotReject = p.RejectBudgetClass(ch, cls)
					})
					if (ge == nil && gotReject != wantReject) || gp != wp || gu != wu || (we == nil) != (ge == nil) {
						t.Fatalf("negated %v %U prefix %d limit %d: polls %d used %d err %v, reference %d %d %v", negated, ch, prefix, limit, gp, gu, ge, wp, wu, we)
					}
				}
			}
		}
	}
}
