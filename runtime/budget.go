package runtime

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Limits bounds one parse invocation. A zero limit leaves that limit
// unlimited.
//
// Budget bounds the cumulative spend of the parse: every input unit
// inspected, every rule call and loop iteration, and every unit of value
// construction adds to one counter, so a bound on it bounds scanning,
// recursion and value building alike.
type Limits struct {
	Budget uint64
}

// ErrLimit is matched by errors.Is for any LimitError.
var ErrLimit = errors.New("parse limit exceeded")

// LimitKind identifies the limit that was exhausted.
type LimitKind uint8

const (
	LimitBudget LimitKind = iota
)

// LimitError reports a reservation that would exceed a limit. Used is the
// counter value before the rejected reservation; Requested is the complete
// atomic reservation that could not be made.
type LimitError struct {
	Kind      LimitKind
	Limit     uint64
	Used      uint64
	Requested uint64
}

func (e *LimitError) Error() string {
	if e == nil {
		return ErrLimit.Error()
	}
	return fmt.Sprintf("parse budget limit exceeded: limit %d, used %d, requested %d", e.Limit, e.Used, e.Requested)
}

// Is reports whether target is the limit sentinel.
func (e *LimitError) Is(target error) bool { return e != nil && target == ErrLimit }

// budgetCheckInterval is the spend between two observations of cancellation.
const budgetCheckInterval = uint64(256)

// BudgetControl is private state for a single parser invocation. Callers must
// split long operations into bounded reservations so cancellation can be
// observed between chunks.
//
// BudgetControl is for generated parsers; not a stable API.
type BudgetControl struct {
	ctx    context.Context
	limits Limits

	// The units reserved are base, which the slow path of charge keeps, plus
	// what the fast path reserved since (used). room is the units the fast path
	// can still reserve without reaching a poll boundary or exceeding a limit,
	// out of the grant that refill computed, so that the common charge is one
	// subtraction that inlines into the matchers.
	base        uint64
	grant, room uint64

	sinceCheck  uint64
	interrupted bool // distinguishes budget interruptions from ordinary parse errors
}

// newBudgetControl returns nil when neither cancellation nor a finite budget is
// active. Without a cancelable context the control never polls, so a parse with
// only a budget reaches the slow path of a charge only at its limit. A nil
// context is equivalent to context.Background.
func newBudgetControl(ctx context.Context, limits Limits) *BudgetControl {
	if ctx != nil && ctx.Done() == nil {
		ctx = nil // it can never be canceled
	}
	if ctx == nil && limits.Budget == 0 {
		return nil
	}
	c := &BudgetControl{ctx: ctx, limits: limits}
	c.refill()
	return c
}

// budgetUsed returns the budget units reserved so far.
func (c *BudgetControl) budgetUsed() uint64 { return c.base + (c.grant - c.room) }

// settle moves what the fast path of charge reserved into the base and the
// poll counter, leaving no room.
func (c *BudgetControl) settle() {
	n := c.grant - c.room
	c.base += n
	c.sinceCheck += n
	c.grant, c.room = 0, 0
}

// refill grants the fast path of charge the room left by the settled counters.
func (c *BudgetControl) refill() {
	interval := budgetCheckInterval
	if c.ctx == nil {
		interval, c.sinceCheck = unpolledGrant, 0
	}
	c.grant = budgetRoom(c.base, c.sinceCheck, c.limits.Budget, interval)
	c.room = c.grant
}

// unpolledGrant is the room granted at a time when nothing observes the context.
// It is far below the overflow of the counters.
const unpolledGrant = uint64(1) << 62

// budgetRoom returns the smallest request that reaches the poll interval
// (reachesInterval) or exceeds the limit (exceedsLimit).
func budgetRoom(used, since, limit, interval uint64) uint64 {
	var room uint64
	if since < interval {
		room = interval - since
	}
	if limit != 0 {
		if used > limit {
			return 0
		}
		if l := limit - used + 1; l != 0 && l < room {
			room = l
		}
	}
	return room
}

// contextPanic attributes a panic to caller-owned context code rather than VM
// execution. Parse boundaries re-raise its original value after cleanup.
type contextPanic struct{ value any }

func contextErr(ctx context.Context) error {
	defer func() {
		if x := recover(); x != nil {
			switch x.(type) {
			case fatal, contextPanic:
				panic(x) // preserve engine interruption and an already attributed panic
			default:
				panic(contextPanic{value: x})
			}
		}
	}()
	return ctx.Err()
}

// Check observes cancellation at an explicit engine checkpoint. It is safe on
// a nil receiver.
func (c *BudgetControl) Check() {
	if c == nil || c.ctx == nil {
		return
	}
	if err := contextErr(c.ctx); err != nil {
		c.interrupted = true
		panic(fatal{err: err})
	}
	c.settle()
	c.sinceCheck = 0
	c.refill()
}

// Charge atomically reserves n budget units. Cancellation wins when it is
// observed at a poll or when the reservation would fail. A failed reservation
// leaves the counter unchanged. It is safe on a nil receiver.
func (c *BudgetControl) Charge(n uint64) {
	if c != nil && n < c.room {
		c.room -= n
		return
	}
	c.chargeSlow(n)
}

// Take is Charge for a condition: it reserves n units and returns true. It
// does nothing on a nil receiver.
func (c *BudgetControl) Take(n uint64) bool {
	if c == nil {
		return true
	}
	if n < c.room {
		c.room -= n
		return true
	}
	c.takeSlow(n)
	return true
}

// takeSlow charges n units one at a time, the reference charge, so that exhaustion and polls
// fall where they do for the matcher that charges its input unit by unit.
func (c *BudgetControl) takeSlow(n uint64) {
	for ; n > 0; n-- {
		c.chargeStep()
	}
}

// chargeStep reserves one unit for a rule call, a loop iteration or a matcher
// step; it does nothing on a nil receiver. n < room is room > 1 for n = 1.
func (c *BudgetControl) chargeStep() {
	if c == nil {
		return
	}
	if c.room > 1 {
		c.room--
	} else {
		c.chargeSlow(1)
	}
}

// chargeSlow is charge at a poll boundary or a limit, or on a nil receiver.
func (c *BudgetControl) chargeSlow(n uint64) {
	if c == nil {
		return
	}
	c.settle()

	exceeded := exceedsLimit(c.base, n, c.limits.Budget)
	poll := c.ctx != nil && reachesInterval(c.sinceCheck, n, budgetCheckInterval)

	// Check on each poll boundary and on a failed reservation so an already
	// canceled invocation wins over a simultaneously exhausted budget.
	if poll || exceeded {
		if c.ctx != nil {
			if err := contextErr(c.ctx); err != nil {
				c.interrupted = true
				panic(fatal{err: err})
			}
		}
	}
	if exceeded {
		c.interrupted = true
		panic(fatal{err: &LimitError{
			Kind: LimitBudget, Limit: c.limits.Budget,
			Used: c.base, Requested: n,
		}})
	}

	c.base = saturatingAdd(c.base, n)
	if poll {
		c.sinceCheck = 0
	} else {
		c.sinceCheck = saturatingAdd(c.sinceCheck, n)
	}
	c.refill()
}

// exceedsLimit checks a reservation without computing a potentially
// overflowing used+requested sum. A zero limit is unlimited.
func exceedsLimit(used, requested, limit uint64) bool {
	return limit != 0 && (used > limit || requested > limit-used)
}

// reachesInterval checks whether adding requested would meet or cross interval
// without overflowing. Progress counters are kept below their interval.
func reachesInterval(used, requested, interval uint64) bool {
	return used >= interval || requested >= interval-used
}

func saturatingAdd(a, b uint64) uint64 {
	if math.MaxUint64-a < b {
		return math.MaxUint64
	}
	return a + b
}

// Spend helpers keep long engine-owned scans between bounded reservations.
//
// SpendRead has one guarded call so that it inlines into the matchers.
func (p *Parser) SpendRead(n uint64) {
	if p.Document != nil {
		p.spendReadTracked(n)
	} else if p.Control != nil {
		p.Control.Charge(n)
	}
}

// spendReadOrdinary is SpendRead in an ordinary parse (ExpectOrdinary), which tracks no input.
func (p *Parser) spendReadOrdinary(uint64) {}

// spendReadTracked is SpendRead in a Document or controlled parse.
func (p *Parser) spendReadTracked(n uint64) {
	if p.Document != nil {
		// Generated matchers can read directly after charging the spend of the read.
		// Overestimating the dependency only discards a reusable memo entry.
		if n > uint64(int(^uint(0)>>1)-p.Pos) {
			p.documentTouch(int(^uint(0) >> 1))
		} else {
			p.documentTouch(p.Pos + int(n))
		}
	}
	if p.Control != nil {
		p.Control.Charge(n)
	}
}
func (p *Parser) Spend() {
	p.Control.chargeStep()
}

// sourceChunk is the bytes of a source that one charge of its preparation covers: one poll
// interval, so that cancellation is observed as often as when each byte is charged alone.
const sourceChunk = int(budgetCheckInterval)

func (p *Parser) setControlledSource(s string) {
	n, valid := 0, len(s) <= 1<<31-1
	// Outside a Document, charge the control directly: SpendRead is too large to inline.
	c := p.Control
	if p.Document != nil {
		c = nil
	}
	// Each of the two passes charges one unit per byte of the source: outside a Document in
	// chunks, before the bytes of a chunk are decoded; in a Document by the size
	// of each character, which it records as examined.
	for off, charged := 0, 0; off < len(s); {
		if c != nil && off >= charged {
			k := min(sourceChunk, len(s)-charged)
			c.Charge(uint64(k))
			charged += k
		}
		if c != nil {
			// The bytes of the charged chunk that are ASCII are one character each.
			for off < charged && s[off] < utf8.RuneSelf {
				n++
				off++
			}
			if off >= charged {
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[off:])
		if c == nil {
			p.SpendRead(uint64(size))
		}
		if r == utf8.RuneError && size == 1 {
			valid = false
		}
		n++
		off += size
	}
	if cap(p.In) >= n {
		p.In = p.In[:n]
	} else {
		p.In = make([]rune, n)
	}
	p.n = n
	var offs []int32
	if valid {
		if cap(p.offs) >= n+1 {
			offs = p.offs[:n+1]
		} else {
			offs = make([]int32, n+1)
		}
		offs[n] = int32(len(s))
	}
	for off, i, charged := 0, 0, 0; off < len(s); i++ {
		if c != nil && off >= charged {
			k := min(sourceChunk, len(s)-charged)
			c.Charge(uint64(k))
			charged += k
		}
		if c != nil {
			for off < charged && s[off] < utf8.RuneSelf {
				p.In[i] = rune(s[off])
				if valid {
					offs[i] = int32(off)
				}
				off++
				i++
			}
			if off >= charged {
				i-- // the loop header advances i
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[off:])
		if c == nil {
			p.SpendRead(uint64(size))
		}
		p.In[i] = r
		if valid {
			offs[i] = int32(off)
		}
		off += size
	}
	p.src, p.offs = "", nil
	if valid {
		p.src, p.offs = s, offs
	}
}

func (p *Parser) literalHasReplacement(rs []rune) bool {
	if p.Control != nil {
		return p.literalHasReplacementControlled(rs)
	}
	return slices.Contains(rs, utf8.RuneError)
}

func (p *Parser) literalHasReplacementControlled(rs []rune) bool {
	for _, r := range rs {
		p.Spend()
		if r == utf8.RuneError {
			return true
		}
	}
	return false
}

func (p *Parser) hasLiteralPrefix(text string) bool {
	src := p.bs[p.Pos:]
	if p.Control == nil {
		return strings.HasPrefix(src, text)
	}
	if len(src) < len(text) {
		p.SpendRead(1)
		return false
	}
	for off := 0; off < len(text); {
		n := min(4096, len(text)-off)
		p.SpendRead(uint64(n))
		if src[off:off+n] != text[off:off+n] {
			return false
		}
		off += n
	}
	return true
}

// RunParseConverted shares admission and counters with a Node-to-typed converter.
//
// RunParseConverted is for generated parsers; not a stable API.
func RunParseConverted[T any](r *Rule, input string, o parseOptions, conv func(*Node, *BudgetControl, error) T) (res T, err error) {
	defer recoverParse(&res, &err)
	o = prepareOptions(o)
	n, err := parseConversionInput(r, input, o)
	o.control.Check()
	res = conv(n, o.control, err)
	o.control.Check()
	return res, err
}

// Ordinary runtime/depth errors retain the configured ParseAST converter's
// historical nil mapping. Budget interruptions bypass conversion without calling
// methods on a caller-owned error to classify it.
func parseConversionInput(r *Rule, input string, o parseOptions) (n *Node, err error) {
	defer func() {
		if x := recover(); x != nil {
			if f, ok := x.(fatal); ok && (o.control == nil || !o.control.interrupted) {
				n, err = nil, f.err
			} else {
				panic(x)
			}
		}
	}()
	return parseResolvedSpend(r, input, o)
}

// BudgetRange is for generated parsers; not a stable API.
type BudgetRange struct{ Lo, Hi rune }

// BudgetClass is a character class with more ranges than a matcher tests inline. Testing a
// character charges one unit per range tried, up to the range that holds it.
//
// BudgetClass is for generated parsers; not a stable API.
type BudgetClass struct {
	ranges  []BudgetRange
	negated bool
	// ascii holds, for each character below 128, the units its test charges
	// shifted left by one, with the result of the test in bit 0.
	ascii [128]uint16
}

// NewBudgetClass returns the class of ranges, negated or not.
func NewBudgetClass(ranges []BudgetRange, negated bool) *BudgetClass {
	c := &BudgetClass{ranges: ranges, negated: negated}
	if len(ranges) >= 1<<15 {
		return c
	}
	for ch := rune(0); ch < 128; ch++ {
		cost, reject := len(ranges), !negated
		for i, r := range ranges {
			if ch >= r.Lo && ch <= r.Hi {
				cost, reject = i+1, negated
				break
			}
		}
		c.ascii[ch] = uint16(cost) << 1
		if reject {
			c.ascii[ch] |= 1
		}
	}
	return c
}

// Reject charges the test of ch and reports whether the class rejects it.
func (p *Parser) RejectBudgetClass(ch rune, c *BudgetClass) bool {
	if uint32(ch) < 128 && len(c.ranges) < 1<<15 {
		t := c.ascii[ch]
		p.Control.Take(uint64(t >> 1))
		return t&1 != 0
	}
	for _, r := range c.ranges {
		p.Spend()
		if ch >= r.Lo && ch <= r.Hi {
			return c.negated
		}
	}
	return !c.negated
}

// Value strings may be action-created, so their scans consume steps alone.
const spendValueChunk = 128

func (p *Parser) spendValue(n int) {
	if p.Control != nil {
		p.Control.Charge(uint64(n))
	}
}

func (p *Parser) compareText(l, r string) int {
	if p.Control != nil {
		return p.compareTextControlled(l, r)
	}
	return strings.Compare(l, r)
}

func (p *Parser) compareTextControlled(l, r string) int {
	for off, end := 0, min(len(l), len(r)); off < end; {
		n := min(spendValueChunk, end-off)
		p.spendValue(2 * n)
		if order := strings.Compare(l[off:off+n], r[off:off+n]); order != 0 {
			return order
		}
		off += n
	}
	if len(l) < len(r) {
		return -1
	}
	if len(l) > len(r) {
		return 1
	}
	return 0
}

func (p *Parser) TextEqual(l, r string) bool {
	return len(l) == len(r) && p.compareText(l, r) == 0
}

// stringBinary handles only string pairs; callers retain their existing
// runtime's scalar, pointer-identity and invalid-operand behavior.
func (p *Parser) stringBinary(op string, l, r any) (any, bool) {
	if p.Control != nil {
		return p.stringBinaryControlled(op, l, r)
	}
	return nil, false
}

func (p *Parser) stringBinaryControlled(op string, l, r any) (any, bool) {
	ls, lok := l.(string)
	rs, rok := r.(string)
	if !lok || !rok {
		return nil, false
	}
	switch op {
	case "==":
		return p.TextEqual(ls, rs), true
	case "!=":
		return !p.TextEqual(ls, rs), true
	case "<":
		return p.compareText(ls, rs) < 0, true
	case "<=":
		return p.compareText(ls, rs) <= 0, true
	case ">":
		return p.compareText(ls, rs) > 0, true
	case ">=":
		return p.compareText(ls, rs) >= 0, true
	case "+":
		var out strings.Builder
		out.Grow(checkedTextSize(len(ls), len(rs)))
		for _, s := range [...]string{ls, rs} {
			for off := 0; off < len(s); {
				n := min(spendValueChunk, len(s)-off)
				p.spendValue(n)
				out.WriteString(s[off : off+n])
				off += n
			}
		}
		return out.String(), true
	}
	return nil, false
}

// compareStrings applies the comparison operator op to two strings as stringBinary does.
func (p *Parser) compareStrings(op string, l, r string) (bool, bool) {
	switch op {
	case "==":
		return p.TextEqual(l, r), true
	case "!=":
		return !p.TextEqual(l, r), true
	case "<":
		return p.compareText(l, r) < 0, true
	case "<=":
		return p.compareText(l, r) <= 0, true
	case ">":
		return p.compareText(l, r) > 0, true
	case ">=":
		return p.compareText(l, r) >= 0, true
	}
	return false, false
}

func (c *ActCtx) Binary(op string, l, r any) any {
	if v, ok := c.P.stringBinary(op, l, r); ok {
		return v
	}
	if c.P.Control != nil {
		return rtBinarySpend(c.P, op, l, r)
	}
	return rtBinary(op, l, r)
}

// detachedText constructs normalized text when malformed source bytes prevent
// slicing the original string. Allocation and Grow remain nonpreemptible.
func (p *Parser) detachedText(start, end int) string {
	src := p.In[start:end]
	size := 0
	for off := 0; off < len(src); {
		n := min(spendValueChunk, len(src)-off)
		p.Control.Charge(2 * uint64(n))
		for _, r := range src[off : off+n] {
			width := utf8.RuneLen(r)
			if width < 0 {
				width = utf8.RuneLen(utf8.RuneError)
			}
			size = checkedTextSize(size, width)
		}
		off += n
	}
	var out strings.Builder
	out.Grow(size)
	for off := 0; off < len(src); {
		n := min(spendValueChunk, len(src)-off)
		p.Control.Charge(2 * uint64(n))
		for _, r := range src[off : off+n] {
			out.WriteRune(r)
		}
		off += n
	}
	return out.String()
}

func checkedTextSize(size, width int) int {
	if size < 0 || width < 0 || size > int(^uint(0)>>1)-width {
		panic(fatal{err: fmt.Errorf("input text exceeds maximum string size")})
	}
	return size + width
}

func copySpend[T any](p *Parser, dst, src []T) {
	if p.Control != nil {
		copyBudgetControlled(p, dst, src)
		return
	}
	copy(dst, src)
}

func copyBudgetControlled[T any](p *Parser, dst, src []T) {
	// Engine callers use disjoint storage; overlapping moves need their own
	// direction-aware implementation rather than a chunked forward copy.
	for off, end := 0, min(len(dst), len(src)); off < end; {
		n := min(spendValueChunk, end-off)
		p.spendValue(n)
		copy(dst[off:off+n], src[off:off+n])
		off += n
	}
}

// growthSpend charges the copy that growing a table from oldLen to newLen entries stands for. The
// charge is a function of the two lengths alone: it falls where the length crosses a power of two,
// as doubling would copy, and never depends on the spare capacity that an earlier parse left in a
// pooled parser, so a parse is charged the same wherever its storage came from. The physical copy
// follows the charge and is not charged again.
func (p *Parser) growthSpend(oldLen, newLen int) {
	if oldLen > 0 && bits.Len(uint(newLen)) > bits.Len(uint(oldLen)) {
		p.chargeValues(oldLen)
	}
}

// chargeValues charges n values in chunks, so that a limit or cancellation is seen between them.
func (p *Parser) chargeValues(n int) {
	for n > 0 {
		c := min(spendValueChunk, n)
		p.spendValue(c)
		n -= c
	}
}

func appendSpend[T any](p *Parser, dst, src []T) []T {
	if p.Control != nil {
		return appendBudgetControlled(p, dst, src)
	}
	return append(dst, src...)
}

func appendBudgetControlled[T any](p *Parser, dst, src []T) []T {
	for off := 0; off < len(src); {
		n := min(spendValueChunk, len(src)-off)
		p.spendValue(n)
		dst = append(dst, src[off:off+n]...)
		off += n
	}
	return dst
}

// equalExpected preserves the native snapshot-identity fast path while charging
// each comparison under active budget controls. Snapshots are never modified.
func (p *Parser) equalExpected(a, b []ExpID) bool {
	if p.Control != nil {
		return p.equalExpectedControlled(a, b)
	}
	return slices.Equal(a, b)
}

func (p *Parser) equalExpectedControlled(a, b []ExpID) bool {
	if len(a) != len(b) {
		return false
	}
	for i, x := range a {
		p.Spend()
		if x != b[i] {
			return false
		}
	}
	return true
}

// sortExpected sorts a newly constructed, parser-owned diagnostic. Comparisons
// and explicit moves are bounded; allocation and GC remain nonpreemptible.
// Published or memoized errors must never be passed to this helper.
func (p *Parser) sortExpected(values []string) {
	if p.Control != nil {
		p.sortExpectedControlled(values)
		return
	}
	sort.Strings(values)
}

func (p *Parser) sortExpectedControlled(values []string) {
	if len(values) < 2 {
		return
	}
	src, dst := values, make([]string, len(values))
	for width := 1; width < len(values); {
		for lo := 0; lo < len(values); {
			mid := lo + min(width, len(values)-lo)
			hi := mid + min(width, len(values)-mid)
			i, j, k := lo, mid, lo
			for i < mid && j < hi {
				order := p.compareText(src[i], src[j])
				p.Spend()
				if order <= 0 {
					dst[k] = src[i]
					i++
				} else {
					dst[k] = src[j]
					j++
				}
				k++
			}
			copySpend(p, dst[k:k+mid-i], src[i:mid])
			k += mid - i
			copySpend(p, dst[k:hi], src[j:hi])
			lo = hi
		}
		src, dst = dst, src
		// Stop before doubling can overflow; this pass merged the last run.
		if width >= len(values)-width {
			break
		}
		width *= 2
	}
	if &src[0] != &values[0] {
		copySpend(p, values, src)
	}
}

// RecoveryErrorText is only for engine-owned Error-node materialization.
// Public post-return Error/Message calls remain caller spend. The local builder
// owns its immutable output and is never pooled or written after return.
func (p *Parser) RecoveryErrorText(e *SyntaxError) string {
	if p.Control != nil {
		return p.recoveryErrorTextControlled(e)
	}
	return e.Error()
}

func (p *Parser) recoveryErrorTextControlled(e *SyntaxError) string {
	prefix := fmt.Sprintf("%d:%d: ", e.Line, e.Col) // bounded integer formatting
	intro, sep := "", "; "
	parts := e.Messages
	if len(parts) == 0 {
		parts, sep = e.Expected, ", "
		intro = "syntax error"
		if len(parts) > 0 {
			intro += ": expected "
		}
	}
	size := checkedDiagnosticSize(len(prefix), len(intro))
	for i, part := range parts {
		p.Spend()
		if i > 0 {
			size = checkedDiagnosticSize(size, len(sep))
		}
		size = checkedDiagnosticSize(size, len(part))
	}
	var out strings.Builder
	out.Grow(size)
	p.writeDiagnosticText(&out, prefix)
	p.writeDiagnosticText(&out, intro)
	for i, part := range parts {
		p.Spend()
		if i > 0 {
			p.writeDiagnosticText(&out, sep)
		}
		p.writeDiagnosticText(&out, part)
	}
	return out.String()
}

func (p *Parser) writeDiagnosticText(out *strings.Builder, text string) {
	for off := 0; off < len(text); {
		n := min(spendValueChunk, len(text)-off)
		p.spendValue(n)
		out.WriteString(text[off : off+n])
		off += n
	}
}

func checkedDiagnosticSize(size, width int) int {
	if size < 0 || width < 0 || size > int(^uint(0)>>1)-width {
		panic(fatal{err: fmt.Errorf("diagnostic text exceeds maximum string size")})
	}
	return size + width
}

// makeControlledError owns any partially assembled diagnostic until completion.
// Interrupted invocations release their partial line table with parser scratch.
func (p *Parser) makeControlledError(pos int, expected []ExpID) *SyntaxError {
	e := &SyntaxError{Pos: pos}
	for _, x := range expected {
		p.Spend()
		if x&MsgBit != 0 {
			e.Messages = append(e.Messages, p.g.Descs[x & ^MsgBit])
		} else {
			e.Expected = append(e.Expected, p.g.Descs[x])
		}
	}
	p.sortExpected(e.Expected)
	if p.Stream != nil {
		e.Line, e.Col = p.streamLineCol(pos)
		return e
	}
	if p.lines == nil {
		p.lines = append(p.lines, 0)
		for i := 0; i < p.n; i++ {
			p.SpendRead(1)
			if p.unit == Bytes && p.bs[i] == '\n' || p.unit == CodePoints && p.In[i] == '\n' {
				p.lines = append(p.lines, i+1)
			}
		}
	}
	pos = min(pos, p.n)
	lo, hi := 0, len(p.lines)
	for lo < hi {
		p.Spend()
		mid := lo + (hi-lo)/2
		if p.lines[mid] < pos+1 {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	e.Line, e.Col = lo, pos-p.lines[lo-1]+1
	p.Control.Check()
	return e
}

func (p *Parser) copyErrors(dst, src []*SyntaxError) []*SyntaxError { return appendSpend(p, dst, src) }

// rawValueEqual preserves the interface equality of memo keys and bindings.
// In particular, a typed nil pointer differs from a nil interface. Action
// valueEqual has different nil semantics and must not be used for these keys.
func (p *Parser) rawValueEqual(l, r any) bool {
	if s, ok := l.(string); ok {
		t, ok := r.(string)
		return ok && len(s) == len(t) && p.compareText(s, t) == 0
	}
	return l == r
}

func (p *Parser) memoKeyEqual(l, r []any) bool {
	if len(l) != len(r) {
		return false
	}
	for i, v := range l {
		p.Spend()
		if !p.rawValueEqual(v, r[i]) {
			return false
		}
	}
	return true
}

func (p *Parser) bindControlled(name string, val any) {
	p.Spend()
	e := p.Env
	if p.bound == nil {
		p.bound = make(map[string]bool)
	}
	if e == nil {
		p.bound[name] = true
		p.Env = &Env{name: name, val: val}
		return
	}
	if p.rawValueEqual(e.name, name) {
		if !p.rawValueEqual(e.val, val) {
			p.Env = &Env{name: name, val: val, next: e.next}
		}
		return
	}
	// Map hashing and allocation are runtime operations that cannot be split
	// into engine checkpoints. Explicit name comparisons above/below can be.
	if !p.bound[name] {
		p.bound[name] = true
		p.Env = &Env{name: name, val: val, next: e}
		return
	}
	old := e
	for old != nil {
		p.Spend()
		if p.rawValueEqual(old.name, name) {
			break
		}
		old = old.next
	}
	if old == nil {
		p.Env = &Env{name: name, val: val, next: e}
		return
	}
	if p.rawValueEqual(old.val, val) {
		return
	}
	head := &Env{name: name, val: val}
	tail := head
	for cur := e; cur != old; cur = cur.next {
		p.Spend()
		tail.next = &Env{name: cur.name, val: cur.val}
		tail = tail.next
	}
	tail.next = old.next
	// Saved environments are immutable; only publish a completed prefix.
	p.Env = head
}

func clearSpend[T any](p *Parser, values []T) {
	if p.Control != nil {
		clearBudgetControlled(p, values)
		return
	}
	clear(values)
}

func clearBudgetControlled[T any](p *Parser, values []T) {
	for off := 0; off < len(values); {
		n := min(spendValueChunk, len(values)-off)
		p.spendValue(n)
		clear(values[off : off+n])
		off += n
	}
}

func (p *Parser) controlledEnvValues(names []string) []any {
	p.Spend()
	if len(names) == 0 {
		return nil
	}
	if p.Env == nil && len(names) <= len(noVals) {
		return noVals[:len(names):len(names)]
	}
	vals := make([]any, len(names))
	for i, name := range names {
		p.Spend()
		for e := p.Env; e != nil; e = e.next {
			p.Spend()
			if p.rawValueEqual(e.name, name) {
				vals[i] = e.val
				break
			}
		}
	}
	return vals
}

func (p *Parser) lookupEnvControlled(name string) (any, bool) {
	for e := p.Env; e != nil; e = e.next {
		p.Spend()
		if p.rawValueEqual(e.name, name) {
			return e.val, true
		}
	}
	return nil, false
}

func (p *Parser) memoGet(rule, pos, min int, key []any) *memoEntry {
	if p.Stream != nil {
		// A call at a position before the pruned memo needs no entry (a
		// start rule called at 0, or still growing there).
		if pos -= p.Stream.memoBase; pos < 0 {
			return nil
		}
	}
	if p.Control != nil {
		return p.memoGetControlled(rule, pos, min, key)
	}
	return p.memo.get(rule, pos, min, key)
}

func (p *Parser) memoGetControlled(rule, pos, min int, key []any) *memoEntry {
	p.Spend()
	if pos >= len(p.memo.slots) {
		return nil
	}
	for e := p.memo.slots[pos]; e != nil; e = e.next {
		p.Spend()
		if e.rule == int32(rule) && e.min == int32(min) && (e.growing || p.memoKeyEqual(e.env, key)) {
			return e
		}
	}
	return nil
}

func (p *Parser) memoPut(rule, pos, min int, key []any, e *memoEntry) {
	if p.Stream != nil {
		if pos -= p.Stream.memoBase; pos < 0 {
			return
		}
	}
	if p.Control != nil {
		p.memoPutControlled(rule, pos, min, key, e)
		return
	}
	p.memo.put(rule, pos, min, key, e)
}

func (p *Parser) memoPutControlled(rule, pos, min int, key []any, e *memoEntry) {
	p.Spend()
	slots := p.memo.slots
	if pos >= len(slots) {
		p.growthSpend(len(slots), pos+1)
		if pos < cap(slots) {
			slots = slots[:pos+1]
		} else {
			grown := make([]*memoEntry, pos+1, max(2*cap(slots), pos+1, 64))
			copy(grown, slots)
			slots = grown
		}
	}
	link := &slots[pos]
	var replaced *memoEntry
	for x := *link; x != nil; link, x = &x.next, x.next {
		p.Spend()
		if x == e || x.rule == int32(rule) && x.min == int32(min) && p.memoKeyEqual(x.env, key) {
			replaced = x
			break
		}
	}
	p.Spend()
	// Commit the completed key/slot array and link together, with no further
	// checkpoint between mutations. A resident self-entry retains its successor.
	p.memo.slots = slots
	e.rule, e.min, e.env = int32(rule), int32(min), key
	if replaced != nil {
		e.next = replaced.next
		*link = e
		return
	}
	e.next = slots[pos]
	slots[pos] = e
}

// controlledFirstCallPrepare charges and grows the first-call bookkeeping of a controlled parse; the
// ordinary FirstCall body then runs on the grown state.
func (p *Parser) controlledFirstCallPrepare(r *Rule) {
	if r.Seen < 0 {
		return
	}
	p.Spend()
	t := &p.memo
	if t.calls == nil {
		t.calls = make([]seenCalls, t.stride)
	}
	if !t.calls[r.Seen].eager && t.sparse {
		if len(t.pages) != t.stride {
			// No step of its own: whether the directory table exists is the pooled parser's state.
			t.pages = make([]seenRulePages, t.stride)
		}
		dir := &t.pages[r.Seen]
		at := p.Pos >> 10
		if at >= len(dir.pages) {
			// The directory is empty at the start of a parse, so its length is the parse's own.
			p.growthSpend(len(dir.pages), at+1)
			dir.pages = slices.Grow(dir.pages, at+1-len(dir.pages))[:at+1]
		}
		if dir.pages[at] == nil {
			p.Spend()
			// A page that starts a chunk is a demand of the parse; whether the chunk exists already
			// is the pooled parser's state. Reserve the capacity alloc appends into here.
			if n := t.bitPages.n; n%64 == 0 {
				p.growthSpend(n/64, n/64+1)
				if n/64 == len(t.bitPages.chunks) {
					t.bitPages.chunks = slices.Grow(t.bitPages.chunks, 1)
				}
			}
			dir.pages[at] = t.bitPages.alloc()
		}
		dir.last, dir.at = dir.pages[at], at
	} else if !t.calls[r.Seen].eager {
		w := (p.Pos*t.stride + r.Seen) >> 6
		if w >= len(t.seen) {
			n := len(t.seen) + max(w+1-len(t.seen), len(t.seen), 64)
			// The new words are the spend, whether they come from spare capacity or an allocation.
			p.chargeValues(n - len(t.seen))
			seen := t.seen
			if n <= cap(seen) {
				seen = seen[:n]
				clear(seen[len(t.seen):])
			} else {
				seen = make([]uint64, n)
				copy(seen, t.seen)
			}
			t.seen = seen
		}
	}
}

// Graph helpers account for normal construction and capture restoration.
// Release/recycle, DropKids/DropTrail and arena reset remain unconditional.
func (p *Parser) valueField(fs Fields, name string) (any, bool) {
	if p.Control != nil {
		return p.valueFieldControlled(fs, name)
	}
	return fs.Get(name)
}

func (p *Parser) valueFieldControlled(fs Fields, name string) (any, bool) {
	for _, f := range fs {
		p.Spend()
		if p.rawValueEqual(f.Name, name) {
			return f.Value, true
		}
	}
	return nil, false
}
func (p *Parser) setValueField(fs *Fields, name string, v any) {
	if p.Control != nil {
		p.setValueFieldControlled(fs, name, v)
		return
	}
	fs.set(name, v)
}

func (p *Parser) setValueFieldControlled(fs *Fields, name string, v any) {
	for i := range *fs {
		p.Spend()
		if p.rawValueEqual((*fs)[i].Name, name) {
			(*fs)[i].Value = v
			return
		}
	}
	p.Spend()
	*fs = append(*fs, NodeField{name, v})
}
func (p *Parser) pushKid(v *Node) { p.Spend(); p.KidStack = append(p.KidStack, v) }
func (p *Parser) undoCaptures(from int) {
	for i := len(p.Trail) - 1; i >= from; i-- {
		p.Spend()
		u := p.Trail[i]
		u.f.Vals[u.slot] = u.old
	}
}

func (c *ActCtx) Fold(fn string, right bool, acc, list any, f func(acc, item any) any) any {
	if c.P.Control != nil {
		return c.foldControlled(fn, right, acc, list, f)
	}
	return rtFold(fn, right, acc, list, f)
}

func (c *ActCtx) foldControlled(fn string, right bool, acc, list any, f func(acc, item any) any) any {
	items := c.ListItems(fn, list)
	for i := range items {
		c.P.Spend()
		item := items[i]
		if right {
			item = items[len(items)-1-i]
		}
		acc = f(acc, nodeOrNil(item))
	}
	return acc
}

// actionErrorText accounts for eager engine-owned error text, including errors
// discarded by predicates. Counter reservations precede each fragment copy.
func (p *Parser) actionErrorText(parts ...string) string {
	size := 0
	for _, part := range parts {
		p.Spend()
		size = checkedDiagnosticSize(size, len(part))
	}
	var out strings.Builder
	out.Grow(size)
	for _, part := range parts {
		p.Spend()
		p.writeDiagnosticText(&out, part)
	}
	return out.String()
}

// evalErrorf accepts the engine's fixed %s/%d leaf formats. The inactive path
// retains fmt semantics; active formatting splits known pieces before copying.
func (p *Parser) evalErrorf(format string, args ...any) {
	if p != nil && p.Control != nil {
		p.evalErrorfControlled(format, args...)
		return
	}
	evalErrorf(format, args...)
}

func (p *Parser) evalErrorfControlled(format string, args ...any) {
	parts := make([]string, 0, 2*len(args)+1)
	arg, start := 0, 0
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		parts = append(parts, format[start:i])
		i++
		if i >= len(format) || arg >= len(args) {
			panic("invalid internal action-error format")
		}
		switch format[i] {
		case 's':
			parts = append(parts, args[arg].(string))
		case 'd':
			parts = append(parts, strconv.Itoa(args[arg].(int)))
		default:
			panic("unsupported internal action-error format")
		}
		arg++
		start = i + 1
	}
	if arg != len(args) {
		panic("invalid internal action-error arguments")
	}
	parts = append(parts, format[start:])
	panic(evalError{p.actionErrorText(parts...)})
}
func (p *Parser) wrapEvalError(where, message string) error {
	if p.Control == nil {
		return fmt.Errorf("action in %s: %s", where, message)
	}
	return errors.New(p.actionErrorText("action in ", where, ": ", message))
}

func (c *ActCtx) Unary(op string, v any) any {
	if c.P.Control != nil {
		return c.unaryControlled(op, v)
	}
	return rtUnary(op, v)
}

func (c *ActCtx) unaryControlled(op string, v any) any {
	return rtUnarySpend(c.P, op, v)
}
func (c *ActCtx) And(l any, r func() any) any {
	if c.P.Control != nil {
		return c.andControlled(l, r)
	}
	return rtAnd(l, r)
}

func (c *ActCtx) andControlled(l any, r func() any) any {
	if !rtBoolSpend(c.P, "&&", l) {
		return false
	}
	return rtBoolSpend(c.P, "&&", r())
}
func (c *ActCtx) Or(l any, r func() any) any {
	if c.P.Control != nil {
		return c.orControlled(l, r)
	}
	return rtOr(l, r)
}

func (c *ActCtx) orControlled(l any, r func() any) any {
	if rtBoolSpend(c.P, "||", l) {
		return true
	}
	return rtBoolSpend(c.P, "||", r())
}
func (c *ActCtx) ListItems(fn string, v any) []*Node {
	if c.P.Control != nil {
		return c.listItemsControlled(fn, v)
	}
	return listItems(fn, v)
}

func (c *ActCtx) listItemsControlled(fn string, v any) []*Node {
	return listItemsSpend(c.P, fn, v)
}
func (c *ActCtx) asNode(fn string, v any) *Node {
	switch v.(type) {
	case int, string, bool:
		// An int, string or bool element travels boxed, as the result of an action does.
		return c.P.NewNode(Node{RawKind: kindBox, box: v, RawStart: int32(c.start), RawEnd: int32(c.end)})
	}
	if c.P.Control == nil {
		return asNode(fn, v)
	}
	return asNodeSpend(c.P, fn, v)
}
