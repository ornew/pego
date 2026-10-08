package xml

import (
	"strings"
	"unicode/utf8"
)

// The namespace names bound to the prefixes xml and xmlns.
const (
	XMLNamespace   = "http://www.w3.org/XML/1998/namespace"
	XMLNSNamespace = "http://www.w3.org/2000/xmlns/"
)

// ResolveNamespaces applies Namespaces in XML 1.0 (Third Edition) to a decoded tree. It sets Space and
// Local of every element and attribute (an attribute without a prefix has no namespace; namespace
// declarations are in XMLNSNamespace, xmlns="..." with the local name xmlns), and checks that the
// document is namespace-well-formed: every element and attribute name is a qualified name, its prefix is
// declared, the prefixes xml and xmlns and their namespace names are used only as the specification
// allows, no prefix is undeclared (xmlns:p="", which is XML 1.1), no element has two attributes with the
// same namespace name and local part, and no entity name, notation name or processing instruction target
// contains a colon.
func (t *Tree) ResolveNamespaces() error {
	r := resolver{t: t}
	if err := r.dtd(); err != nil {
		return err
	}
	for _, items := range [][]Item{t.Prolog, t.Epilog} {
		for _, it := range items {
			if pi, ok := it.(*PI); ok && strings.Contains(pi.Target.Text, ":") {
				return r.errorf(pi.Start, -1, "the processing instruction target %s contains a colon", pi.Target.Text)
			}
		}
	}
	return r.elem(t.Root, nil)
}

type binding struct {
	prefix, uri string
	next        *binding
}

func (b *binding) lookup(prefix string) (string, bool) {
	for ; b != nil; b = b.next {
		if b.prefix == prefix {
			return b.uri, true
		}
	}
	switch prefix {
	case "xml":
		return XMLNamespace, true
	case "":
		return "", true
	}
	return "", false
}

type resolver struct{ t *Tree }

func (r *resolver) errorf(pos, ref int, format string, args ...any) error {
	p := processor{input: r.t.input, ref: -1}
	if ref >= 0 {
		pos = ref
	}
	return p.errorf(pos, format, args...)
}

// dtd checks the names of the declarations.
func (r *resolver) dtd() error {
	if r.t.Doc.Doctype == nil {
		return nil
	}
	seen := map[*Entity]bool{}
	var check func(decls []Decl, ref int) error
	check = func(decls []Decl, ref int) error {
		for _, d := range decls {
			var name string
			var pos int
			switch d := d.(type) {
			case *EntityDecl:
				name, pos = d.Name.Text, d.Start
			case *NotationDecl:
				name, pos = d.Name.Text, d.Start
			case *PI:
				name, pos = d.Target.Text, d.Start
			case *PERef:
				if e := r.t.DTD.Params[d.Name()]; e != nil && e.decls != nil && !seen[e] {
					seen[e] = true
					at := ref
					if at < 0 {
						at = d.Start
					}
					if err := check(e.decls, at); err != nil {
						return err
					}
				}
				continue
			default:
				continue
			}
			if strings.Contains(name, ":") {
				return r.errorf(pos, ref, "the name %s contains a colon", name)
			}
		}
		return nil
	}
	return check(r.t.Doc.Doctype.Subset, -1)
}

// qname splits a qualified name, reporting whether name is one.
func qname(name string) (prefix, local string, ok bool) {
	i := strings.IndexByte(name, ':')
	if i < 0 {
		return "", name, true
	}
	prefix, local = name[:i], name[i+1:]
	if prefix == "" || local == "" || strings.IndexByte(local, ':') >= 0 {
		return "", "", false
	}
	if r, _ := utf8.DecodeRuneInString(local); !isNameStart(r) {
		return "", "", false
	}
	return prefix, local, true
}

func (r *resolver) elem(el *Elem, scope *binding) error {
	pos := el.Src.Start
	for i := range el.Attrs {
		a := &el.Attrs[i]
		if a.Name == "xmlns" {
			if a.Value == XMLNamespace || a.Value == XMLNSNamespace {
				return r.errorf(pos, el.ref, "%s cannot be the default namespace", a.Value)
			}
			scope = &binding{"", a.Value, scope}
			a.Space, a.Local = XMLNSNamespace, "xmlns"
			continue
		}
		prefix, local, ok := qname(a.Name)
		if !ok {
			return r.errorf(pos, el.ref, "the attribute name %s is not a qualified name", a.Name)
		}
		if prefix != "xmlns" {
			continue
		}
		switch {
		case local == "xmlns":
			return r.errorf(pos, el.ref, "the prefix xmlns must not be declared")
		case local == "xml" && a.Value != XMLNamespace:
			return r.errorf(pos, el.ref, "the prefix xml cannot be bound to %s", a.Value)
		case local != "xml" && a.Value == XMLNamespace:
			return r.errorf(pos, el.ref, "%s can be bound only to the prefix xml", a.Value)
		case a.Value == XMLNSNamespace:
			return r.errorf(pos, el.ref, "%s cannot be bound to a prefix", a.Value)
		case a.Value == "":
			return r.errorf(pos, el.ref, "the prefix %s cannot be undeclared in XML 1.0", local)
		}
		scope = &binding{local, a.Value, scope}
		a.Space, a.Local = XMLNSNamespace, local
	}
	prefix, local, ok := qname(el.Name)
	if !ok {
		return r.errorf(pos, el.ref, "the element name %s is not a qualified name", el.Name)
	}
	if prefix == "xmlns" {
		return r.errorf(pos, el.ref, "the element name %s has the prefix xmlns", el.Name)
	}
	uri, ok := scope.lookup(prefix)
	if !ok {
		return r.errorf(pos, el.ref, "the prefix %s is not declared", prefix)
	}
	el.Space, el.Local = uri, local
	for i := range el.Attrs {
		a := &el.Attrs[i]
		if a.Space == XMLNSNamespace {
			continue
		}
		prefix, local, _ := qname(a.Name)
		uri := ""
		if prefix != "" {
			if uri, ok = scope.lookup(prefix); !ok {
				return r.errorf(pos, el.ref, "the prefix %s is not declared", prefix)
			}
		}
		a.Space, a.Local = uri, local
		for _, o := range el.Attrs[:i] {
			if o.Space == uri && o.Local == local {
				return r.errorf(pos, el.ref, "the attributes %s and %s have the same namespace name and local part", o.Name, a.Name)
			}
		}
	}
	for _, c := range el.Children {
		switch c := c.(type) {
		case *Elem:
			if err := r.elem(c, scope); err != nil {
				return err
			}
		case *PI:
			if strings.Contains(c.Target.Text, ":") {
				return r.errorf(c.Start, el.ref, "the processing instruction target %s contains a colon", c.Target.Text)
			}
		}
	}
	return nil
}
