package flow

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/core"
)

// valueFree makes a module of one value-free rule with the body, as the builder does: the
// expectations of its literals and classes are interned after the five fixed ones, and every node
// charges one step.
func valueFree(body core.Node) (*core.Module, *core.Rule) {
	m := &core.Module{Exps: []string{"any character", "beginning of input", "end of input", "beginning of line", "end of line"}}
	core.Walk(body, func(n core.Node) {
		n.Attrs().Steps = 1
		switch n := n.(type) {
		case *core.Lit:
			n.Exp = core.ExpID(len(m.Exps))
			m.Exps = append(m.Exps, `"`+n.Text+`"`)
		case *core.ClassNode:
			n.Class.Exp = core.ExpID(len(m.Exps))
			m.Exps = append(m.Exps, "class")
			m.Classes = append(m.Classes, n.Class)
		}
	})
	r := &core.Rule{ID: 0, Name: "r", Body: body, Policy: core.Policy{Seen: -1}}
	m.Rules = []*core.Rule{r}
	core.CallGraph(m)
	core.Analyze(m)
	return m, r
}

func alts(bodies ...core.Node) *core.Choice {
	c := &core.Choice{}
	for _, b := range bodies {
		c.Alts = append(c.Alts, &core.Alt{Body: b})
	}
	return c
}

// TestLower checks the functions lowered from small rules: their statements and the variables
// they declare, and that they verify.
func TestLower(t *testing.T) {
	for _, c := range []struct {
		name string
		body core.Node
		opts Options
		want string
	}{
		{"choice of literals", alts(&core.Lit{Text: "ab"}, &core.Lit{Text: "c"}), Options{InlineLiterals: 1}, `
func r
  var x1 int
  var ok bool
  x1 = pos
  block L2 {
    block L3 {
      if _, ok = matchlit("ab"); !ok {
        exit L3
      }
      exit L2
    }
    pos = x1
    block L4 {
      if haslit("c") {
        advance 1
      } else {
        if _, ok = matchlit("c"); !ok {
          exit L4
        }
      }
      exit L2
    }
    pos = x1
    exit fail
  }
  v = nil
`},
		{"repetition with work and recovery", &core.Repeat{Min: 1, Max: -1, Body: &core.Lit{Text: "a"}}, Options{Work: true, Recovers: true}, `
func r
  var x1 int
  var x2 int
  var x3 int
  var ok bool
  step
  x1 = 0
  loop loop1 {
    x2, x3 = pos, reclen
    block L4 {
      step
      if _, ok = matchlit("a"); !ok {
        exit L4
      }
      x1++
      if pos == x2 {
        exit loop1
      }
      continue loop1
    }
    restore x2, x3
    exit loop1
  }
  if x1 < 1 {
    exit fail
  }
  v = nil
`},
		{"alternatives after one that cannot fail", alts(&core.Lit{Text: "a"}, &core.Succeed{}, &core.Lit{Text: "b"}), Options{}, `
func r
  var x1 int
  var ok bool
  x1 = pos
  block L2 {
    block L3 {
      if _, ok = matchlit("a"); !ok {
        exit L3
      }
      exit L2
    }
    pos = x1
    block L4 {
      exit L2
    }
    pos = x1
    exit fail
  }
  v = nil
`},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, r := valueFree(c.body)
			f, err := Lower(m, r, "r", c.opts)
			if err != nil {
				t.Fatal(err)
			}
			if err := Verify(f); err != nil {
				t.Fatal(err)
			}
			if got := String(f); got != strings.TrimPrefix(c.want, "\n") {
				t.Errorf("got\n%s\nwant\n%s", got, strings.TrimPrefix(c.want, "\n"))
			}
		})
	}
}

// TestLowerFrame checks the framed functions lowered from small bodies.
func TestLowerFrame(t *testing.T) {
	for _, c := range []struct {
		name  string
		body  core.Node
		slots []core.Slot
		want  string
	}{
		{"recovery of a repetition with captures", &core.Recover{
			Body: &core.Repeat{Min: 1, Max: -1, Scope: []core.Slot{{Name: "x"}},
				Body: &core.Capture{Slot: 0, Name: "x", Body: &core.Lit{Text: "a"}}},
			Skip: &core.Any{}}, nil, `
func r framed
  var x1 mark
  var x2 expmark
  var x3 int
  var x4 expids
  var x7 frame
  var x8 frame
  var x9 int
  var x10 mark
  var x12 int
  var x13 int
  var ok bool
  var v14 value
  var v15 value
  var size int
  var x17 error
  // Recovery inlined under the runtime-owned frame.
  x1 = mark
  x2 = isolate
  block L6 {
    block L5 {
      x9 = 0
      loop loop1 {
        x10 = mark
        x8 = frame
        x7 = newframe([x])
        frame = x7
        block L11 {
          x12 = pos
          x13 = pos
          if _, ok = matchlit("a"); !ok {
            exit L11
          }
          v14 = match(x13, "a")
          v15 = capture(v14, x12)
          setcapture 0, v15
          frame = x8
          names [x]
          droptrail x10
          free x7
          x9++
          if pos == x10.pos {
            exit loop1
          }
          continue loop1
        }
        frame = x8
        reset x10
        exit loop1
      }
      if x9 < 1 {
        exit L5
      }
      x3, x4 = unisolate(x2)
      merge x4 at x3
      exit L6
    }
    x3, x4 = unisolate(x2)
    x4 = keep(x4)
    reset x1
    block L16 {
      peek _, size, ok
      if !ok {
        expect #0 at pos
        exit L16
      }
      advance size
      if pos == x1.pos {
        exit L16
      }
      x17 = error(x3, x4)
      recovered x17
      exit L6
    }
    reset x1
    merge x4 at x3
    exit fail
  }
  return nil
`},
		{"cut at the top", &core.Seq{Items: []core.Node{&core.Capture{Slot: 0, Name: "y", Body: &core.Lit{Text: "a"}}, &core.Cut{}}},
			[]core.Slot{{Name: "y"}}, `
func r framed
  var cut1 bool
  var x2 int
  var x3 int
  var ok bool
  var v4 value
  var v5 value
  cut1 = cut
  x2 = pos
  x3 = pos
  if _, ok = matchlit("a"); !ok {
    exit fail
  }
  v4 = match(x3, "a")
  v5 = capture(v4, x2)
  setcapture 0, v5
  cut1 = true
  cut = cut1
  return nil
`},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, r := valueFree(c.body)
			f, err := LowerFrame(m, r, r.Body, c.slots, false, "r", Options{})
			if err != nil {
				t.Fatal(err)
			}
			if err := Verify(f); err != nil {
				t.Fatal(err)
			}
			if got := String(f); got != strings.TrimPrefix(c.want, "\n") {
				t.Errorf("got\n%s\nwant\n%s", got, strings.TrimPrefix(c.want, "\n"))
			}
		})
	}
}

// TestVerifyRejects checks that Verify rejects functions that break each invariant it checks.
func TestVerifyRejects(t *testing.T) {
	newFunc := func(body ...Stmt) *Func {
		return &Func{Name: "f", Body: body, Fail: &Label{Name: "fail"},
			Start: &Var{Name: "start", Type: TypeInt, Entry: true}, Result: &Var{Name: "v", Type: TypeValue, Entry: true},
			Ctx: &Var{Name: "c", Type: TypeCtx, Entry: true}}
	}
	inner, outer := &Label{Name: "L1"}, &Label{Name: "L2"}
	stray := &Var{Name: "x9", Type: TypeInt}
	for _, c := range []struct {
		name string
		f    *Func
		want string
	}{
		{"exit to a block that does not enclose it", newFunc(&Block{Label: inner}, &Exit{To: inner}), "does not enclose"},
		{"continue outside its loop", newFunc(&Block{Label: outer, Body: []Stmt{&Continue{To: outer}}}), "continue"},
		{"undeclared variable", newFunc(&Set{Dst: []LValue{RegPos}, X: []Expr{&Use{V: stray}}}), "not declared"},
		{"assignment arity", newFunc(&Set{Dst: []LValue{RegPos, &Discard{}}, X: []Expr{&Int{N: 1}}}), "1 values to 2 places"},
		{"label used twice", newFunc(&Block{Label: inner}, &Block{Label: inner}), "label of its own"},
		{"unbounded loop that never exits", newFunc(&Loop{Label: &Label{}, Body: []Stmt{&Step{}}}), "never exits"},
		{"framed function with entry variables", func() *Func { f := newFunc(); f.Frame = true; return f }(), "entry variable"},
		{"local function with a value", func() *Func { f := newFunc(); f.Value = &Nil{}; return f }(), "local function"},
		{"undeclared cut flag", func() *Func {
			return &Func{Name: "f", Fail: &Label{Name: "fail"}, Frame: true, Cut: &Var{Name: "cut1", Type: TypeBool}}
		}(), "not declared"},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := Verify(c.f)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("got %v, want an error containing %q", err, c.want)
			}
		})
	}
	// A valid function of the same parts verifies.
	ok := newFunc(&Block{Label: inner, Body: []Stmt{&Set{Dst: []LValue{RegPos}, X: []Expr{&Int{N: 1}}}, &Exit{To: inner}}},
		&Loop{Label: outer, Body: []Stmt{&Step{}, &Continue{To: outer}, &Exit{To: outer}}})
	ok.Body = append(ok.Body, &Exit{To: ok.Fail})
	if err := Verify(ok); err != nil {
		t.Errorf("a valid function: %v", err)
	}
}
