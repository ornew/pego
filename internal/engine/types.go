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
	tyAny    basicTy = "any" // type not known statically (compatible with every type)
	tyNever  basicTy = "_|_" // no value, and the initial value for inference
	tyInt    basicTy = "int"
	tyString basicTy = "string"
	tyBool   basicTy = "bool"
	tyNil    basicTy = "nil"
	tyNode   basicTy = "node" // any node
)

func (t basicTy) String() string {
	if t == tyNode {
		return "node[_]" // node without a type argument is not a type
	}
	return string(t)
}

// namedTy is a named node type.
type namedTy struct {
	name string
	kind byte // 's': struct, 'c': reserved node kind
}

// String returns the name of a struct or Missing, and the value-model term of another reserved
// node kind (node[string], node, error), so that diagnostics name the types a grammar can write.
func (t namedTy) String() string {
	if t.kind == 'c' && t != tyMissing {
		return fromLegacy(t).String()
	}
	return t.name
}

var (
	tyMatch   = namedTy{TypeString, 'c'}
	tySeq     = namedTy{TypeTuple, 'c'}
	tyList    = namedTy{TypeList, 'c'}
	tyError   = namedTy{TypeError, 'c'}
	tyMissing = namedTy{TypeMissing, 'c'}
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

// recordTy is the type of a tuple node whose fields are its captures. Diagnostics print it as
// node{k: T, ...}.
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
	return "node{" + strings.Join(parts, ", ") + "}"
}

func typeTerm(t ty) string {
	if _, ok := t.(unionTy); ok {
		return "(" + t.String() + ")"
	}
	return t.String()
}

// isNode reports whether t is a node type.
func isNode(t ty) bool {
	switch t := t.(type) {
	case namedTy, listTy, recordTy:
		return true
	case basicTy:
		return t == tyNode || t == tyAny || t == tyNever
	case nodeTy:
		return t.name != "" // a named node type has a Go type of its own
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
