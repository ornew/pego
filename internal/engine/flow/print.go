package flow

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ornew/pego/internal/engine/core"
)

// String returns a readable listing of the function f, for tests and debugging: its variables,
// then its statements, one per line, indented by block. Loops without a name are numbered in the
// order they occur.
func String(f *Func) string {
	p := &printer{loops: map[*Label]string{}}
	fmt.Fprintf(&p.b, "func %s", f.Name)
	if f.Frame {
		p.b.WriteString(" framed")
	}
	if f.Binds {
		p.b.WriteString(" binds")
	}
	p.b.WriteString("\n")
	for _, v := range f.Vars {
		fmt.Fprintf(&p.b, "  var %s %s\n", v.Name, v.Type)
	}
	p.stmts(f.Body, 1)
	if f.Cut != nil {
		p.line(1, "cut = %s", f.Cut.Name)
	}
	if f.Value != nil {
		p.line(1, "return %s", p.expr(f.Value))
	}
	return p.b.String()
}

type printer struct {
	b     strings.Builder
	loops map[*Label]string
}

func (p *printer) line(depth int, format string, args ...any) {
	p.b.WriteString(strings.Repeat("  ", depth))
	fmt.Fprintf(&p.b, format+"\n", args...)
}

func (p *printer) name(l *Label) string {
	if l.Name != "" {
		return l.Name
	}
	if n, ok := p.loops[l]; ok {
		return n
	}
	n := fmt.Sprintf("loop%d", len(p.loops)+1)
	p.loops[l] = n
	return n
}

func (p *printer) stmts(ss []Stmt, d int) {
	for _, s := range ss {
		p.stmt(s, d)
	}
}

func (p *printer) stmt(s Stmt, d int) {
	switch s := s.(type) {
	case *Block:
		p.line(d, "block %s {", p.name(s.Label))
		p.stmts(s.Body, d+1)
		p.line(d, "}")
	case *Loop:
		head := "loop " + p.name(s.Label)
		switch {
		case s.Counter != nil:
			head += fmt.Sprintf(" %s < %d", s.Counter.Name, s.Max)
		case s.While != nil:
			head += " while " + p.expr(s.While)
		}
		p.line(d, "%s {", head)
		p.stmts(s.Body, d+1)
		p.line(d, "}")
	case *If:
		head := "if "
		if s.Init != nil {
			head += p.set(s.Init) + "; "
		}
		p.line(d, "%s%s {", head, p.expr(s.Cond))
		p.stmts(s.Then, d+1)
		if len(s.Else) > 0 {
			p.line(d, "} else {")
			p.stmts(s.Else, d+1)
		}
		p.line(d, "}")
	case *Exit:
		p.line(d, "exit %s", p.name(s.To))
	case *Continue:
		p.line(d, "continue %s", p.name(s.To))
	case *Set:
		p.line(d, "%s", p.set(s))
	case *Inc:
		p.line(d, "%s++", s.V.Name)
	case *Step:
		p.line(d, "step")
	case *Silent:
		p.line(d, "silent %+d", s.Delta)
	case *Peek:
		p.line(d, "peek %s, %s, %s", varName(s.Ch), varName(s.Size), varName(s.OK))
	case *Advance:
		p.line(d, "advance %s", p.expr(s.N))
	case *Expect:
		kind := "expect"
		if s.Message {
			kind = "expect message"
		}
		p.line(d, "%s %s at %s", kind, exp(s.Exp), p.expr(s.At))
	case *Restore:
		p.line(d, "restore %s, %s", p.expr(s.Pos), p.expr(s.Rec))
	case *MergeExpected:
		p.line(d, "merge %s at %s", p.expr(s.Exps), p.expr(s.Far))
	case *MergeRenamed:
		p.line(d, "merge %s at %s as %s", p.expr(s.Exps), p.expr(s.Far), exp(s.Exp))
	case *PushKid:
		p.line(d, "push %s", p.expr(s.X))
	case *DropKids:
		p.line(d, "drop kids %s", p.expr(s.Base))
	case *StreamStart:
		p.line(d, "stream start")
	case *EmitStream:
		p.line(d, "emit %s", p.expr(s.X))
	case *Append:
		spread := ""
		if s.Spread {
			spread = "..."
		}
		p.line(d, "append %s, %s%s", s.Vals.Name, p.expr(s.X), spread)
	case *SetWhere:
		p.line(d, "where %q", s.Name)
	case *DropCreated:
		p.line(d, "drop created %s", p.expr(s.C))
	case *Unfresh:
		p.line(d, "unfresh %s", p.expr(s.X))
	case *Reset:
		p.line(d, "reset %s", p.expr(s.M))
	case *SetCapture:
		p.line(d, "setcapture %d, %s", s.Slot, p.expr(s.X))
	case *ClearVals:
		p.line(d, "clear %s", s.F.Name)
	case *DropTrail:
		p.line(d, "droptrail %s", p.expr(s.M))
	case *FreeFrame:
		p.line(d, "free %s", s.F.Name)
	case *AppendError:
		p.line(d, "recovered %s", p.expr(s.X))
	case *DeclareNames:
		p.line(d, "names %v", s.N.List)
	case *Comment:
		p.line(d, "// %s", s.Text)
	case *ObserveSkip:
		p.line(d, "observe skip")
	default:
		p.line(d, "?%T", s)
	}
}

func varName(v *Var) string {
	if v == nil {
		return "_"
	}
	return v.Name
}

func exp(id core.ExpID) string { return "#" + strconv.Itoa(int(id)) }

func (p *printer) set(s *Set) string {
	dst := make([]string, len(s.Dst))
	for i, d := range s.Dst {
		switch d := d.(type) {
		case *Var:
			dst[i] = d.Name
		case Reg:
			dst[i] = p.expr(&Read{R: d})
		default:
			dst[i] = "_"
		}
	}
	return strings.Join(dst, ", ") + " = " + p.exprs(s.X...)
}

func (p *printer) exprs(xs ...Expr) string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = p.expr(x)
	}
	return strings.Join(out, ", ")
}

func (p *printer) caps(c Captures) string {
	if c.Vars == nil {
		return "[frame]"
	}
	var xs []string
	for i, v := range c.Vars {
		if v != nil {
			xs = append(xs, c.Names[i]+"="+v.Name)
		}
	}
	return "[" + strings.Join(xs, " ") + "]"
}

func (p *printer) expr(x Expr) string {
	switch x := x.(type) {
	case *Use:
		return x.V.Name
	case *Read:
		switch x.R {
		case RegEnv:
			return "env"
		case RegFrame:
			return "frame"
		case RegCut:
			return "cut"
		}
		return "pos"
	case *RecLen:
		return "reclen"
	case *KidsLen:
		return "kidslen"
	case *StreamOn:
		return "streaming"
	case *Nil:
		return "nil"
	case *Int:
		return strconv.Itoa(x.N)
	case *Bool:
		return strconv.FormatBool(x.B)
	case *Not:
		return "!" + p.expr(x.X)
	case *Or:
		return "(" + p.expr(x.X) + " || " + p.expr(x.Y) + ")"
	case *Cmp:
		return p.expr(x.L) + " " + x.Op + " " + p.expr(x.R)
	case *HasLit:
		return fmt.Sprintf("haslit(%q)", x.Lit.Text)
	case *MatchLit:
		return fmt.Sprintf("matchlit(%q)", x.Lit.Text)
	case *MatchClass:
		return fmt.Sprintf("matchclass(%s)", exp(x.Class.Exp))
	case *InClass:
		return fmt.Sprintf("inclass(%s, %s)", exp(x.Class.Exp), x.Ch.Name)
	case *GuardFails:
		return fmt.Sprintf("guardfails(%s, %s, %s, depth %d)", exp(x.Guard.Exp), x.More.Name, x.Ch.Name, x.Guard.Depth)
	case *AtAnchor:
		return fmt.Sprintf("at(%d)", x.Kind)
	case *SkipTerminal:
		return "skipterminal"
	case *SkipClasses:
		return fmt.Sprintf("skipclasses(%d)", len(x.Run.Classes))
	case *CallRule:
		return fmt.Sprintf("call(%d, %d)", x.Rule, x.Level)
	case *Isolate:
		return "isolate"
	case *Unisolate:
		return "unisolate(" + p.expr(x.Mark) + ")"
	case *Text:
		return "text(" + p.expr(x.Start) + ")"
	case *LiteralText:
		return fmt.Sprintf("littext(%s, %q)", p.expr(x.Start), x.Lit)
	case *Str:
		return strconv.Quote(x.S)
	case *NewMatch:
		return "match(" + p.exprs(x.Start, x.Text) + ")"
	case *NewNode:
		kind := "tuple"
		if x.List {
			kind = "list"
		}
		return kind + "(" + p.exprs(x.Start, x.Kids) + ")"
	case *NewTerminalNode:
		return "terminal(" + p.exprs(x.Start, x.Text) + ")"
	case *NewBranchNode:
		kind := "branch"
		if x.List {
			kind = "branchlist"
		}
		return kind + "(" + p.exprs(x.Start, x.Kids) + ")"
	case *NewVals:
		return fmt.Sprintf("vals(%d)", x.N)
	case *Kids:
		return "kids(" + p.expr(x.Base) + ")"
	case *ShapeSingle:
		return "single(" + p.exprs(x.X, x.Start) + ")"
	case *ShapeCapture:
		return "capture(" + p.exprs(x.X, x.Start) + ")"
	case *ScopeValue:
		return "scope(" + p.exprs(x.X, x.Start) + ")"
	case *AttachCaptures:
		vals := make([]string, len(x.Vals))
		for i, v := range x.Vals {
			vals[i] = v.Name
		}
		if x.Frame != nil {
			vals = []string{x.Frame.Name + ".vals"}
		}
		return fmt.Sprintf("attach(%s, %v, [%s], %s)", p.expr(x.X), x.Names.List, strings.Join(vals, " "), p.expr(x.Start))
	case *MarkState:
		return "mark"
	case *MarkPos:
		return x.M.Name + ".pos"
	case *NewFrame:
		return fmt.Sprintf("newframe(%v)", x.N.List)
	case *FrameVal:
		return fmt.Sprintf("%s.vals[%d]", x.F.Name, x.Slot)
	case *Keep:
		return "keep(" + p.expr(x.Exps) + ")"
	case *MakeError:
		return "error(" + p.exprs(x.Far, x.Exps) + ")"
	case *ErrorValue:
		return "errorvalue(" + p.exprs(x.Start, x.X) + ")"
	case *NewErrorNode:
		return "errornode(" + p.exprs(x.Start, x.X) + ")"
	case *FreshNode:
		return "fresh(" + p.expr(x.X) + ")"
	case *ConvertResult:
		if x.Owned != nil {
			return fmt.Sprintf("convert(%d, %s)", x.Rule, p.exprs(x.X, x.Owned))
		}
		return fmt.Sprintf("convert(%d, %s)", x.Rule, p.expr(x.X))
	case *UseCtx:
		return "ctx(" + p.expr(x.Start) + ")"
	case *Pred:
		return "pred" + p.caps(x.Caps)
	case *Bind:
		return fmt.Sprintf("bind(%s)%s", x.Node.Var, p.caps(x.Caps))
	case *Eval:
		return fmt.Sprintf("eval(%s)%s", x.C.Name, p.caps(x.Caps))
	case *CtxFinish:
		return fmt.Sprintf("finish(%s, %s)", x.C.Name, p.expr(x.X))
	}
	return fmt.Sprintf("?%T", x)
}
