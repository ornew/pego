package compile

import (
	"sort"
	"strings"

	"github.com/ornew/pego/internal/value"
)

// Ty is a type used in type checking.
type Ty interface{ String() string }

// BasicTy is a built-in type.
type BasicTy string

const (
	TyAny    BasicTy = "unknown" // type not known statically (compatible with every type); no type can be written to mean it
	tyNever  BasicTy = "_|_"     // no value, and the initial value for inference
	TyInt    BasicTy = "int"
	TyString BasicTy = "string"
	TyBool   BasicTy = "bool"
	TyNil    BasicTy = "nil"
	TyNode   BasicTy = "node" // any node
)

func (t BasicTy) String() string {
	if t == TyNode {
		return "node[_]" // node without a type argument is not a type
	}
	return string(t)
}

// NamedTy is a named node type.
type NamedTy struct {
	Name string
	Kind byte // 's': struct, 'c': reserved node kind
}

// String returns the name of a struct or Missing, and the value-model term of another reserved
// node kind (node[string], node, error), so that diagnostics name the types a grammar can write.
func (t NamedTy) String() string {
	if t.Kind == 'c' && t != TyMissing {
		return FromLegacy(t).String()
	}
	return t.Name
}

var (
	TyMatch   = NamedTy{value.TypeString, 'c'}
	tySeq     = NamedTy{value.TypeTuple, 'c'}
	tyList    = NamedTy{value.TypeList, 'c'}
	TyError   = NamedTy{value.TypeError, 'c'}
	TyMissing = NamedTy{value.TypeMissing, 'c'}
)

type ListTy struct{ Elem Ty }

func (t ListTy) String() string { return "[]" + typeTerm(t.Elem) }

type OptTy struct{ Elem Ty }

func (t OptTy) String() string { return "*" + typeTerm(t.Elem) }

type JoinTy struct{ Alts []Ty }

func (t JoinTy) String() string {
	parts := make([]string, len(t.Alts))
	for i, a := range t.Alts {
		parts[i] = a.String()
	}
	return strings.Join(parts, " | ")
}

// RecordTy is the type of a tuple node whose fields are its captures. Diagnostics print it as
// node{k: T, ...}.
type RecordTy struct{ fields map[string]Ty }

func (t RecordTy) String() string {
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

func typeTerm(t Ty) string {
	if _, ok := t.(JoinTy); ok {
		return "(" + t.String() + ")"
	}
	return t.String()
}

// IsNode reports whether t is a node type.
func IsNode(t Ty) bool {
	switch t := t.(type) {
	case NamedTy, ListTy, RecordTy:
		return true
	case BasicTy:
		return t == TyNode || t == TyAny || t == tyNever
	case NodeTy:
		return t.Name != "" // a named node type has a Go type of its own
	case JoinTy:
		for _, a := range t.Alts {
			if !IsNode(a) {
				return false
			}
		}
		return true
	}
	return false
}
