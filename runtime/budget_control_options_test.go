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

// referenceSourceCharge charges the two passes of setControlledSource one character at a time.
func referenceSourceCharge(c *BudgetControl, s string) (err error) {
	defer func() {
		if x := recover(); x != nil {
			err = x.(fatal).err
		}
	}()
	for range 2 {
		for off := 0; off < len(s); {
			c.Charge(uint64(min(4, len(s)-off)))
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

func TestSetControlledSourceChargesLikeOneByOne(t *testing.T) {
	long := ""
	for range 300 {
		long += "abcdefghij"
	}
	for _, s := range []string{"", "abc", "abcdefgh", long, long + "é" + long, "é" + long + "日本" + long[:40], long[:20] + "\xff" + long[:20]} {
		for _, limit := range []uint64{0, 1, 7, 100, 1000, 2400, 2401, 2402, 6000, 100000} {
			for _, ctx := range []bool{false, true} {
				var cx context.Context
				if ctx {
					cx = &pollCounter{Context: context.Background(), done: make(chan struct{})}
				}
				want := newBudgetControl(cx, Limits{Budget: limit})
				if want == nil {
					want = &BudgetControl{}
					want.refill()
				}
				wantErr := referenceSourceCharge(want, s)
				got := newBudgetControl(cx, Limits{Budget: limit})
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
			}
		}
	}
}
