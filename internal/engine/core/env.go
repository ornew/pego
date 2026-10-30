package core

// Env is a persistent vector with one current value per declared variable, indexed by the
// variable's slot (its position in the declaration order). A vector with at most envWidth
// variables is one node holding the values; a larger one is a trie of nodes with envWidth
// children, whose depth (the levels above the leaves) every holder knows from the variable
// count (EnvLevels). Environments saved for rollback are never modified: a rebind copies the
// nodes on the path to its slot and shares the rest.
type Env struct {
	// vals holds the values of a leaf; in an inner node every element is the *Env of a child.
	vals []any
}

const (
	envBits  = 4
	envWidth = 1 << envBits
	envMask  = envWidth - 1
)

// EnvLevels returns the number of levels above the leaves of an environment of n variables.
func EnvLevels(n int) int {
	levels := 0
	for limit := envWidth; n > limit; limit <<= envBits {
		levels++
	}
	return levels
}

// NewEnv returns the environment that holds vals; levels is EnvLevels(len(vals)).
func NewEnv(vals []any, levels int) *Env {
	if len(vals) == 0 {
		return nil
	}
	if levels == 0 {
		return &Env{vals: append([]any(nil), vals...)}
	}
	span := 1 << (envBits * levels)
	kids := make([]any, 0, (len(vals)+span-1)/span)
	for lo := 0; lo < len(vals); lo += span {
		hi := min(lo+span, len(vals))
		kids = append(kids, NewEnv(vals[lo:hi], levels-1))
	}
	return &Env{vals: kids}
}

// Get returns the value of the variable in slot i.
func (e *Env) Get(i, levels int) any {
	for l := levels; l > 0; l-- {
		e = e.vals[i>>(envBits*l)&envMask].(*Env)
	}
	return e.vals[i&envMask]
}

// Set returns the environment that differs from e in the value of slot i. The copied nodes are
// those on the path to the slot: levels+1 of them.
func (e *Env) Set(i, levels int, v any) *Env {
	if levels == 0 {
		return e.setLeaf(i, v)
	}
	k := i >> (envBits * levels) & envMask
	kids := make([]any, len(e.vals))
	copy(kids, e.vals)
	kids[k] = e.vals[k].(*Env).Set(i&(1<<(envBits*levels)-1), levels-1, v)
	return &Env{vals: kids}
}

// setLeaf copies a leaf. The leaves of up to four variables, the common case, are allocated
// together with their values.
func (e *Env) setLeaf(i int, v any) *Env {
	switch len(e.vals) {
	case 1:
		n := &struct {
			Env
			a [1]any
		}{a: [1]any{v}}
		n.vals = n.a[:]
		return &n.Env
	case 2:
		n := &struct {
			Env
			a [2]any
		}{a: [2]any(e.vals)}
		n.a[i] = v
		n.vals = n.a[:]
		return &n.Env
	case 3:
		n := &struct {
			Env
			a [3]any
		}{a: [3]any(e.vals)}
		n.a[i] = v
		n.vals = n.a[:]
		return &n.Env
	case 4:
		n := &struct {
			Env
			a [4]any
		}{a: [4]any(e.vals)}
		n.a[i] = v
		n.vals = n.a[:]
		return &n.Env
	}
	vals := make([]any, len(e.vals))
	copy(vals, e.vals)
	vals[i] = v
	return &Env{vals: vals}
}

// SetVars makes vals the values of the variables of the parser, in slot order, and the
// environment the current one.
func (p *Parser) SetVars(vals []any) {
	p.envLevels = EnvLevels(len(vals))
	p.Env = NewEnv(vals, p.envLevels)
}

// EnvDepth returns the depth of the parser's environment, for Env.Get and Env.Set.
func (p *Parser) EnvDepth() int { return p.envLevels }
