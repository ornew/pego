package runtime

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// WorkLimits bounds cumulative engine work for one parse invocation. A zero
// limit leaves that counter unlimited. Scanning input may charge only input
// work, so MaxSteps alone does not bound it; MaxInputWork does.
type WorkLimits struct {
	MaxInputWork uint64
	MaxSteps     uint64
}

// ErrWorkLimit is matched by errors.Is for any WorkLimitError.
var ErrWorkLimit = errors.New("parse work limit exceeded")

// WorkLimitKind identifies the counter that exhausted its limit.
type WorkLimitKind uint8

const (
	InputWorkLimit WorkLimitKind = iota
	StepWorkLimit
)

// WorkLimitError reports a reservation that would exceed one work limit.
// Used is the counter value before the rejected reservation; Requested is the
// complete atomic reservation that could not be made.
type WorkLimitError struct {
	Kind      WorkLimitKind
	Limit     uint64
	Used      uint64
	Requested uint64
}

func (e *WorkLimitError) Error() string {
	if e == nil {
		return ErrWorkLimit.Error()
	}
	name := "input work"
	if e.Kind == StepWorkLimit {
		name = "steps"
	}
	return fmt.Sprintf("parse %s limit exceeded: limit %d, used %d, requested %d", name, e.Limit, e.Used, e.Requested)
}

// Is reports whether target is the work-limit sentinel.
func (e *WorkLimitError) Is(target error) bool { return e != nil && target == ErrWorkLimit }

// WorkControlsEnabled is for generated parsers; not a stable API.
const WorkControlsEnabled = true

const (
	workCheckStepInterval  = uint64(256)
	workCheckInputInterval = uint64(4096)
)

// WorkControl is private state for a single parser invocation. Callers must
// split long operations into bounded reservations so cancellation can be
// observed between chunks.
//
// WorkControl is for generated parsers; not a stable API.
type WorkControl struct {
	ctx    context.Context
	limits WorkLimits

	// The units reserved are inputBase and stepsBase, which the slow path of
	// charge keeps, plus what the fast path reserved since (inputUsed,
	// stepsUsed). inputRoom and stepsRoom are the units the fast path can still
	// reserve without reaching a poll boundary or exceeding a limit, out of the
	// inputGrant and stepsGrant that refill computed, so that the common charge
	// is two subtractions that inline into the matchers.
	inputBase, stepsBase   uint64
	inputGrant, stepsGrant uint64
	inputRoom, stepsRoom   uint64

	inputSinceCheck uint64
	stepsSinceCheck uint64
	interrupted     bool // distinguishes work interruptions from ordinary parse errors
}

// newWorkControl returns nil when neither cancellation nor a finite budget is
// active. A nil context is equivalent to context.Background.
func newWorkControl(ctx context.Context, limits WorkLimits) *WorkControl {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Done() == nil && limits.MaxInputWork == 0 && limits.MaxSteps == 0 {
		return nil
	}
	c := &WorkControl{ctx: ctx, limits: limits}
	c.refill()
	return c
}

// inputUsed and stepsUsed return the input-work and step units reserved so far.
func (c *WorkControl) inputUsed() uint64 { return c.inputBase + (c.inputGrant - c.inputRoom) }
func (c *WorkControl) stepsUsed() uint64 { return c.stepsBase + (c.stepsGrant - c.stepsRoom) }

// settle moves what the fast path of charge reserved into the bases and the
// poll counters, leaving no room.
func (c *WorkControl) settle() {
	in, steps := c.inputGrant-c.inputRoom, c.stepsGrant-c.stepsRoom
	c.inputBase, c.stepsBase = c.inputBase+in, c.stepsBase+steps
	c.inputSinceCheck, c.stepsSinceCheck = c.inputSinceCheck+in, c.stepsSinceCheck+steps
	c.inputGrant, c.stepsGrant, c.inputRoom, c.stepsRoom = 0, 0, 0, 0
}

// refill grants the fast path of charge the room left by the settled counters.
func (c *WorkControl) refill() {
	c.inputGrant = workRoom(c.inputBase, c.inputSinceCheck, c.limits.MaxInputWork, workCheckInputInterval)
	c.stepsGrant = workRoom(c.stepsBase, c.stepsSinceCheck, c.limits.MaxSteps, workCheckStepInterval)
	c.inputRoom, c.stepsRoom = c.inputGrant, c.stepsGrant
}

// workRoom returns the smallest request that reaches the poll interval
// (reachesInterval) or exceeds the limit (exceedsLimit).
func workRoom(used, since, limit, interval uint64) uint64 {
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
func (c *WorkControl) Check() {
	if c == nil || c.ctx == nil {
		return
	}
	if err := contextErr(c.ctx); err != nil {
		c.interrupted = true
		panic(fatal{err: err})
	}
	c.settle()
	c.inputSinceCheck = 0
	c.stepsSinceCheck = 0
	c.refill()
}

// Charge atomically reserves input-work and step units. Cancellation wins when
// it is observed at a poll or when either reservation would fail; otherwise
// input-work exhaustion takes priority over step exhaustion. A failed
// reservation leaves both counters unchanged.
func (c *WorkControl) Charge(inputWork, steps uint64) {
	if c != nil && inputWork < c.inputRoom && steps < c.stepsRoom {
		c.inputRoom -= inputWork
		c.stepsRoom -= steps
		return
	}
	c.chargeSlow(inputWork, steps)
}

// chargeInput is charge(n, 1), and chargeStep is charge(0, 1), for the
// matchers and rule calls. c must not be nil in chargeInput; chargeStep does
// nothing on a nil receiver. chargeStep need not test inputRoom: outside
// chargeSlow it is at least 1, which is the room of a request of no input.
func (c *WorkControl) chargeInput(n uint64) {
	if n < c.inputRoom && c.stepsRoom > 1 {
		c.inputRoom -= n
		c.stepsRoom--
	} else {
		c.chargeSlow(n, 1)
	}
}

func (c *WorkControl) chargeStep() {
	if c == nil {
		return
	}
	if c.stepsRoom > 1 {
		c.stepsRoom--
	} else {
		c.chargeSlow(0, 1)
	}
}

// chargeSlow is charge at a poll boundary or a limit, or on a nil receiver.
func (c *WorkControl) chargeSlow(inputWork, steps uint64) {
	if c == nil {
		return
	}
	c.settle()

	inputExceeded := exceedsLimit(c.inputBase, inputWork, c.limits.MaxInputWork)
	stepsExceeded := exceedsLimit(c.stepsBase, steps, c.limits.MaxSteps)
	poll := reachesInterval(c.inputSinceCheck, inputWork, workCheckInputInterval) ||
		reachesInterval(c.stepsSinceCheck, steps, workCheckStepInterval)

	// Check on each poll boundary and on a failed reservation so an already
	// canceled invocation wins over a simultaneously exhausted budget.
	if poll || inputExceeded || stepsExceeded {
		if c.ctx != nil {
			if err := contextErr(c.ctx); err != nil {
				c.interrupted = true
				panic(fatal{err: err})
			}
		}
	}
	if inputExceeded {
		c.interrupted = true
		panic(fatal{err: &WorkLimitError{
			Kind: InputWorkLimit, Limit: c.limits.MaxInputWork,
			Used: c.inputBase, Requested: inputWork,
		}})
	}
	if stepsExceeded {
		c.interrupted = true
		panic(fatal{err: &WorkLimitError{
			Kind: StepWorkLimit, Limit: c.limits.MaxSteps,
			Used: c.stepsBase, Requested: steps,
		}})
	}

	c.inputBase = saturatingAdd(c.inputBase, inputWork)
	c.stepsBase = saturatingAdd(c.stepsBase, steps)
	if poll {
		c.inputSinceCheck = 0
		c.stepsSinceCheck = 0
	} else {
		c.inputSinceCheck = saturatingAdd(c.inputSinceCheck, inputWork)
		c.stepsSinceCheck = saturatingAdd(c.stepsSinceCheck, steps)
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

// Work helpers keep long engine-owned scans between bounded reservations.
//
// InputWork has one guarded call so that it inlines into the matchers.
func (p *Parser) InputWork(n uint64) {
	if p.Document != nil {
		p.inputWorkTracked(n)
	} else if WorkControlsEnabled && p.Control != nil {
		p.Control.chargeInput(n)
	}
}

// inputWorkOrdinary is InputWork in an ordinary parse (ExpectOrdinary), which tracks no input.
func (p *Parser) inputWorkOrdinary(uint64) {}

// inputWorkTracked is InputWork in a Document or controlled parse.
func (p *Parser) inputWorkTracked(n uint64) {
	if p.Document != nil {
		// Generated matchers can read directly after charging input work.
		// Overestimating the dependency only discards a reusable memo entry.
		if n > uint64(int(^uint(0)>>1)-p.Pos) {
			p.documentTouch(int(^uint(0) >> 1))
		} else {
			p.documentTouch(p.Pos + int(n))
		}
	}
	if WorkControlsEnabled && p.Control != nil {
		p.Control.Charge(n, 1)
	}
}
func (p *Parser) StepWork() {
	p.Control.chargeStep()
}

func (p *Parser) setControlledSource(s string) {
	n, valid := 0, len(s) <= 1<<31-1
	// Outside a Document, charge the control directly: InputWork is too large to inline.
	c := p.Control
	if p.Document != nil {
		c = nil
	}
	for off := 0; off < len(s); {
		if w := uint64(min(utf8.UTFMax, len(s)-off)); c != nil {
			c.chargeInput(w)
		} else {
			p.InputWork(w)
		}
		r, size := utf8.DecodeRuneInString(s[off:])
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
	for off, i := 0, 0; off < len(s); i++ {
		p.InputWork(uint64(min(utf8.UTFMax, len(s)-off)))
		r, size := utf8.DecodeRuneInString(s[off:])
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
	if WorkControlsEnabled && p.Control != nil {
		return p.literalHasReplacementControlled(rs)
	}
	return slices.Contains(rs, utf8.RuneError)
}

func (p *Parser) literalHasReplacementControlled(rs []rune) bool {
	for _, r := range rs {
		p.StepWork()
		if r == utf8.RuneError {
			return true
		}
	}
	return false
}

func (p *Parser) hasLiteralPrefix(text string) bool {
	src := p.bs[p.Pos:]
	if !WorkControlsEnabled || p.Control == nil {
		return strings.HasPrefix(src, text)
	}
	if len(src) < len(text) {
		p.InputWork(1)
		return false
	}
	for off := 0; off < len(text); {
		n := min(4096, len(text)-off)
		p.InputWork(uint64(n))
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
func RunParseConverted[T any](r *Rule, input string, o parseOptions, conv func(*Node, *WorkControl, error) T) (res T, err error) {
	defer recoverParse(&res, &err)
	o = prepareOptions(o)
	n, err := parseConversionInput(r, input, o)
	o.control.Check()
	res = conv(n, o.control, err)
	o.control.Check()
	return res, err
}

// Ordinary runtime/depth errors retain the configured ParseAST converter's
// historical nil mapping. Work interruptions bypass conversion without calling
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
	return parseResolvedWork(r, input, o)
}

// WorkRange is for generated parsers; not a stable API.
type WorkRange struct{ Lo, Hi rune }

func (p *Parser) RejectWorkClass(ch rune, ranges []WorkRange, negated bool) bool {
	for _, r := range ranges {
		p.StepWork()
		if ch >= r.Lo && ch <= r.Hi {
			return negated
		}
	}
	return !negated
}

// Value strings may be action-created, so their scans consume steps alone.
const valueWorkChunk = 128

func (p *Parser) valueWork(n int) {
	if WorkControlsEnabled && p.Control != nil {
		p.Control.Charge(0, uint64(n))
	}
}

func (p *Parser) compareText(l, r string) int {
	if WorkControlsEnabled && p.Control != nil {
		return p.compareTextControlled(l, r)
	}
	return strings.Compare(l, r)
}

func (p *Parser) compareTextControlled(l, r string) int {
	for off, end := 0, min(len(l), len(r)); off < end; {
		n := min(valueWorkChunk, end-off)
		p.valueWork(2 * n)
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
	if WorkControlsEnabled && p.Control != nil {
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
				n := min(valueWorkChunk, len(s)-off)
				p.valueWork(n)
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
	if WorkControlsEnabled && c.P.Control != nil {
		return rtBinaryWork(c.P, op, l, r)
	}
	return rtBinary(op, l, r)
}

// detachedText constructs normalized text when malformed source bytes prevent
// slicing the original string. Allocation and Grow remain nonpreemptible.
func (p *Parser) detachedText(start, end int) string {
	src := p.In[start:end]
	size := 0
	for off := 0; off < len(src); {
		n := min(valueWorkChunk, len(src)-off)
		p.Control.Charge(uint64(n), uint64(n))
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
		n := min(valueWorkChunk, len(src)-off)
		p.Control.Charge(uint64(n), uint64(n))
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

func copyWork[T any](p *Parser, dst, src []T) {
	if WorkControlsEnabled && p.Control != nil {
		copyWorkControlled(p, dst, src)
		return
	}
	copy(dst, src)
}

func copyWorkControlled[T any](p *Parser, dst, src []T) {
	// Engine callers use disjoint storage; overlapping moves need their own
	// direction-aware implementation rather than a chunked forward copy.
	for off, end := 0, min(len(dst), len(src)); off < end; {
		n := min(valueWorkChunk, end-off)
		p.valueWork(n)
		copy(dst[off:off+n], src[off:off+n])
		off += n
	}
}

// growWork bounds the explicit copy of existing values when capacity must
// grow. Allocation and runtime zeroing remain nonpreemptible intervals.
func growWork[T any](p *Parser, values []T, extra int) []T {
	if WorkControlsEnabled && p.Control != nil {
		return growWorkControlled(p, values, extra)
	}
	return slices.Grow(values, extra)
}

func growWorkControlled[T any](p *Parser, values []T, extra int) []T {
	if extra < 0 {
		return slices.Grow(values, extra) // reject negative growth before spare-capacity checks
	}
	if extra <= cap(values)-len(values) {
		return values
	}
	maxInt := int(^uint(0) >> 1)
	if extra > maxInt-len(values) {
		return slices.Grow(values, extra) // preserve the library's invalid-size panic
	}
	capacity := len(values) + extra
	if cap(values) <= maxInt/2 {
		// Keep repeated single-element additions amortized; allocating only
		// the exact new length would repeatedly copy the accumulated prefix.
		capacity = max(capacity, 2*cap(values))
	}
	out := make([]T, len(values), capacity)
	copyWork(p, out, values)
	return out
}

func appendWork[T any](p *Parser, dst, src []T) []T {
	if WorkControlsEnabled && p.Control != nil {
		return appendWorkControlled(p, dst, src)
	}
	return append(dst, src...)
}

func appendWorkControlled[T any](p *Parser, dst, src []T) []T {
	for off := 0; off < len(src); {
		n := min(valueWorkChunk, len(src)-off)
		p.valueWork(n)
		dst = append(dst, src[off:off+n]...)
		off += n
	}
	return dst
}

// equalExpected preserves the native snapshot-identity fast path while charging
// each comparison under active work controls. Snapshots are never modified.
func (p *Parser) equalExpected(a, b []ExpID) bool {
	if WorkControlsEnabled && p.Control != nil {
		return p.equalExpectedControlled(a, b)
	}
	return slices.Equal(a, b)
}

func (p *Parser) equalExpectedControlled(a, b []ExpID) bool {
	if len(a) != len(b) {
		return false
	}
	for i, x := range a {
		p.StepWork()
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
	if WorkControlsEnabled && p.Control != nil {
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
				p.StepWork()
				if order <= 0 {
					dst[k] = src[i]
					i++
				} else {
					dst[k] = src[j]
					j++
				}
				k++
			}
			copyWork(p, dst[k:k+mid-i], src[i:mid])
			k += mid - i
			copyWork(p, dst[k:hi], src[j:hi])
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
		copyWork(p, values, src)
	}
}

// RecoveryErrorText is only for engine-owned Error-node materialization.
// Public post-return Error/Message calls remain caller work. The local builder
// owns its immutable output and is never pooled or written after return.
func (p *Parser) RecoveryErrorText(e *SyntaxError) string {
	if WorkControlsEnabled && p.Control != nil {
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
		p.StepWork()
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
		p.StepWork()
		if i > 0 {
			p.writeDiagnosticText(&out, sep)
		}
		p.writeDiagnosticText(&out, part)
	}
	return out.String()
}

func (p *Parser) writeDiagnosticText(out *strings.Builder, text string) {
	for off := 0; off < len(text); {
		n := min(valueWorkChunk, len(text)-off)
		p.valueWork(n)
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
		p.StepWork()
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
			p.InputWork(1)
			if p.unit == Bytes && p.bs[i] == '\n' || p.unit == CodePoints && p.In[i] == '\n' {
				p.lines = append(p.lines, i+1)
			}
		}
	}
	pos = min(pos, p.n)
	lo, hi := 0, len(p.lines)
	for lo < hi {
		p.StepWork()
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

func (p *Parser) copyErrors(dst, src []*SyntaxError) []*SyntaxError { return appendWork(p, dst, src) }

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
		p.StepWork()
		if !p.rawValueEqual(v, r[i]) {
			return false
		}
	}
	return true
}

func (p *Parser) bindControlled(name string, val any) {
	p.StepWork()
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
		p.StepWork()
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
		p.StepWork()
		tail.next = &Env{name: cur.name, val: cur.val}
		tail = tail.next
	}
	tail.next = old.next
	// Saved environments are immutable; only publish a completed prefix.
	p.Env = head
}

func clearWork[T any](p *Parser, values []T) {
	if WorkControlsEnabled && p.Control != nil {
		clearWorkControlled(p, values)
		return
	}
	clear(values)
}

func clearWorkControlled[T any](p *Parser, values []T) {
	for off := 0; off < len(values); {
		n := min(valueWorkChunk, len(values)-off)
		p.valueWork(n)
		clear(values[off : off+n])
		off += n
	}
}

func (p *Parser) controlledEnvValues(names []string) []any {
	p.StepWork()
	if len(names) == 0 {
		return nil
	}
	if p.Env == nil && len(names) <= len(noVals) {
		return noVals[:len(names):len(names)]
	}
	vals := make([]any, len(names))
	for i, name := range names {
		p.StepWork()
		for e := p.Env; e != nil; e = e.next {
			p.StepWork()
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
		p.StepWork()
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
	if WorkControlsEnabled && p.Control != nil {
		return p.memoGetControlled(rule, pos, min, key)
	}
	return p.memo.get(rule, pos, min, key)
}

func (p *Parser) memoGetControlled(rule, pos, min int, key []any) *memoEntry {
	p.StepWork()
	if pos >= len(p.memo.slots) {
		return nil
	}
	for e := p.memo.slots[pos]; e != nil; e = e.next {
		p.StepWork()
		if e.rule == int32(rule) && e.min == int32(min) && p.memoKeyEqual(e.env, key) {
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
	if WorkControlsEnabled && p.Control != nil {
		p.memoPutControlled(rule, pos, min, key, e)
		return
	}
	p.memo.put(rule, pos, min, key, e)
}

func (p *Parser) memoPutControlled(rule, pos, min int, key []any, e *memoEntry) {
	p.StepWork()
	slots := p.memo.slots
	if pos >= len(slots) {
		if pos < cap(slots) {
			slots = slots[:pos+1]
		} else {
			grown := make([]*memoEntry, pos+1, max(2*cap(slots), pos+1, 64))
			copyWork(p, grown, slots)
			slots = grown
		}
	}
	link := &slots[pos]
	var replaced *memoEntry
	for x := *link; x != nil; link, x = &x.next, x.next {
		p.StepWork()
		if x == e || x.rule == int32(rule) && x.min == int32(min) && p.memoKeyEqual(x.env, key) {
			replaced = x
			break
		}
	}
	p.StepWork()
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
	p.StepWork()
	t := &p.memo
	if t.calls == nil {
		t.calls = make([]seenCalls, t.stride)
	}
	if !t.calls[r.Seen].eager && t.sparse {
		if len(t.pages) != t.stride {
			p.StepWork()
			t.pages = make([]seenRulePages, t.stride)
		}
		dir := &t.pages[r.Seen]
		at := p.Pos >> 10
		if at >= len(dir.pages) {
			pages := growWork(p, dir.pages, at+1-len(dir.pages))
			dir.pages = pages[:at+1]
		}
		if dir.pages[at] == nil {
			p.StepWork()
			// Reserve capacity before alloc's append so copying a large arena
			// directory uses the invocation's bounded copy helper.
			if t.bitPages.n/64 == len(t.bitPages.chunks) {
				t.bitPages.chunks = growWork(p, t.bitPages.chunks, 1)
			}
			dir.pages[at] = t.bitPages.alloc()
		}
		dir.last, dir.at = dir.pages[at], at
	} else if !t.calls[r.Seen].eager {
		w := (p.Pos*t.stride + r.Seen) >> 6
		if w >= len(t.seen) {
			n := len(t.seen) + max(w+1-len(t.seen), len(t.seen), 64)
			seen := t.seen
			if n <= cap(seen) {
				seen = seen[:n]
				clearWork(p, seen[len(t.seen):])
			} else {
				seen = make([]uint64, n)
				copyWork(p, seen, t.seen)
			}
			t.seen = seen
		}
	}
}

// Graph helpers account for normal construction and capture restoration.
// Release/recycle, DropKids/DropTrail and arena reset remain unconditional.
func (p *Parser) valueField(fs Fields, name string) (any, bool) {
	if WorkControlsEnabled && p.Control != nil {
		return p.valueFieldControlled(fs, name)
	}
	return fs.Get(name)
}

func (p *Parser) valueFieldControlled(fs Fields, name string) (any, bool) {
	for _, f := range fs {
		p.StepWork()
		if p.rawValueEqual(f.Name, name) {
			return f.Value, true
		}
	}
	return nil, false
}
func (p *Parser) setValueField(fs *Fields, name string, v any) {
	if WorkControlsEnabled && p.Control != nil {
		p.setValueFieldControlled(fs, name, v)
		return
	}
	fs.set(name, v)
}

func (p *Parser) setValueFieldControlled(fs *Fields, name string, v any) {
	for i := range *fs {
		p.StepWork()
		if p.rawValueEqual((*fs)[i].Name, name) {
			(*fs)[i].Value = v
			return
		}
	}
	p.StepWork()
	*fs = append(*fs, NodeField{name, v})
}
func (p *Parser) pushKid(v *Node) { p.StepWork(); p.KidStack = append(p.KidStack, v) }
func (p *Parser) undoCaptures(from int) {
	for i := len(p.Trail) - 1; i >= from; i-- {
		p.StepWork()
		u := p.Trail[i]
		u.f.Vals[u.slot] = u.old
	}
}

func (c *ActCtx) Fold(fn string, right bool, acc, list any, f func(acc, item any) any) any {
	if WorkControlsEnabled && c.P.Control != nil {
		return c.foldControlled(fn, right, acc, list, f)
	}
	return rtFold(fn, right, acc, list, f)
}

func (c *ActCtx) foldControlled(fn string, right bool, acc, list any, f func(acc, item any) any) any {
	items := c.ListItems(fn, list)
	for i := range items {
		c.P.StepWork()
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
		p.StepWork()
		size = checkedDiagnosticSize(size, len(part))
	}
	var out strings.Builder
	out.Grow(size)
	for _, part := range parts {
		p.StepWork()
		p.writeDiagnosticText(&out, part)
	}
	return out.String()
}

// evalErrorf accepts the engine's fixed %s/%d leaf formats. The inactive path
// retains fmt semantics; active formatting splits known pieces before copying.
func (p *Parser) evalErrorf(format string, args ...any) {
	if WorkControlsEnabled && p != nil && p.Control != nil {
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
	if !WorkControlsEnabled || p.Control == nil {
		return fmt.Errorf("action in %s: %s", where, message)
	}
	return errors.New(p.actionErrorText("action in ", where, ": ", message))
}

func (c *ActCtx) Unary(op string, v any) any {
	if WorkControlsEnabled && c.P.Control != nil {
		return c.unaryControlled(op, v)
	}
	return rtUnary(op, v)
}

func (c *ActCtx) unaryControlled(op string, v any) any {
	return rtUnaryWork(c.P, op, v)
}
func (c *ActCtx) And(l any, r func() any) any {
	if WorkControlsEnabled && c.P.Control != nil {
		return c.andControlled(l, r)
	}
	return rtAnd(l, r)
}

func (c *ActCtx) andControlled(l any, r func() any) any {
	if !rtBoolWork(c.P, "&&", l) {
		return false
	}
	return rtBoolWork(c.P, "&&", r())
}
func (c *ActCtx) Or(l any, r func() any) any {
	if WorkControlsEnabled && c.P.Control != nil {
		return c.orControlled(l, r)
	}
	return rtOr(l, r)
}

func (c *ActCtx) orControlled(l any, r func() any) any {
	if rtBoolWork(c.P, "||", l) {
		return true
	}
	return rtBoolWork(c.P, "||", r())
}
func (c *ActCtx) ListItems(fn string, v any) []*Node {
	if WorkControlsEnabled && c.P.Control != nil {
		return c.listItemsControlled(fn, v)
	}
	return listItems(fn, v)
}

func (c *ActCtx) listItemsControlled(fn string, v any) []*Node {
	return listItemsWork(c.P, fn, v)
}
func (c *ActCtx) asNode(fn string, v any) *Node {
	switch v.(type) {
	case int, string, bool:
		// An int, string or bool element travels boxed, as the result of an action does.
		return c.P.NewNode(Node{RawKind: kindBox, box: v, RawStart: int32(c.start), RawEnd: int32(c.end)})
	}
	if !WorkControlsEnabled || c.P.Control == nil {
		return asNode(fn, v)
	}
	return asNodeWork(c.P, fn, v)
}
