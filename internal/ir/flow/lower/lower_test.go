package lower

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/ir/core"
	"github.com/ornew/pego/internal/ir/flow"
)

// valueFree makes a module of one value-free rule with the body, as the builder does: the
// expectations of its literals and classes are interned after the five fixed ones.
func valueFree(body core.Node) (*core.Module, *core.Rule) {
	m := &core.Module{Exps: []string{"any character", "beginning of input", "end of input", "beginning of line", "end of line"}}
	core.Walk(body, func(n core.Node) {
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
		{"repetition with work and recovery", &core.Repeat{Min: 1, Max: -1, Body: &core.Lit{Text: "a"}}, Options{Budget: true, Recovers: true}, `
func r
  var x1 int
  var x2 int
  var x3 int
  var ok bool
  x1 = 0
  loop loop1 {
    step
    x2, x3 = pos, reclen
    block L4 {
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
			if err := flow.Verify(f); err != nil {
				t.Fatal(err)
			}
			if got := flow.String(f); got != strings.TrimPrefix(c.want, "\n") {
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
			if err := flow.Verify(f); err != nil {
				t.Fatal(err)
			}
			if got := flow.String(f); got != strings.TrimPrefix(c.want, "\n") {
				t.Errorf("got\n%s\nwant\n%s", got, strings.TrimPrefix(c.want, "\n"))
			}
		})
	}
}

// TestLowerNodeValues checks that a Node lowering builds the Node runtime's values, in a function that
// Verify accepts, and a typed lowering builds the typed ones.
func TestLowerNodeValues(t *testing.T) {
	for _, node := range []bool{false, true} {
		m, r := valueFree(&core.Repeat{Min: 1, Max: -1, Body: &core.Lit{Text: "a"}})
		f, err := LowerFrame(m, r, r.Body, nil, true, "r", Options{Node: node})
		if err != nil {
			t.Fatal(err)
		}
		if err := flow.Verify(f); err != nil {
			t.Fatalf("node %v: %v", node, err)
		}
		got := flow.String(f)
		hasNode := strings.Contains(got, "branchlist(") || strings.Contains(got, "terminal(")
		hasTyped := strings.Contains(got, "list(") && !strings.Contains(got, "branchlist(") || strings.Contains(got, "match(") && !strings.Contains(got, "terminal(")
		if f.Node != node || hasNode != node || hasTyped == node {
			t.Errorf("node %v: Func.Node %v, Node operations %v, typed operations %v in\n%s", node, f.Node, hasNode, hasTyped, got)
		}
	}
}

// TestLowerVerifies checks that lowering returns an error, rather than a function, when Verify rejects
// the function it built: a Node function that holds a typed value operation is not printed.
func TestLowerVerifies(t *testing.T) {
	newFunc := func(body ...flow.Stmt) *flow.Func {
		return &flow.Func{Name: "f", Body: body, Fail: &flow.Label{Name: "fail"},
			Start: &flow.Var{Name: "start", Type: flow.TypeInt, Entry: true}, Result: &flow.Var{Name: "v", Type: flow.TypeValue, Entry: true},
			Ctx: &flow.Var{Name: "c", Type: flow.TypeCtx, Entry: true}}
	}
	f := newFunc(&flow.Set{Dst: []flow.LValue{flow.RegPos}, X: []flow.Expr{&flow.NewMatch{Start: &flow.Int{}, Text: &flow.Str{}}}})
	f.Node = true
	if g, err := verified(f); err == nil || g != nil || !strings.Contains(err.Error(), "typed match in a Node function") {
		t.Errorf("verified: got %v, %v", g, err)
	}
	good := newFunc()
	if g, err := verified(good); err != nil || g != good {
		t.Errorf("verified of a valid function: got %v, %v", g, err)
	}
}

// TestLowerStream checks the lowering of a #stream repetition in a framed Node function: with Options.Stream
// it starts the stream and delivers each element instead of gathering it, guarded by StreamOn; without the
// option it is the batch repetition, which has none of these operations.
func TestLowerStream(t *testing.T) {
	m := &core.Module{Rules: []*core.Rule{nil}}
	rep := &core.Repeat{Min: 0, Max: -1, Body: &core.Lit{Text: "a"}}
	body := &core.Stream{Repeat: rep}
	r := &core.Rule{Name: "main", Body: body}
	m.Rules[0] = r
	lower := func(stream bool) string {
		f, err := LowerFrame(m, r, body, nil, true, "main", Options{Node: true, Stream: stream})
		if err != nil {
			t.Fatal(err)
		}
		if err := flow.Verify(f); err != nil {
			t.Fatal(err)
		}
		return flow.String(f)
	}
	batch, stream := lower(false), lower(true)
	for _, op := range []string{"stream start", "emit ", "streaming"} {
		if strings.Contains(batch, op) {
			t.Errorf("the batch lowering has %q:\n%s", op, batch)
		}
		if !strings.Contains(stream, op) {
			t.Errorf("the stream lowering lacks %q:\n%s", op, stream)
		}
	}
	if strings.Count(stream, "push ") != strings.Count(batch, "push ") {
		t.Errorf("the stream lowering keeps the gathering of elements for the parses that do not stream:\n%s", stream)
	}
}

// A Document parse records how far it examined the input in the primitives that read it, so a Document function
// reads a literal in place only where the primitive follows as the alternative, and Verify rejects the other
// forms: an in-place read that is part of a larger condition or has no primitive to fall back to.
func TestVerifyDocumentReads(t *testing.T) {
	lit := &core.Lit{Text: "ab"}
	m, r := valueFree(lit)
	build := func(document bool) *flow.Func {
		f, err := Lower(m, r, "r", Options{InlineLiterals: 4, Document: document})
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	f := build(true)
	if err := flow.Verify(f); err != nil {
		t.Fatalf("a Document function with the primitive as the alternative of its in-place read: %v", err)
	}
	// find returns the If whose condition is the in-place read.
	var find func(ss []flow.Stmt) *flow.If
	find = func(ss []flow.Stmt) *flow.If {
		for _, s := range ss {
			switch s := s.(type) {
			case *flow.If:
				if _, ok := s.Cond.(*flow.HasLit); ok {
					return s
				}
				if x := find(s.Then); x != nil {
					return x
				}
				if x := find(s.Else); x != nil {
					return x
				}
			case *flow.Block:
				if x := find(s.Body); x != nil {
					return x
				}
			case *flow.Loop:
				if x := find(s.Body); x != nil {
					return x
				}
			}
		}
		return nil
	}
	read := find(f.Body)
	if read == nil {
		t.Fatalf("no in-place read in:\n%s", flow.String(f))
	}
	saved := *read
	check := func(name, want string) {
		t.Run(name, func(t *testing.T) {
			err := flow.Verify(f)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("got %v, want an error containing %q", err, want)
			}
		})
		*read = saved
	}
	read.Else = nil
	check("no primitive", "without the recording primitive")
	read.Cond = &flow.Or{X: saved.Cond, Y: &flow.Not{X: saved.Cond}}
	check("larger condition", "part of a larger condition")
	if err := flow.Verify(f); err != nil {
		t.Fatalf("restored: %v", err)
	}
	// Outside a Document function the same read stands alone.
	g := build(false)
	if read := find(g.Body); read == nil {
		t.Fatalf("no in-place read in:\n%s", flow.String(g))
	} else {
		read.Else = nil
		if err := flow.Verify(g); err != nil && strings.Contains(err.Error(), "recording primitive") {
			t.Errorf("a function that is not a Document function: %v", err)
		}
	}
}
