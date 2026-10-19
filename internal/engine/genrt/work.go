package genrt

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
// limit leaves that counter unlimited.
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

const workControlsEnabled = true

const (
	workCheckStepInterval  = uint64(256)
	workCheckInputInterval = uint64(4096)
)

// workControl is private state for a single parser invocation. Callers must
// split long operations into bounded reservations so cancellation can be
// observed between chunks.
type workControl struct {
	ctx    context.Context
	limits WorkLimits

	inputUsed uint64
	stepsUsed uint64

	inputSinceCheck uint64
	stepsSinceCheck uint64
	interrupted     bool // distinguishes work interruptions from ordinary parse errors
}

// newWorkControl returns nil when neither cancellation nor a finite budget is
// active. A nil context is equivalent to context.Background.
func newWorkControl(ctx context.Context, limits WorkLimits) *workControl {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Done() == nil && limits.MaxInputWork == 0 && limits.MaxSteps == 0 {
		return nil
	}
	return &workControl{ctx: ctx, limits: limits}
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

// check observes cancellation at an explicit engine checkpoint. It is safe on
// a nil receiver.
func (c *workControl) check() {
	if c == nil || c.ctx == nil {
		return
	}
	if err := contextErr(c.ctx); err != nil {
		c.interrupted = true
		panic(fatal{err: err})
	}
	c.inputSinceCheck = 0
	c.stepsSinceCheck = 0
}

// charge atomically reserves input-work and step units. Cancellation wins when
// it is observed at a poll or when either reservation would fail; otherwise
// input-work exhaustion takes priority over step exhaustion. A failed
// reservation leaves both counters unchanged.
func (c *workControl) charge(inputWork, steps uint64) {
	if !workControlsEnabled || c == nil {
		return
	}

	inputExceeded := exceedsLimit(c.inputUsed, inputWork, c.limits.MaxInputWork)
	stepsExceeded := exceedsLimit(c.stepsUsed, steps, c.limits.MaxSteps)
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
			Used: c.inputUsed, Requested: inputWork,
		}})
	}
	if stepsExceeded {
		c.interrupted = true
		panic(fatal{err: &WorkLimitError{
			Kind: StepWorkLimit, Limit: c.limits.MaxSteps,
			Used: c.stepsUsed, Requested: steps,
		}})
	}

	c.inputUsed = saturatingAdd(c.inputUsed, inputWork)
	c.stepsUsed = saturatingAdd(c.stepsUsed, steps)
	if poll {
		c.inputSinceCheck = 0
		c.stepsSinceCheck = 0
		return
	}
	c.inputSinceCheck = saturatingAdd(c.inputSinceCheck, inputWork)
	c.stepsSinceCheck = saturatingAdd(c.stepsSinceCheck, steps)
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
func (p *parser) inputWork(n uint64) {
	if documentEnabled && p.document != nil {
		// Generated matchers can read directly after charging input work.
		// Overestimating the dependency only discards a reusable memo entry.
		if n > uint64(int(^uint(0)>>1)-p.pos) {
			p.documentTouch(int(^uint(0) >> 1))
		} else {
			p.documentTouch(p.pos + int(n))
		}
	}
	if workControlsEnabled && p.control != nil {
		p.control.charge(n, 1)
	}
}
func (p *parser) stepWork() {
	if workControlsEnabled && p.control != nil {
		p.control.charge(0, 1)
	}
}

func (p *parser) setControlledSource(s string) {
	n, valid := 0, len(s) <= 1<<31-1
	for off := 0; off < len(s); {
		p.inputWork(uint64(min(utf8.UTFMax, len(s)-off)))
		r, size := utf8.DecodeRuneInString(s[off:])
		if r == utf8.RuneError && size == 1 {
			valid = false
		}
		n++
		off += size
	}
	if cap(p.in) >= n {
		p.in = p.in[:n]
	} else {
		p.in = make([]rune, n)
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
		p.inputWork(uint64(min(utf8.UTFMax, len(s)-off)))
		r, size := utf8.DecodeRuneInString(s[off:])
		p.in[i] = r
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

func (p *parser) literalHasReplacement(rs []rune) bool {
	if !workControlsEnabled || p.control == nil {
		return slices.Contains(rs, utf8.RuneError)
	}
	for _, r := range rs {
		p.stepWork()
		if r == utf8.RuneError {
			return true
		}
	}
	return false
}

func (p *parser) hasLiteralPrefix(text string) bool {
	src := p.bs[p.pos:]
	if !workControlsEnabled || p.control == nil {
		return strings.HasPrefix(src, text)
	}
	if len(src) < len(text) {
		p.inputWork(1)
		return false
	}
	for off := 0; off < len(text); {
		n := min(4096, len(text)-off)
		p.inputWork(uint64(n))
		if src[off:off+n] != text[off:off+n] {
			return false
		}
		off += n
	}
	return true
}

// parseConverted shares admission and counters with a Node-to-typed converter.
func parseConverted[T any](r *rule, seen int, input string, o parseOptions, conv func(*Node, *workControl, error) T) (res T, err error) {
	defer recoverParse(&res, &err)
	o = prepareOptions(o)
	n, err := parseConversionInput(r, seen, input, o)
	o.control.check()
	res = conv(n, o.control, err)
	o.control.check()
	return res, err
}

// Ordinary runtime/depth errors retain the configured ParseAST converter's
// historical nil mapping. Work interruptions bypass conversion without calling
// methods on a caller-owned error to classify it.
func parseConversionInput(r *rule, seen int, input string, o parseOptions) (n *Node, err error) {
	defer func() {
		if x := recover(); x != nil {
			if f, ok := x.(fatal); ok && (o.control == nil || !o.control.interrupted) {
				n, err = nil, f.err
			} else {
				panic(x)
			}
		}
	}()
	return parseResolvedWork(r, seen, input, o)
}

type workRange struct{ lo, hi rune }

func (p *parser) rejectWorkClass(ch rune, ranges []workRange, negated bool) bool {
	for _, r := range ranges {
		p.stepWork()
		if ch >= r.lo && ch <= r.hi {
			return negated
		}
	}
	return !negated
}

// Value strings may be action-created, so their scans consume steps alone.
const valueWorkChunk = 128

func (p *parser) valueWork(n int) {
	if workControlsEnabled && p.control != nil {
		p.control.charge(0, uint64(n))
	}
}

func (p *parser) compareText(l, r string) int {
	if !workControlsEnabled || p.control == nil {
		return strings.Compare(l, r)
	}
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

func (p *parser) textEqual(l, r string) bool {
	return len(l) == len(r) && p.compareText(l, r) == 0
}

// stringBinary handles only string pairs; callers retain their existing
// runtime's scalar, pointer-identity and invalid-operand behavior.
func (p *parser) stringBinary(op string, l, r any) (any, bool) {
	if !workControlsEnabled || p.control == nil {
		return nil, false
	}
	ls, lok := l.(string)
	rs, rok := r.(string)
	if !lok || !rok {
		return nil, false
	}
	switch op {
	case "==":
		return p.textEqual(ls, rs), true
	case "!=":
		return !p.textEqual(ls, rs), true
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

// fieldOperand reads x.name as member does, keeping the text of a text struct unboxed.
func (c *actx) fieldOperand(x any, name string) toperand {
	return c.memberOperand(x, name)
}

// compare applies a comparison operator as binary does, comparing two strings without boxing them.
func (c *actx) compare(op string, l, r toperand) any {
	if !l.isStr {
		l.str, l.isStr = l.v.(string)
	}
	if !r.isStr {
		r.str, r.isStr = r.v.(string)
	}
	if l.isStr && r.isStr {
		if v, ok := c.p.compareStrings(op, l.str, r.str); ok {
			return v
		}
	}
	return c.binary(op, l.value(), r.value())
}

// textFieldName reports whether name is Text, the field of a text struct, charging the steps of
// valueField looking it up in the field list {Text}.
func (p *parser) textFieldName(name string) bool {
	if !workControlsEnabled || p.control == nil {
		return name == "Text"
	}
	p.stepWork()
	return p.textEqual("Text", name)
}

// compareStrings applies the comparison operator op to two strings as stringBinary does.
func (p *parser) compareStrings(op string, l, r string) (bool, bool) {
	switch op {
	case "==":
		return p.textEqual(l, r), true
	case "!=":
		return !p.textEqual(l, r), true
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

func (c *actx) binary(op string, l, r any) any {
	if v, ok := c.p.stringBinary(op, l, r); ok {
		return v
	}
	if workControlsEnabled && c.p.control != nil {
		return rtBinaryWork(c.p, op, l, r)
	}
	return rtBinary(op, l, r)
}

// detachedText constructs normalized text when malformed source bytes prevent
// slicing the original string. Allocation and Grow remain nonpreemptible.
func (p *parser) detachedText(start, end int) string {
	src := p.in[start:end]
	size := 0
	for off := 0; off < len(src); {
		n := min(valueWorkChunk, len(src)-off)
		p.control.charge(uint64(n), uint64(n))
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
		p.control.charge(uint64(n), uint64(n))
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

func copyWork[T any](p *parser, dst, src []T) {
	if !workControlsEnabled || p.control == nil {
		copy(dst, src)
		return
	}
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
func growWork[T any](p *parser, values []T, extra int) []T {
	if !workControlsEnabled || p.control == nil {
		return slices.Grow(values, extra)
	}
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

func appendWork[T any](p *parser, dst, src []T) []T {
	if !workControlsEnabled || p.control == nil {
		return append(dst, src...)
	}
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
func (p *parser) equalExpected(a, b []expID) bool {
	if !workControlsEnabled || p.control == nil {
		return slices.Equal(a, b)
	}
	if len(a) != len(b) {
		return false
	}
	for i, x := range a {
		p.stepWork()
		if x != b[i] {
			return false
		}
	}
	return true
}

// sortExpected sorts a newly constructed, parser-owned diagnostic. Comparisons
// and explicit moves are bounded; allocation and GC remain nonpreemptible.
// Published or memoized errors must never be passed to this helper.
func (p *parser) sortExpected(values []string) {
	if !workControlsEnabled || p.control == nil {
		sort.Strings(values)
		return
	}
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
				p.stepWork()
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

// recoveryErrorText is only for engine-owned Error-node materialization.
// Public post-return Error/Message calls remain caller work. The local builder
// owns its immutable output and is never pooled or written after return.
func (p *parser) recoveryErrorText(e *SyntaxError) string {
	if !workControlsEnabled || p.control == nil {
		return e.Error()
	}
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
		p.stepWork()
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
		p.stepWork()
		if i > 0 {
			p.writeDiagnosticText(&out, sep)
		}
		p.writeDiagnosticText(&out, part)
	}
	return out.String()
}

func (p *parser) writeDiagnosticText(out *strings.Builder, text string) {
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
func (p *parser) makeControlledError(pos int, expected []expID) *SyntaxError {
	e := &SyntaxError{Pos: pos}
	for _, x := range expected {
		p.stepWork()
		if x&msgBit != 0 {
			e.Messages = append(e.Messages, descs[x & ^msgBit])
		} else {
			e.Expected = append(e.Expected, descs[x])
		}
	}
	p.sortExpected(e.Expected)
	// stream:begin
	if p.stream != nil {
		e.Line, e.Col = p.streamLineCol(pos)
		return e
	}
	// stream:end
	if p.lines == nil {
		p.lines = append(p.lines, 0)
		for i := 0; i < p.n; i++ {
			p.inputWork(1)
			if p.unit == Bytes && p.bs[i] == '\n' || p.unit == CodePoints && p.in[i] == '\n' {
				p.lines = append(p.lines, i+1)
			}
		}
	}
	pos = min(pos, p.n)
	lo, hi := 0, len(p.lines)
	for lo < hi {
		p.stepWork()
		mid := lo + (hi-lo)/2
		if p.lines[mid] < pos+1 {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	e.Line, e.Col = lo, pos-p.lines[lo-1]+1
	p.control.check()
	return e
}

func (p *parser) copyErrors(dst, src []*SyntaxError) []*SyntaxError { return appendWork(p, dst, src) }

// rawValueEqual preserves the interface equality of memo keys and bindings.
// In particular, a typed nil pointer differs from a nil interface. Action
// valueEqual has different nil semantics and must not be used for these keys.
func (p *parser) rawValueEqual(l, r any) bool {
	if s, ok := l.(string); ok {
		t, ok := r.(string)
		return ok && len(s) == len(t) && p.compareText(s, t) == 0
	}
	return l == r
}

func (p *parser) memoKeyEqual(l, r []any) bool {
	if len(l) != len(r) {
		return false
	}
	for i, v := range l {
		p.stepWork()
		if !p.rawValueEqual(v, r[i]) {
			return false
		}
	}
	return true
}

func (p *parser) bindControlled(name string, val any) {
	p.stepWork()
	e := p.env
	if p.bound == nil {
		p.bound = make(map[string]bool)
	}
	if e == nil {
		p.bound[name] = true
		p.env = &env{name: name, val: val}
		return
	}
	if p.rawValueEqual(e.name, name) {
		if !p.rawValueEqual(e.val, val) {
			p.env = &env{name: name, val: val, next: e.next}
		}
		return
	}
	// Map hashing and allocation are runtime operations that cannot be split
	// into engine checkpoints. Explicit name comparisons above/below can be.
	if !p.bound[name] {
		p.bound[name] = true
		p.env = &env{name: name, val: val, next: e}
		return
	}
	old := e
	for old != nil {
		p.stepWork()
		if p.rawValueEqual(old.name, name) {
			break
		}
		old = old.next
	}
	if old == nil {
		p.env = &env{name: name, val: val, next: e}
		return
	}
	if p.rawValueEqual(old.val, val) {
		return
	}
	head := &env{name: name, val: val}
	tail := head
	for cur := e; cur != old; cur = cur.next {
		p.stepWork()
		tail.next = &env{name: cur.name, val: cur.val}
		tail = tail.next
	}
	tail.next = old.next
	// Saved environments are immutable; only publish a completed prefix.
	p.env = head
}

func clearWork[T any](p *parser, values []T) {
	if !workControlsEnabled || p.control == nil {
		clear(values)
		return
	}
	for off := 0; off < len(values); {
		n := min(valueWorkChunk, len(values)-off)
		p.valueWork(n)
		clear(values[off : off+n])
		off += n
	}
}

func (p *parser) controlledEnvValues(names []string) []any {
	p.stepWork()
	if len(names) == 0 {
		return nil
	}
	if p.env == nil && len(names) <= len(noVals) {
		return noVals[:len(names):len(names)]
	}
	vals := make([]any, len(names))
	for i, name := range names {
		p.stepWork()
		for e := p.env; e != nil; e = e.next {
			p.stepWork()
			if p.rawValueEqual(e.name, name) {
				vals[i] = e.val
				break
			}
		}
	}
	return vals
}

func (p *parser) lookupEnvControlled(name string) (any, bool) {
	for e := p.env; e != nil; e = e.next {
		p.stepWork()
		if p.rawValueEqual(e.name, name) {
			return e.val, true
		}
	}
	return nil, false
}

func (p *parser) memoGet(rule, pos, min int, key []any) *memoEntry {
	// stream:begin
	if p.stream != nil {
		// A call at a position before the pruned memo needs no entry (a
		// start rule called at 0, or still growing there).
		if pos -= p.stream.memoBase; pos < 0 {
			return nil
		}
	}
	// stream:end
	if !workControlsEnabled || p.control == nil {
		return p.memo.get(rule, pos, min, key)
	}
	p.stepWork()
	if pos >= len(p.memo.slots) {
		return nil
	}
	for e := p.memo.slots[pos]; e != nil; e = e.next {
		p.stepWork()
		if e.rule == int32(rule) && e.min == int32(min) && p.memoKeyEqual(e.env, key) {
			return e
		}
	}
	return nil
}

func (p *parser) memoPut(rule, pos, min int, key []any, e *memoEntry) {
	// stream:begin
	if p.stream != nil {
		if pos -= p.stream.memoBase; pos < 0 {
			return
		}
	}
	// stream:end
	if !workControlsEnabled || p.control == nil {
		p.memo.put(rule, pos, min, key, e)
		return
	}
	p.stepWork()
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
		p.stepWork()
		if x == e || x.rule == int32(rule) && x.min == int32(min) && p.memoKeyEqual(x.env, key) {
			replaced = x
			break
		}
	}
	p.stepWork()
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

func (p *parser) controlledFirstCall(r *rule) bool {
	if r.seen < 0 {
		return false
	}
	p.stepWork()
	t := &p.memo
	if t.calls == nil {
		t.calls = make([]seenCalls, t.stride)
	}
	if !t.calls[r.seen].eager && sparseSeen && t.sparse {
		if len(t.pages) != t.stride {
			p.stepWork()
			t.pages = make([]seenRulePages, t.stride)
		}
		dir := &t.pages[r.seen]
		at := p.pos >> 10
		if at >= len(dir.pages) {
			pages := growWork(p, dir.pages, at+1-len(dir.pages))
			dir.pages = pages[:at+1]
		}
		if dir.pages[at] == nil {
			p.stepWork()
			// Reserve capacity before alloc's append so copying a large arena
			// directory uses the invocation's bounded copy helper.
			if t.bitPages.n/64 == len(t.bitPages.chunks) {
				t.bitPages.chunks = growWork(p, t.bitPages.chunks, 1)
			}
			dir.pages[at] = t.bitPages.alloc()
		}
		dir.last, dir.at = dir.pages[at], at
	} else if !t.calls[r.seen].eager {
		w := (p.pos*t.stride + r.seen) >> 6
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
	return p.firstCallOrdinary(r)
}

// Graph helpers account for normal construction and capture restoration.
// Release/recycle, dropKids/dropTrail and arena reset remain unconditional.
func (p *parser) valueField(fs Fields, name string) (any, bool) {
	if !workControlsEnabled || p.control == nil {
		return fs.Get(name)
	}
	for _, f := range fs {
		p.stepWork()
		if p.rawValueEqual(f.Name, name) {
			return f.Value, true
		}
	}
	return nil, false
}
func (p *parser) setValueField(fs *Fields, name string, v any) {
	if !workControlsEnabled || p.control == nil {
		fs.set(name, v)
		return
	}
	for i := range *fs {
		p.stepWork()
		if p.rawValueEqual((*fs)[i].Name, name) {
			(*fs)[i].Value = v
			return
		}
	}
	p.stepWork()
	*fs = append(*fs, NodeField{name, v})
}
func (p *parser) pushKid(v *Node) { p.stepWork(); p.kidStack = append(p.kidStack, v) }
func (p *parser) undoCaptures(from int) {
	for i := len(p.trail) - 1; i >= from; i-- {
		p.stepWork()
		u := p.trail[i]
		u.f.vals[u.slot] = u.old
	}
}

func (c *actx) fold(fn string, right bool, acc, list any, f func(acc, item any) any) any {
	if !workControlsEnabled || c.p.control == nil {
		return rtFold(fn, right, acc, list, f)
	}
	items := c.listItems(fn, list)
	for i := range items {
		c.p.stepWork()
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
func (p *parser) actionErrorText(parts ...string) string {
	size := 0
	for _, part := range parts {
		p.stepWork()
		size = checkedDiagnosticSize(size, len(part))
	}
	var out strings.Builder
	out.Grow(size)
	for _, part := range parts {
		p.stepWork()
		p.writeDiagnosticText(&out, part)
	}
	return out.String()
}

// evalErrorf accepts the engine's fixed %s/%d leaf formats. The inactive path
// retains fmt semantics; active formatting splits known pieces before copying.
func (p *parser) evalErrorf(format string, args ...any) {
	if !workControlsEnabled || p == nil || p.control == nil {
		evalErrorf(format, args...)
		return
	}
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
func (p *parser) wrapEvalError(where, message string) error {
	if !workControlsEnabled || p.control == nil {
		return fmt.Errorf("action in %s: %s", where, message)
	}
	return errors.New(p.actionErrorText("action in ", where, ": ", message))
}

func (c *actx) unary(op string, v any) any {
	if !workControlsEnabled || c.p.control == nil {
		return rtUnary(op, v)
	}
	return rtUnaryWork(c.p, op, v)
}
func (c *actx) and(l any, r func() any) any {
	if !workControlsEnabled || c.p.control == nil {
		return rtAnd(l, r)
	}
	if !rtBoolWork(c.p, "&&", l) {
		return false
	}
	return rtBoolWork(c.p, "&&", r())
}
func (c *actx) or(l any, r func() any) any {
	if !workControlsEnabled || c.p.control == nil {
		return rtOr(l, r)
	}
	if rtBoolWork(c.p, "||", l) {
		return true
	}
	return rtBoolWork(c.p, "||", r())
}
func (c *actx) listItems(fn string, v any) []*Node {
	if !workControlsEnabled || c.p.control == nil {
		return listItems(fn, v)
	}
	return listItemsWork(c.p, fn, v)
}
func (c *actx) asNode(fn string, v any) *Node {
	if !workControlsEnabled || c.p.control == nil {
		return asNode(fn, v)
	}
	return asNodeWork(c.p, fn, v)
}
