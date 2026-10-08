package cel

import "fmt"

// CheckError reports what the grammar cannot: a literal out of range, an expression over a limit, or a macro call
// with arguments that the macro does not take.
type CheckError struct {
	Span          // the expression or literal
	Line, Col int // 1-based position of the start, in code points; 0 unless ParseExpr set them
	Message   string
}

func (e *CheckError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%d:%d: %s", e.Line, e.Col, e.Message)
	}
	return fmt.Sprintf("offset %d: %s", e.Start, e.Message)
}

// An Option changes what Check and ParseExpr check.
type Option func(*checkOptions)

type checkOptions struct {
	limits Limits
	macros bool
}

// WithLimits checks against l instead of DefaultLimits. A limit of zero or less is not checked: WithLimits(Limits{})
// turns the checks of size and depth off.
func WithLimits(l Limits) Option { return func(o *checkOptions) { o.limits = l } }

// WithMacros also checks the calls of the standard macros as cel-go's parser does when it expands them (see
// CheckMacros).
func WithMacros() Option { return func(o *checkOptions) { o.macros = true } }

func newCheckOptions(opts []Option) checkOptions {
	o := checkOptions{limits: DefaultLimits}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// Check reports what the grammar does not: a literal out of range (an int or uint outside the range of int64 or
// uint64, a double outside the range of float64), an expression deeper than the limit (CheckLimits), and with
// WithMacros a call of a standard macro with arguments it does not take. It returns a *CheckError.
func Check(e Expr, opts ...Option) error {
	o := newCheckOptions(opts)
	var err error
	Inspect(e, func(e Expr) bool {
		if err != nil {
			return false
		}
		var verr error
		switch e := e.(type) {
		case *IntLit:
			_, verr = e.Value()
		case *UintLit:
			_, verr = e.Value()
		case *DoubleLit:
			_, verr = e.Value()
		default:
			return true
		}
		if verr != nil {
			err = &CheckError{Span: SpanOf(e), Message: verr.Error()}
		}
		return true
	})
	if err != nil {
		return err
	}
	if o.macros {
		if err := checkMacros(e); err != nil {
			return err
		}
	}
	return CheckLimits(e, o.limits)
}

// CheckMacros reports a call of a standard macro whose arguments are not what the macro takes, as the parser of cel-go
// rejects it when it has the standard macros: has() of something that is not the selection of a field, and the first
// argument of all, exists, exists_one, existsOne, map and filter that is not a name (or is __result__, the name of the
// accumulator).
func CheckMacros(e Expr) error { return checkMacros(e) }

// ParseExpr parses an expression and checks it: ParseAST followed by Check, with a check of the size of the input
// against the limit first. It returns a *SyntaxError for input that is not an expression and a *CheckError for the rest.
func ParseExpr(input string, opts ...Option) (Expr, error) {
	o := newCheckOptions(opts)
	if err := checkSize(input, o.limits); err != nil {
		return nil, err
	}
	e, err := ParseAST(input)
	if err != nil {
		return nil, err
	}
	if err := Check(e, opts...); err != nil {
		ce := err.(*CheckError)
		ce.Line, ce.Col = lineCol(input, ce.Start)
		return nil, ce
	}
	return e, nil
}

// Valid reports whether input is an expression that ParseExpr accepts.
func Valid(input string, opts ...Option) bool {
	_, err := ParseExpr(input, opts...)
	return err == nil
}

// lineCol returns the 1-based line and column of the code point at pos in input.
func lineCol(input string, pos int) (line, col int) {
	line, col = 1, 1
	i := 0
	for _, r := range input {
		if i >= pos {
			break
		}
		if r == '\n' {
			line, col = line+1, 1
		} else {
			col++
		}
		i++
	}
	return line, col
}
