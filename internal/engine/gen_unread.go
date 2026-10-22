package engine

import (
	goast "go/ast"
	"go/format"
	goparser "go/parser"
	"go/token"
	"sort"
)

// dropUnread removes the local variables of the generated functions that are assigned but never
// read, with their declarations, the assignments to them and their operands in the `_ = x`
// statements that kept the compiler quiet about them. An assignment whose value can have an effect
// (a call, or an operation that can panic) stays, with the variable replaced by the blank
// identifier. Only the statements in the statement lists of blocks are changed: a variable that
// is assigned in the init or post statement of a for, if or switch counts as read. Removing an
// assignment can leave the variables it read unread, so it repeats until nothing changes.
func dropUnread(src []byte) ([]byte, error) {
	for {
		out, changed, err := dropUnreadOnce(src)
		if err != nil || !changed {
			return src, err
		}
		src = out
	}
}

// removed is the replacement of a statement that is cut out.
const removed = ";"

type byteEdit struct {
	from, to int
	repl     string
}

func dropUnreadOnce(src []byte) ([]byte, bool, error) {
	fset := token.NewFileSet()
	f, err := goparser.ParseFile(fset, "parser.go", src, goparser.SkipObjectResolution)
	if err != nil {
		return nil, false, err
	}
	off := func(p token.Pos) int { return fset.Position(p).Offset }
	var edits []byteEdit
	// The names of the package: a local variable of one of them hides it, and the analysis does
	// not tell the two apart.
	outer := map[string]bool{}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *goast.FuncDecl:
			if d.Recv == nil {
				outer[d.Name.Name] = true
			}
		case *goast.GenDecl:
			for _, sp := range d.Specs {
				switch sp := sp.(type) {
				case *goast.ValueSpec:
					for _, n := range sp.Names {
						outer[n.Name] = true
					}
				case *goast.TypeSpec:
					outer[sp.Name.Name] = true
				}
			}
		}
	}
	for _, d := range f.Decls {
		fn, ok := d.(*goast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		edits = append(edits, unreadEdits(fn, outer, off)...)
	}
	if len(edits) == 0 {
		return src, false, nil
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].from < edits[j].from })
	var out []byte
	last := 0
	for _, e := range edits {
		if e.repl == removed {
			// A statement alone on its line takes the line with it.
			for e.from > last && (src[e.from-1] == ' ' || src[e.from-1] == '\t') {
				e.from--
			}
			if (e.from == 0 || src[e.from-1] == '\n') && e.to < len(src) && src[e.to] == '\n' {
				e.to++
			}
			e.repl = ""
		}
		if e.from < last {
			// An insertion at the end of a statement that was cut with its line.
			e.from, e.to = last, last
		}
		out = append(out, src[last:e.from]...)
		out = append(out, e.repl...)
		last = e.to
	}
	out = append(out, src[last:]...)
	out, err = format.Source(out)
	return out, true, err
}

// unreadEdits returns the edits that remove the unread variables declared by `var` statements
// directly in body (not in nested function literals, whose variables are left alone).
func unreadEdits(fn *goast.FuncDecl, outer map[string]bool, off func(token.Pos) int) []byteEdit {
	body := fn.Body
	declared := map[string]bool{}
	var declNames []string
	declEnds := map[string][]int{} // the end of each var statement that declares a name
	declName := map[*goast.Ident]bool{}
	// Var statements are collected wherever they are in the function (not in function literals);
	// the generated functions repeat the declarations of a variable in nested blocks, and the
	// analysis treats the variables of one name as one.
	goast.Inspect(body, func(n goast.Node) bool {
		if _, ok := n.(*goast.FuncLit); ok {
			return false
		}
		ds, ok := n.(*goast.DeclStmt)
		if !ok {
			return true
		}
		gd, ok := ds.Decl.(*goast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			return false
		}
		// Every var statement names its variables apart from the reads of its values.
		for _, sp := range gd.Specs {
			for _, n := range sp.(*goast.ValueSpec).Names {
				declName[n] = true
			}
		}
		if len(gd.Specs) == 1 && !gd.Lparen.IsValid() {
			if sp := gd.Specs[0].(*goast.ValueSpec); len(sp.Values) == 0 {
				for _, n := range sp.Names {
					if !declared[n.Name] {
						declNames = append(declNames, n.Name)
					}
					declared[n.Name] = true
					declEnds[n.Name] = append(declEnds[n.Name], off(ds.End()))
				}
			}
		}
		return true
	})
	if len(declared) == 0 {
		return nil
	}
	// A local of the name of a parameter, a result, the receiver or a package-level name hides
	// it, and the analysis does not tell the two apart: such a name is never removed, and is
	// silenced when nothing reads it.
	shared := map[string]bool{}
	for name := range outer {
		shared[name] = true
	}
	for _, fl := range []*goast.FieldList{fn.Recv, fn.Type.Params, fn.Type.Results} {
		if fl == nil {
			continue
		}
		for _, f := range fl.List {
			for _, n := range f.Names {
				shared[n.Name] = true
			}
		}
	}
	// inList: the statements of statement lists, and the statement a label in a list labels.
	inList := map[goast.Stmt]bool{}
	addList := func(stmts []goast.Stmt) {
		for _, s := range stmts {
			inList[s] = true
			if l, ok := s.(*goast.LabeledStmt); ok {
				inList[l.Stmt] = true
			}
		}
	}
	unsafe := false
	goast.Inspect(body, func(n goast.Node) bool {
		switch n := n.(type) {
		case *goast.BlockStmt:
			addList(n.List)
		case *goast.CaseClause:
			addList(n.Body)
		case *goast.CommClause:
			addList(n.Body)
		case *goast.AssignStmt:
			// Another declaration of a name makes the analysis unsafe.
			if n.Tok == token.DEFINE {
				for _, l := range n.Lhs {
					if id, ok := l.(*goast.Ident); ok && declared[id.Name] {
						unsafe = true
					}
				}
			}
		case *goast.RangeStmt:
			if n.Tok == token.DEFINE {
				for _, e := range []goast.Expr{n.Key, n.Value} {
					if id, ok := e.(*goast.Ident); ok && declared[id.Name] {
						unsafe = true
					}
				}
			}
		}
		return true
	})
	if unsafe {
		return nil
	}
	// notRead: the identifiers that are only written (the targets of a plain =, the operand of
	// ++ and --) or silenced (the operands of `_ = x`), in statement lists.
	notRead := map[*goast.Ident]bool{}
	// stuck: the identifiers written by statements that are not in a statement list (the init and
	// post statements of a for, if or switch), which cannot be cut.
	stuck := map[*goast.Ident]bool{}
	silencers := map[goast.Stmt]bool{}
	// silenceOp: the operands of the silencers. pinned: the variables written opposite an
	// untyped nil in an assignment of several values, which cannot take the blank identifier.
	silenceOp := map[*goast.Ident]bool{}
	pinned := map[string]bool{}
	goast.Inspect(body, func(n goast.Node) bool {
		switch n := n.(type) {
		case *goast.FuncLit:
			// Whatever a literal does with a variable counts as a read.
			return false
		case *goast.AssignStmt:
			if n.Tok != token.ASSIGN {
				return true
			}
			if !inList[n] {
				for _, l := range n.Lhs {
					if id, ok := l.(*goast.Ident); ok {
						stuck[id] = true
					}
				}
				return true
			}
			if isSilencer(n) {
				silencers[n] = true
				for _, r := range n.Rhs {
					silenceOp[r.(*goast.Ident)] = true
				}
				return false
			}
			if len(n.Lhs) > 1 && len(n.Lhs) == len(n.Rhs) {
				for i, r := range n.Rhs {
					if id, ok := r.(*goast.Ident); ok && id.Name == "nil" {
						if l, ok := n.Lhs[i].(*goast.Ident); ok {
							pinned[l.Name] = true
						}
					}
				}
			}
			for _, l := range n.Lhs {
				if id, ok := l.(*goast.Ident); ok {
					notRead[id] = true
				}
			}
		case *goast.IncDecStmt:
			if id, ok := n.X.(*goast.Ident); ok {
				if inList[n] {
					notRead[id] = true
				} else {
					stuck[id] = true
				}
			}
		}
		return true
	})
	reads, written, silenced := map[string]bool{}, map[string]bool{}, map[string]bool{}
	goast.Inspect(body, func(n goast.Node) bool {
		id, ok := n.(*goast.Ident)
		if !ok || !declared[id.Name] || declName[id] {
			return true
		}
		switch {
		case stuck[id]:
			written[id.Name] = true
		case silenceOp[id]:
			silenced[id.Name] = true
		case notRead[id]:
		default:
			reads[id.Name] = true
		}
		return true
	})
	// A variable that is only written, in a statement that cannot be cut, stays and is silenced.
	var edits []byteEdit
	dead := map[string]bool{}
	var quiet []string
	for _, name := range declNames {
		switch {
		case reads[name]:
		case written[name] || pinned[name] || shared[name]:
			if !silenced[name] {
				quiet = append(quiet, name)
			}
		default:
			dead[name] = true
		}
	}
	// The silencer follows each var statement that declares the names.
	quietAt := map[int][]string{}
	for _, name := range quiet {
		for _, end := range declEnds[name] {
			quietAt[end] = append(quietAt[end], name)
		}
	}
	for end, names := range quietAt {
		edits = append(edits, byteEdit{end, end, "\n" + silencer(names)})
	}
	if len(dead) == 0 {
		return edits
	}
	removeStmt := func(s goast.Stmt) { edits = append(edits, byteEdit{off(s.Pos()), off(s.End()), removed}) }
	isDead := func(e goast.Expr) bool {
		id, ok := e.(*goast.Ident)
		return ok && dead[id.Name]
	}
	// edit handles one statement of a statement list.
	edit := func(s goast.Stmt) {
		// A statement alone on its line is cut with its line; one after a label leaves the label.
		cut := removeStmt
		switch s := s.(type) {
		case *goast.IncDecStmt:
			if isDead(s.X) {
				cut(s)
			}
		case *goast.AssignStmt:
			if s.Tok != token.ASSIGN {
				return
			}
			if silencers[s] {
				var live []string
				for _, r := range s.Rhs {
					if !isDead(r) {
						live = append(live, r.(*goast.Ident).Name)
					}
				}
				switch {
				case len(live) == len(s.Rhs):
				case len(live) == 0:
					cut(s)
				default:
					edits = append(edits, byteEdit{off(s.Pos()), off(s.End()), silencer(live)})
				}
				return
			}
			allDead := true
			anyDead := false
			for _, l := range s.Lhs {
				if isDead(l) {
					anyDead = true
				} else {
					allDead = false
				}
			}
			switch {
			case !anyDead:
			case allDead && len(s.Rhs) == len(s.Lhs) && pureAll(s.Rhs):
				cut(s)
			default:
				for _, l := range s.Lhs {
					if isDead(l) {
						edits = append(edits, byteEdit{off(l.Pos()), off(l.End()), "_"})
					}
				}
			}
		}
	}
	list := func(stmts []goast.Stmt) {
		for _, s := range stmts {
			switch s := s.(type) {
			case *goast.DeclStmt:
				if gd, ok := s.Decl.(*goast.GenDecl); ok && gd.Tok == token.VAR {
					edits = append(edits, declEdits(s, gd, dead, off)...)
				}
			case *goast.LabeledStmt:
				edit(s.Stmt)
			default:
				edit(s)
			}
		}
	}
	goast.Inspect(body, func(n goast.Node) bool {
		switch n := n.(type) {
		case *goast.FuncLit:
			return false
		case *goast.BlockStmt:
			list(n.List)
		case *goast.CaseClause:
			list(n.Body)
		case *goast.CommClause:
			list(n.Body)
		}
		return true
	})
	return edits
}

// silencer returns the statement that uses the variables.
func silencer(names []string) string {
	out := "_"
	for range names[1:] {
		out += ", _"
	}
	out += " = "
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}

// declEdits removes the dead names of a var statement.
func declEdits(s *goast.DeclStmt, gd *goast.GenDecl, dead map[string]bool, off func(token.Pos) int) []byteEdit {
	if len(gd.Specs) != 1 || gd.Lparen.IsValid() {
		return nil
	}
	sp := gd.Specs[0].(*goast.ValueSpec)
	if len(sp.Values) != 0 {
		return nil
	}
	var keep []string
	for _, n := range sp.Names {
		if !dead[n.Name] {
			keep = append(keep, n.Name)
		}
	}
	if len(keep) == len(sp.Names) {
		return nil
	}
	if len(keep) == 0 {
		return []byteEdit{{off(s.Pos()), off(s.End()), removed}}
	}
	repl := ""
	for i, k := range keep {
		if i > 0 {
			repl += ", "
		}
		repl += k
	}
	return []byteEdit{{off(sp.Names[0].Pos()), off(sp.Names[len(sp.Names)-1].End()), repl}}
}

// isSilencer reports whether a is `_, _ = x, y`: blank targets and identifier values.
func isSilencer(a *goast.AssignStmt) bool {
	if len(a.Lhs) != len(a.Rhs) {
		return false
	}
	for i := range a.Lhs {
		l, ok := a.Lhs[i].(*goast.Ident)
		if !ok || l.Name != "_" {
			return false
		}
		if _, ok := a.Rhs[i].(*goast.Ident); !ok {
			return false
		}
	}
	return true
}

// pureAll reports whether evaluating the expressions has no effect and cannot panic (a selector
// of a receiver that is not nil is taken to be such).
func pureAll(es []goast.Expr) bool {
	for _, e := range es {
		if !pure(e) {
			return false
		}
	}
	return true
}

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
