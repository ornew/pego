// Package complexitytest measures how the parse cost of a grammar grows with the input and
// classifies the growth. The cost is the budget spend of a parse, which is deterministic for a
// grammar, an input and a backend.
//
// Steps reads the spend of an ordinary parse by bisection over Limits.Budget. StepsTraced reads
// it from the counter of a traced parse and is cheaper, but a traced parse spends a little more
// than an untraced one (1 to 9% in the tests; the cause is not investigated), so it is only an
// upper estimate; the suites use Steps.
package complexitytest

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/internal/engine/core"
	"github.com/ornew/zeroparse/internal/starttest"
)

// Backends are the backends whose cost is measured.
var Backends = []zp.Backend{zp.Closure, zp.Bytecode, zp.BytecodeIterative}

// BackendName names b for test output.
func BackendName(b zp.Backend) string {
	switch b {
	case zp.Closure:
		return "closure"
	case zp.Bytecode:
		return "bytecode"
	case zp.BytecodeIterative:
		return "iterative"
	}
	return fmt.Sprint(int(b))
}

// Compile compiles the ZeroParse source with every rule a start rule and main as the default start
// rule.
func Compile(src string) (*zp.Parser, error) {
	g, err := zp.ParseGrammar(src)
	if err != nil {
		return nil, err
	}
	return zp.Compile(starttest.MarkAll(g), "")
}

// Cost is the measured cost of one parse.
type Cost struct {
	// Steps is the budget spend. For a Capped cost it is the cap, a lower bound.
	Steps uint64
	// Capped reports that the parse exhausted the cap.
	Capped bool
	// ParseErr is the error of a parse that finished within the budget but reported a syntax
	// error. Only StepsLenient returns a Cost with it; Steps returns the error instead.
	ParseErr error
}

// parses reports whether the parse of input finishes within budget, with or without a syntax
// error. It returns the error of a parse that finished; an exhausted budget is not one.
func parses(p *zp.Parser, input string, b zp.Backend, budget uint64) (bool, error) {
	_, err := p.Parse(input, zp.WithBackend(b), zp.WithLimits(zp.Limits{Budget: budget}))
	var le *zp.LimitError
	if errors.As(err, &le) && le.Kind == zp.LimitBudget {
		return false, nil
	}
	return true, err
}

// Steps returns the spend of the untraced parse of input: the smallest budget with which the
// parse finishes, to within the tolerance below. A parse that still exhausts cap returns
// Cost{cap, true}. A parse that reports a syntax error is returned as an error.
//
// The search stops when the interval is within 0.4% of the result, which moves a slope by less
// than 0.01. ExactSteps searches to the unit.
func Steps(p *zp.Parser, input string, b zp.Backend, cap uint64) (Cost, error) {
	return strict(steps(p, input, b, cap, 256))
}

// ExactSteps is Steps with a search to the unit.
func ExactSteps(p *zp.Parser, input string, b zp.Backend, cap uint64) (Cost, error) {
	return strict(steps(p, input, b, cap, 0))
}

// StepsLenient is Steps for an input that the grammar reports syntax errors for, such as one
// that exercises error recovery: the cost of a parse that finishes with a syntax error carries
// the error in Cost.ParseErr instead of failing. Only a *zp.SyntaxError or zp.SyntaxErrors
// is accepted; any other error is returned.
func StepsLenient(p *zp.Parser, input string, b zp.Backend, cap uint64) (Cost, error) {
	return steps(p, input, b, cap, 256)
}

// isSyntaxError reports whether err is a syntax error of the input or the list of errors that a
// parse recovered from. Any other error is a failure of the measurement.
func isSyntaxError(err error) bool {
	var one *zp.SyntaxError
	var many zp.SyntaxErrors
	return errors.As(err, &one) || errors.As(err, &many)
}

func strict(c Cost, err error) (Cost, error) {
	if err == nil && c.ParseErr != nil {
		return Cost{}, c.ParseErr
	}
	return c, err
}

func steps(p *zp.Parser, input string, b zp.Backend, cap, tolDiv uint64) (Cost, error) {
	ok, parseErr := parses(p, input, b, cap)
	if !ok {
		return Cost{Steps: cap, Capped: true}, nil
	}
	if parseErr != nil && !isSyntaxError(parseErr) {
		return Cost{}, parseErr
	}
	lo, hi := uint64(0), cap // budget lo fails (or is zero), budget hi finishes
	for step := uint64(1024); step < hi; step *= 4 {
		if ok, _ := parses(p, input, b, step); ok {
			hi = step
			break
		}
		lo = step
	}
	for hi-lo > 1 && (tolDiv == 0 || hi-lo > hi/tolDiv) {
		mid := lo + (hi-lo)/2
		if ok, _ := parses(p, input, b, mid); ok {
			hi = mid
		} else {
			lo = mid
		}
	}
	return Cost{Steps: hi, ParseErr: parseErr}, nil
}

// StepsTraced returns the spend of a traced parse, read from the counter at the last event. It
// stops at cap like Steps. A traced parse spends more than an untraced one; see the tests.
func StepsTraced(p *zp.Parser, input string, b zp.Backend, cap uint64) (Cost, error) {
	var used uint64
	seen := false
	_, err := p.Parse(input, zp.WithBackend(b), zp.WithLimits(zp.Limits{Budget: cap}),
		zp.WithTrace(func(e zp.TraceEvent) {
			if c := core.TraceParser(e).Control; c != nil {
				used, seen = c.Used, true
			}
		}))
	var le *zp.LimitError
	if errors.As(err, &le) && le.Kind == zp.LimitBudget {
		return Cost{Steps: cap, Capped: true}, nil
	}
	if err != nil {
		return Cost{}, err
	}
	if !seen {
		return Cost{}, errors.New("complexitytest: the parse reported no counter")
	}
	return Cost{Steps: used}, nil
}

// Series is the cost at doubling input sizes: Sizes[i] = Sizes[0] * 2^i.
type Series struct {
	Sizes []int
	Costs []Cost
}

// Measure builds the input of every size with build, checks that it parses, and measures the
// cost with the untraced Steps. The sizes are n, 2n and 4n for the three-point series of the
// suite; any number of doubling sizes is accepted.
func Measure(p *zp.Parser, build func(n int) string, sizes []int, b zp.Backend, cap uint64) (Series, error) {
	s := Series{Sizes: sizes}
	for _, n := range sizes {
		c, err := Steps(p, build(n), b, cap)
		if err != nil {
			return s, fmt.Errorf("size %d: %w", n, err)
		}
		s.Costs = append(s.Costs, c)
	}
	return s, nil
}

// Slopes returns the growth exponents log2(cost(2n)/cost(n)) between consecutive sizes. A
// capped cost is taken as its cap, so a slope that ends in a capped point is a lower bound.
func (s Series) Slopes() []float64 {
	var out []float64
	for i := 1; i < len(s.Costs); i++ {
		out = append(out, math.Log2(float64(s.Costs[i].Steps)/float64(max(s.Costs[i-1].Steps, 1))))
	}
	return out
}

// CappedPoints reports whether any cost of s is capped.
func (s Series) CappedPoints() bool {
	for _, c := range s.Costs {
		if c.Capped {
			return true
		}
	}
	return false
}

// Order is a growth class of the cost.
type Order struct {
	// Kind is KindLinear, KindPoly or KindExp.
	Kind OrderKind
	// Degree is k of a KindPoly order.
	Degree int
}

// OrderKind is the kind of an Order.
type OrderKind int

const (
	KindUnknown OrderKind = iota
	KindLinear
	KindPoly
	KindExp
)

// Linear, Exp and Unknown are the orders without a degree.
var (
	Linear  = Order{Kind: KindLinear}
	Exp     = Order{Kind: KindExp}
	Unknown = Order{}
)

// Poly returns the polynomial order of degree k.
func Poly(k int) Order { return Order{Kind: KindPoly, Degree: k} }

func (o Order) String() string {
	switch o.Kind {
	case KindLinear:
		return "Linear"
	case KindPoly:
		return fmt.Sprintf("Poly(%d)", o.Degree)
	case KindExp:
		return "Exp"
	}
	return "Unknown"
}

// Bands of Classify.
const (
	linearMax    = 1.15 // slopes of a linear series stay at or below this
	polyBelow    = 0.35 // Poly(k) accepts s2 from k-polyBelow
	polyAbove    = 0.20 // to k+polyAbove
	expMinSlope  = 4.0  // Exp needs s2 at least this and s1+1
	maxPolyKnown = 6
)

// Classify returns the order of a series of at least three doubling sizes, from its last two
// slopes s1 (between the first two sizes) and s2 (between the last two): Linear if s1 and s2
// are at most 1.15; Poly(k) if k-0.35 <= s2 <= k+0.2; Exp if s2 >= max(4, s1+1). A capped last
// point counts as a cost of at least the cap, so its slope is a lower bound and only Exp can
// follow from it. A series that fits no band is Unknown.
func Classify(s Series) Order {
	sl := s.Slopes()
	if len(sl) < 2 {
		return Unknown
	}
	s1, s2 := sl[len(sl)-2], sl[len(sl)-1]
	last := s.Costs[len(s.Costs)-1]
	if last.Capped {
		if s2 >= math.Max(expMinSlope, s1+1) {
			return Exp
		}
		return Unknown
	}
	if s.CappedPoints() {
		return Unknown // a capped earlier point makes the slopes lower bounds
	}
	if s1 <= linearMax && s2 <= linearMax {
		return Linear
	}
	if s2 >= math.Max(expMinSlope, s1+1) {
		return Exp
	}
	for k := 2; k <= maxPolyKnown; k++ {
		if float64(k)-polyBelow <= s2 && s2 <= float64(k)+polyAbove {
			return Poly(k)
		}
	}
	return Unknown
}

// rep repeats s n times.
func rep(s string, n int) string { return strings.Repeat(s, n) }
