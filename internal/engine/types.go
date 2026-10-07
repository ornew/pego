package engine

import (
	"sort"
	"strings"
)

// ty is a type used in type checking.
type ty interface{ String() string }

// basicTy is a built-in type.
type basicTy string

const (
	tyAny      basicTy = "any"   // type not known statically (compatible with every type)
	tyNever    basicTy = "never" // no value (the initial value for inference)
	tyInt      basicTy = "int"
	tyString   basicTy = "string"
	tyBool     basicTy = "bool"
	tyNil      basicTy = "nil"
	tyNode     basicTy = "node"     // any node
	tyTerminal basicTy = "terminal" // any terminal
)

func (t basicTy) String() string { return string(t) }

// namedTy is a named node type.
type namedTy struct {
	name string
	kind byte // 's': struct, 't': terminal type, 'c': reserved CST node type
}

func (t namedTy) String() string { return t.name }

var (
	tyMatch    = namedTy{TypeMatch, 'c'}
	tySeq      = namedTy{TypeSeq, 'c'}
	tyList     = namedTy{TypeList, 'c'}
	tyOperator = namedTy{TypeOperator, 'c'}
	tyError    = namedTy{TypeError, 'c'}
)

type listTy struct{ elem ty }

func (t listTy) String() string { return "[]" + typeTerm(t.elem) }

type optTy struct{ elem ty }

func (t optTy) String() string { return "*" + typeTerm(t.elem) }

type unionTy struct{ alts []ty }

func (t unionTy) String() string {
	parts := make([]string, len(t.alts))
	for i, a := range t.alts {
		parts[i] = a.String()
	}
	return strings.Join(parts, " | ")
}

// recordTy is the type of a Seq node whose fields are its captures.
type recordTy struct{ fields map[string]ty }

func (t recordTy) String() string {
	names := make([]string, 0, len(t.fields))
	for n := range t.fields {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = n + ": " + t.fields[n].String()
	}
	return "Seq{" + strings.Join(parts, ", ") + "}"
}

func typeTerm(t ty) string {
	if _, ok := t.(unionTy); ok {
		return "(" + t.String() + ")"
	}
	return t.String()
}

// union builds the union of types. It flattens nested unions and removes duplicates and never.
func union(ts ...ty) ty {
	var alts []ty
	seen := map[string]bool{}
	var add func(t ty)
	add = func(t ty) {
		switch t := t.(type) {
		case unionTy:
			for _, a := range t.alts {
				add(a)
			}
			return
		case nil:
			return
		}
		if t == tyNever || seen[t.String()] {
			return
		}
		seen[t.String()] = true
		alts = append(alts, t)
	}
	for _, t := range ts {
		add(t)
	}
	for _, a := range alts {
		if a == tyAny {
			return tyAny
		}
	}
	// A union containing nil is collapsed into an optional type.
	hasNil := false
	var rest []ty
	for _, a := range alts {
		switch a := a.(type) {
		case optTy:
			hasNil = true
			rest = append(rest, a.elem)
		default:
			if a == tyNil {
				hasNil = true
			} else {
				rest = append(rest, a)
			}
		}
	}
	if hasNil {
		if len(rest) == 0 {
			return tyNil
		}
		return optTy{union(rest...)}
	}
	switch len(alts) {
	case 0:
		return tyNever
	case 1:
		return alts[0]
	}
	return unionTy{alts}
}

func optional(t ty) ty {
	switch t.(type) {
	case optTy:
		return t
	}
	if t == tyAny || t == tyNil {
		return t
	}
	return union(t, tyNil)
}

// isNode reports whether t is a node type.
func isNode(t ty) bool {
	switch t := t.(type) {
	case namedTy, listTy, recordTy:
		return true
	case basicTy:
		return t == tyNode || t == tyTerminal || t == tyAny || t == tyNever
	case unionTy:
		for _, a := range t.alts {
			if !isNode(a) {
				return false
			}
		}
		return true
	}
	return false
}

// assignable reports whether a value of type s can be placed where type d is expected.
func assignable(s, d ty) bool {
	if s == tyAny || d == tyAny || s == tyNever {
		return true
	}
	if s.String() == d.String() {
		return true
	}
	if su, ok := s.(unionTy); ok {
		for _, a := range su.alts {
			if !assignable(a, d) {
				return false
			}
		}
		return true
	}
	switch d := d.(type) {
	case unionTy:
		for _, a := range d.alts {
			if assignable(s, a) {
				return true
			}
		}
		return false
	case optTy:
		if s == tyNil {
			return true
		}
		if so, ok := s.(optTy); ok {
			return assignable(so.elem, d.elem)
		}
		return assignable(s, d.elem)
	}
	if _, ok := s.(optTy); ok {
		return false
	}
	if s == tyError {
		// Error-recovery nodes can be placed where any node type is expected.
		return isNode(d)
	}
	switch d := d.(type) {
	case basicTy:
		switch d {
		case tyNode:
			return isNode(s)
		case tyTerminal:
			n, ok := s.(namedTy)
			return ok && (n.kind == 't' || n == tyMatch) || s == tyTerminal
		}
		return false
	case namedTy:
		switch s := s.(type) {
		case namedTy:
			return s == d
		case recordTy:
			return d == tySeq
		case listTy:
			return d == tyList
		}
		return false
	case listTy:
		if sl, ok := s.(listTy); ok {
			return assignable(sl.elem, d.elem)
		}
		return false
	case recordTy:
		sr, ok := s.(recordTy)
		if !ok {
			return false
		}
		for n, ft := range d.fields {
			st, ok := sr.fields[n]
			if !ok || !assignable(st, ft) {
				return false
			}
		}
		return true
	}
	return false
}
