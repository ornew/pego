package exec

// Walk calls stmt with each statement of b, its nested blocks included, before their bodies, and
// expr with each expression, its subexpressions included, before them. Either may be nil.
func Walk(b *Block, stmt func(Stmt), expr func(Expr)) {
	var x func(Expr)
	x = func(e Expr) {
		if e == nil {
			return
		}
		if expr != nil {
			expr(e)
		}
		switch e := e.(type) {
		case *Unary:
			x(e.X)
		case *Binary:
			x(e.X)
			x(e.Y)
		case *Convert:
			x(e.X)
		case *Call:
			for _, a := range e.Args {
				x(a)
			}
		}
	}
	var set func(*Set)
	set = func(s *Set) {
		if s == nil {
			return
		}
		if stmt != nil {
			stmt(s)
		}
		for _, v := range s.Values {
			x(v)
		}
	}
	var blk func(*Block)
	blk = func(b *Block) {
		for _, st := range b.Stmts {
			if _, ok := st.(*Set); ok {
				set(st.(*Set))
				continue
			}
			if stmt != nil {
				stmt(st)
			}
			switch st := st.(type) {
			case *Block:
				blk(st)
			case *Loop:
				set(st.Init)
				x(st.Cond)
				set(st.Post)
				blk(st.Body)
			case *If:
				set(st.Init)
				x(st.Cond)
				blk(st.Then)
				if st.Else != nil {
					blk(st.Else)
				}
			case *Let:
				x(st.Value)
			case *Do:
				x(st.Call)
			case *Return:
				for _, v := range st.Values {
					x(v)
				}
			case *Assert:
				x(st.Cond)
			}
		}
	}
	blk(b)
}
