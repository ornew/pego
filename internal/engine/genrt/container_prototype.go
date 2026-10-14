package genrt

import (
	"fmt"
	"unicode/utf8"
)

// These private adapters are experimental building blocks, not generated API.
// Parsing deliberately uses the batch oracle until examined-input metadata is
// emitted for every generated rule. Keeping batch memo entries across edits is
// unsound: farthest failure does not describe successful lookahead dependencies.
type prototypeDocument struct {
	text     string
	defaults parseOptions
	start    *rule
	seen     int
}

func newPrototypeDocument(start *rule, seen int, text string, opts ...ParseOption) (d *prototypeDocument, err error) {
	defer recoverParse(&d, &err)
	o, err := resolveOptions(opts)
	if err != nil {
		return nil, err
	}
	if o.unit != Bytes && o.unit != CodePoints {
		return nil, fmt.Errorf("invalid document position unit: %d", o.unit)
	}
	if start == nil {
		return nil, fmt.Errorf("document start rule is required")
	}
	prepared := prepareOptions(o)
	prepared.control.check()
	// Controllers and invocation counters never become construction defaults.
	o.control, o.prepared = nil, false
	return &prototypeDocument{text: text, defaults: o, start: start, seen: seen}, nil
}

func (d *prototypeDocument) parse(opts ...ParseOption) (*Node, error) {
	o := d.defaults
	// Resolve defaults and overrides once in the existing ordered option path.
	merged := make([]ParseOption, 1, len(opts)+1)
	merged[0] = func(v *parseOptions) { *v = o }
	merged = append(merged, opts...)
	resolved, err := resolveOptions(merged)
	if err != nil {
		return nil, err
	}
	if resolved.unit != d.defaults.unit {
		return nil, fmt.Errorf("document parse cannot change position unit")
	}
	return parseResolved(d.start, d.seen, d.text, resolved)
}

func (d *prototypeDocument) edit(start, end int, replacement string) error {
	from, err := prototypeBytePosition(d.text, d.defaults.unit, start)
	if err != nil {
		return err
	}
	to, err := prototypeBytePosition(d.text, d.defaults.unit, end)
	if err != nil {
		return err
	}
	if end < start {
		return fmt.Errorf("invalid edit range [%d,%d)", start, end)
	}
	// All admission precedes mutation, including both byte boundaries.
	d.text = d.text[:from] + replacement + d.text[to:]
	return nil
}

func prototypeBytePosition(text string, unit Unit, pos int) (int, error) {
	if pos < 0 {
		return 0, fmt.Errorf("negative input position: %d", pos)
	}
	if unit == CodePoints {
		at := 0
		for i := 0; i < pos; i++ {
			if at == len(text) {
				return 0, fmt.Errorf("input position out of range: %d", pos)
			}
			_, size := utf8.DecodeRuneInString(text[at:])
			at += size
		}
		return at, nil
	}
	if pos > len(text) {
		return 0, fmt.Errorf("input position out of range: %d", pos)
	}
	if pos < len(text) && !utf8.RuneStart(text[pos]) {
		for start := pos - 1; start >= max(0, pos-utf8.UTFMax+1); start-- {
			if utf8.RuneStart(text[start]) {
				_, size := utf8.DecodeRuneInString(text[start:])
				if start+size > pos {
					return 0, fmt.Errorf("position %d is not at a character boundary", pos)
				}
				break
			}
		}
	}
	return pos, nil
}

// prototypeMemoDependency is separate from batch memoEntry until analysis can
// supply these facts. A missing fact must prevent reuse, never default to safe.
type prototypeMemoDependency struct {
	from, examined int
	positionReads  bool
	recovered      bool
	provisional    bool
	complete       bool
}

// advance returns a copy so invalidation cannot partially shift a retained
// entry. The caller must additionally shift memo keys, result spans and failure
// positions, preserving DAG identities and variable/operator-level keys.
func (m prototypeMemoDependency) advance(start, end, delta int) (prototypeMemoDependency, bool) {
	if !m.complete || m.provisional {
		return m, false
	}
	if m.examined <= start {
		return m, true
	}
	if m.from >= end && !m.positionReads && !m.recovered {
		m.from += delta
		m.examined += delta
		return m, true
	}
	return m, false
}

// prototypeCloneNode follows the engine Clone ownership contract: node aliases
// and cycles are preserved; arbitrary mutable action values are not frozen.
func prototypeCloneNode(n *Node, seen map[*Node]*Node) *Node {
	if n == nil {
		return nil
	}
	if copy, ok := seen[n]; ok {
		return copy
	}
	copy := new(Node)
	*copy = *n
	seen[n] = copy
	if n.Children != nil {
		copy.Children = make([]*Node, len(n.Children))
		for i, child := range n.Children {
			copy.Children[i] = prototypeCloneNode(child, seen)
		}
	}
	if n.Fields != nil {
		copy.Fields = append(Fields{}, n.Fields...)
		for i := range copy.Fields {
			if child, ok := copy.Fields[i].Value.(*Node); ok {
				copy.Fields[i].Value = prototypeCloneNode(child, seen)
			}
		}
	}
	return copy
}
