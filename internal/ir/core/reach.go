package core

// Entry is a way of entering a rule. A generator enters a rule through its slot, the rule table's
// entry that the runtime runs with its bookkeeping (Body), or through its wrapper, a function of its
// own that calls the rule directly; a rule that nothing enters in either way has nothing to print.
type Entry uint8

const (
	// EnterSlot enters the rule through its rule table entry.
	EnterSlot Entry = 1 << iota
	// EnterWrapper enters the rule through its own function, which does not run the table entry's body.
	EnterWrapper
)

// Root is a rule that something outside the rules enters, such as a start rule, in the way Entry.
type Root struct {
	Rule  int
	Entry Entry
}

// Reach returns, for each rule by ID, the ways in which a parse can enter it (zero if it cannot). It
// follows the call graph (Calls) from the roots: a reached rule enters each rule it calls in the way
// enter(rule) names, which is how the generator prints a call of that rule. The result covers value-free
// twins like any other rule.
func (m *Module) Reach(roots []Root, enter func(rule int) Entry) []Entry {
	reach := make([]Entry, len(m.Rules))
	var work []int
	add := func(rule int, e Entry) {
		if reach[rule]&e == e {
			return
		}
		if reach[rule] == 0 {
			work = append(work, rule)
		}
		reach[rule] |= e
	}
	for _, r := range roots {
		add(r.Rule, r.Entry)
	}
	for len(work) > 0 {
		k := work[len(work)-1]
		work = work[:len(work)-1]
		for _, to := range m.Calls[k] {
			add(to, enter(to))
		}
	}
	return reach
}
