package document

import "github.com/ornew/zeroparse/internal/engine/core"

// equalChangeDiagnostics compares complete supported diagnostic values. Unknown
// diagnostic types conservatively differ rather than running uncontrolled code.
func equalChangeDiagnostics(p *core.Parser, a, b error) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	switch x := a.(type) {
	case *core.SyntaxError:
		y, ok := b.(*core.SyntaxError)
		if !ok {
			return false
		}
		if x == nil || y == nil {
			return x == nil && y == nil
		}
		p.Spend()
		if x.Pos != y.Pos || x.Line != y.Line || x.Col != y.Col {
			return false
		}
		for i, xs := range [][]string{x.Expected, x.Messages} {
			ys := [][]string{y.Expected, y.Messages}[i]
			if (xs == nil) != (ys == nil) || len(xs) != len(ys) {
				return false
			}
			for j := range xs {
				if !equalChangeText(p, xs[j], ys[j]) {
					return false
				}
			}
		}
		return true
	case core.SyntaxErrors:
		y, ok := b.(core.SyntaxErrors)
		if !ok || (x == nil) != (y == nil) || len(x) != len(y) {
			return false
		}
		for i := range x {
			p.Spend()
			if !equalChangeDiagnostics(p, x[i], y[i]) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func equalChangeText(p *core.Parser, a, b string) bool {
	p.Spend()
	if len(a) != len(b) {
		return false
	}
	for off := 0; off < len(a); {
		end := min(len(a), off+2048)
		p.Control.Charge(uint64(end-off) * 2)
		if a[off:end] != b[off:end] {
			return false
		}
		off = end
	}
	return true
}

func equalChangeSnapshots(a, b *Snapshot) bool {
	return equalChangeSnapshotsControlled(&core.Parser{}, a, b)
}

// equalChangeSnapshotsControlled compares corresponding immutable directory IDs.
// Capture registers each node identity once; record sharing preserves its ID.
// Exact directory equality proves equal metadata, ordered edges and alias topology,
// including cycles. A different directory layout may conservatively differ; no
// allocation or isomorphism search is needed to establish unchanged output.
func equalChangeSnapshotsControlled(p *core.Parser, a, b *Snapshot) bool {
	p.Control.Check()
	p.Spend()
	if a.Unit() != b.Unit() || !equalChangeDiagnostics(p, a.parseError, b.parseError) {
		return false
	}
	ag, bg := &a.graph, &b.graph
	if ag.root != bg.root || ag.count() != bg.count() {
		return false
	}
	for i := 0; i < ag.count(); i++ {
		p.Spend()
		x, y := ag.at(i), bg.at(i)
		xp, yp := ag.position(i, x), bg.position(i, y)
		if !equalChangeText(p, x.kind.TypeName(), y.kind.TypeName()) || !equalChangeText(p, x.kind.RuleName(), y.kind.RuleName()) || xp.start != yp.start || xp.end != yp.end || !equalChangeText(p, x.text, y.text) || x.terminal != y.terminal || ag.hasChildren(x) != bg.hasChildren(y) || ag.childCount(x) != bg.childCount(y) {
			return false
		}
		if (x.fields == nil) != (y.fields == nil) || len(x.fields) != len(y.fields) {
			return false
		}
		for j, field := range x.fields {
			p.Spend()
			other := y.fields[j]
			if !equalChangeText(p, field.Name, other.Name) {
				return false
			}
			switch value := field.Value.(type) {
			case string:
				text, ok := other.Value.(string)
				if !ok || !equalChangeText(p, value, text) {
					return false
				}
			default:
				// snapshotRef IDs preserve typed nil and aliases. The only other
				// captured values are comparable nil, int and bool scalars.
				if field.Value != other.Value {
					return false
				}
			}
		}
		for j := 0; j < ag.childCount(x); j++ {
			p.Spend()
			if ag.childID(x, j) != bg.childID(y, j) {
				return false
			}
		}
	}
	p.Control.Check()
	return true
}
