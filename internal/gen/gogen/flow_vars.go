package gogen

import (
	"fmt"
	goast "go/ast"
	goparser "go/parser"
	"go/scanner"
	"go/token"
	"strconv"
	"strings"

	"github.com/ornew/pego/internal/ir/flow"
)

// Variables a printed function never reads.
//
// Flow IR assigns variables that no printed statement reads: a position saved for a restore that
// no path takes, a captured value that no capture slot keeps. The printer does not write them.
// A statement that only assigns variables nobody reads, with values that have no effect, is left out,
// the unread variable is not declared, and an assignment of several values with an effect keeps its
// values and writes the blank identifier for the unread places. Leaving a statement out leaves the
// variables its values read unread, so the removal repeats until no more variables become unread.
//
// A variable assigned where a statement cannot be left out (the initialization statement of an if) or
// opposite an untyped nil in an assignment of several values (which cannot be written to the blank
// identifier) stays declared and, if nothing reads it, is silenced with `_ = x`.
//
// The statements that are removable are formed while the function is printed and replaced by their
// text, or by nothing, when its variables are known (FlowVars.Resolve).

// FlowVarsMutant, set by a test, makes every variable of the function unread: the generated code must then fail
// to compile, which shows that the tests of generated code catch a wrong decision.
var FlowVarsMutant bool

// FlowVarsMutantUses, set by a test, ignores the reads of the values of assignments that stay, which
// leaves variables that only they read undeclared: the generated code must then fail to compile.
var FlowVarsMutantUses bool

// FlowVars collects the assignments of a printed function and the reads that decide which variables
// it keeps.
type FlowVars struct {
	// Hard holds the variables read by statements that stay whatever is removed.
	Hard map[*flow.Var]bool
	// sink, while an assignment is formed, takes the reads of its values.
	sink map[*flow.Var]bool
	// Stuck holds the variables assigned where no statement can be left out, and Pinned those
	// assigned opposite an untyped nil.
	Stuck, Pinned map[*flow.Var]bool
	stmts         []*VarStmt
}

// VarStmt is an assignment or increment that is left out if its variables are unread.
type VarStmt struct {
	// Targets are the variables it writes, nil for a place that is not a variable. Only a variable of
	// the function (see FlowVars.Resolve) can be unread.
	Targets []*flow.Var
	Uses    map[*flow.Var]bool
	// Pure reports that evaluating the values has no effect and cannot panic, which allows leaving
	// the statement out. A statement that is not pure is kept with the blank identifier for each
	// unread target.
	Pure bool
	// Text returns the statement, with the blank identifier for the variables that dead reports.
	Text func(dead func(*flow.Var) bool) string
}

func NewFlowVars() FlowVars {
	return FlowVars{Hard: map[*flow.Var]bool{}, Stuck: map[*flow.Var]bool{}, Pinned: map[*flow.Var]bool{}}
}

// read records that the variable v is read.
func (fv *FlowVars) read(v *flow.Var) {
	if fv.sink != nil {
		fv.sink[v] = true
		return
	}
	fv.Hard[v] = true
}

// marker is the text that stands for the statement i until the variables are known.
func varMarker(i int) string { return "\x00" + strconv.Itoa(i) + "\x00" }

// Add registers the removable statement st, formed with its reads in st.uses, and returns its marker.
func (fv *FlowVars) Add(st *VarStmt) string {
	fv.stmts = append(fv.stmts, st)
	return varMarker(len(fv.stmts) - 1)
}

// begin starts collecting the reads of the values of an assignment into the statement's uses.
func (fv *FlowVars) begin() map[*flow.Var]bool {
	fv.sink = map[*flow.Var]bool{}
	return fv.sink
}

func (fv *FlowVars) end() { fv.sink = nil }

// pureValues reports whether none of the printed values has an effect or can panic.
func pureValues(xs []string) bool {
	for _, x := range xs {
		e, err := goparser.ParseExpr(x)
		if err != nil || !pure(e) {
			return false
		}
	}
	return true
}

// flowVarsResult is the outcome of resolve.
type flowVarsResult struct {
	// Dead reports that the variable is not declared: nothing reads it and nothing forces it to stay.
	Dead func(*flow.Var) bool
	// Read reports that a statement that stays reads the variable.
	Read func(*flow.Var) bool
	// Body is the printed body with the removable statements settled.
	Body string
	// Err reports an internal error: the body names a variable that is not declared.
	Err error
}

// Resolve decides which of the variables vars are unread, in the printed body, and settles the
// removable statements in it.
func (fv *FlowVars) Resolve(vars []*flow.Var, body string) flowVarsResult {
	cand := map[*flow.Var]bool{}
	for _, v := range vars {
		cand[v] = true
	}
	// Start with every read kept, as removing statements one at a time does, and remove what
	// becomes unread until nothing changes.
	reads := map[*flow.Var]bool{}
	for v := range fv.Hard {
		reads[v] = true
	}
	for _, st := range fv.stmts {
		for v := range st.Uses {
			reads[v] = !FlowVarsMutantUses
		}
	}
	isDead := func(v *flow.Var) bool {
		if FlowVarsMutant {
			return v != nil && cand[v]
		}
		return v != nil && cand[v] && !reads[v] && !fv.Stuck[v] && !fv.Pinned[v]
	}
	cut := func(st *VarStmt) bool {
		if !st.Pure {
			return false
		}
		for _, t := range st.Targets {
			if !isDead(t) {
				return false
			}
		}
		return true
	}
	for {
		next := map[*flow.Var]bool{}
		for v := range fv.Hard {
			next[v] = true
		}
		for _, st := range fv.stmts {
			if !cut(st) && !FlowVarsMutantUses {
				for v := range st.Uses {
					next[v] = true
				}
			}
		}
		if len(next) == len(reads) {
			break
		}
		reads = next
	}
	var b strings.Builder
	rest := body
	for {
		i := strings.IndexByte(rest, 0)
		if i < 0 {
			break
		}
		j := strings.IndexByte(rest[i+1:], 0) + i + 1
		n, err := strconv.Atoi(rest[i+1 : j])
		if err != nil {
			panic(fmt.Sprintf("flow printer: statement marker %q", rest[i+1:j]))
		}
		b.WriteString(rest[:i])
		if st := fv.stmts[n]; !cut(st) {
			b.WriteString(st.Text(isDead))
		}
		rest = rest[j+1:]
	}
	b.WriteString(rest)
	var err error
	if !FlowVarsMutant && !FlowVarsMutantUses {
		// A variable that is not declared must not be named: the name would bind to a package-level
		// function or variable of the same name.
		dead := map[string]bool{}
		for _, v := range vars {
			if isDead(v) {
				dead[v.Name] = true
			}
		}
		if len(dead) > 0 {
			for _, id := range FreeIdents(b.String()) {
				if dead[id] {
					err = fmt.Errorf("internal error: the printed body names the variable %s, which is not declared", id)
					break
				}
			}
		}
	}
	return flowVarsResult{Dead: isDead, Read: func(v *flow.Var) bool { return reads[v] }, Body: b.String(), Err: err}
}

// FreeIdents returns the identifiers of the Go statements src that are not selected from a value.
func FreeIdents(src string) []string {
	var sc scanner.Scanner
	fset := token.NewFileSet()
	sc.Init(fset.AddFile("", fset.Base(), len(src)), []byte(src), func(token.Position, string) {}, 0)
	var ids []string
	prev := token.ILLEGAL
	for {
		_, tok, lit := sc.Scan()
		if tok == token.EOF {
			return ids
		}
		if tok == token.IDENT && prev != token.PERIOD {
			ids = append(ids, lit)
		}
		prev = tok
	}
}

// pure reports whether evaluating the expression has no effect and cannot panic (a selector of a receiver
// that is not nil is taken to be such).
func pure(e goast.Expr) bool {
	switch e := e.(type) {
	case *goast.Ident, *goast.BasicLit:
		return true
	case *goast.SelectorExpr:
		return pure(e.X)
	case *goast.ParenExpr:
		return pure(e.X)
	case *goast.UnaryExpr:
		return (e.Op == token.SUB || e.Op == token.ADD || e.Op == token.NOT || e.Op == token.XOR) && pure(e.X)
	case *goast.BinaryExpr:
		// Comparing interfaces holding values that cannot be compared panics, shifting by a
		// negative count panics and dividing by zero panics.
		switch e.Op {
		case token.ADD, token.SUB, token.MUL, token.AND, token.OR, token.XOR, token.AND_NOT, token.LAND, token.LOR, token.LSS, token.GTR, token.LEQ, token.GEQ:
			return pure(e.X) && pure(e.Y)
		}
	}
	return false
}
