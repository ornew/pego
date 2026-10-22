package engine

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ornew/pego/grammar"
)

// resolveStarts reads the declaration attributes of rules and returns the
// start rules, the rules marked @start where parsing may begin, in declaration
// order, and the default one, which Parse uses when no start rule is named.
// Every declaration attribute other than @start and @start(default) is an
// error, and so is @start on a hidden rule. A grammar needs a start rule;
// with several, one of them must be the default.
func resolveStarts(rules []*grammar.RuleDef, control *compileControl) (starts []string, def string, errs ErrorList) {
	errorf := func(pos grammar.Pos, format string, args ...any) {
		errs = append(errs, &Error{Pos: pos, Msg: fmt.Sprintf(format, args...)})
	}
	var second *grammar.Attribute
	for _, rd := range rules {
		control.poll()
		var start *grammar.Attribute
		isDefault := false
		for _, a := range rd.Attrs {
			control.poll()
			if a.Pragma || a.Name != "start" {
				errorf(a.Diagnostic(), "unknown declaration attribute %s%s", annotationMarker(a), a.Name)
				continue
			}
			if start != nil {
				errorf(a.Diagnostic(), "duplicate @start on rule %s", rd.Name)
				continue
			}
			start = a
			switch {
			case len(a.Args) == 0 && len(a.KwArgs) == 0:
			case len(a.Args) == 1 && len(a.KwArgs) == 0 && isDefaultArg(a.Args[0]):
				isDefault = true
			default:
				errorf(a.Diagnostic(), "@start: the only argument is default")
			}
		}
		if start == nil || slices.Contains(starts, rd.Name) {
			continue // a rule defined twice is reported by the compiler
		}
		if hiddenRule(rd.Name) {
			errorf(start.Diagnostic(), "rule %s is hidden: a hidden rule cannot be a start rule", rd.Name)
			continue
		}
		starts = append(starts, rd.Name)
		if len(starts) == 2 {
			second = start
		}
		if isDefault {
			if def != "" {
				errorf(start.Diagnostic(), "more than one default start rule: %s and %s", def, rd.Name)
				continue
			}
			def = rd.Name
		}
	}
	switch {
	case len(errs) > 0:
	case len(starts) == 0:
		var pos grammar.Pos
		if len(rules) > 0 {
			pos = rules[0].Diagnostic()
		}
		errorf(pos, "the grammar has no start rule: mark each rule where parsing may begin with @start")
	case def == "" && len(starts) == 1:
		def = starts[0]
	case def == "":
		errorf(second.Diagnostic(), "several start rules (%s) and no default: mark one with @start(default)", strings.Join(starts, ", "))
	}
	return starts, def, errs
}

// isDefaultArg reports whether the attribute argument is the bare identifier
// default.
func isDefaultArg(arg any) bool {
	ref, ok := arg.(*grammar.Ref)
	return ok && ref.Name == "default" && ref.Level == ""
}

// Starts returns the start rules of the grammar, the rules marked @start where
// parsing may begin, in declaration order.
func (prog *Program) Starts() []string { return slices.Clone(prog.starts) }

// DefaultStart returns the default start rule: the one marked
// @start(default), or the only start rule.
func (prog *Program) DefaultStart() string { return prog.defaultStart }

// CheckStart reports an error unless name selects a start rule: it is the name
// of a start rule, or empty for the default start rule.
func (prog *Program) CheckStart(name string) error {
	if slices.Contains(prog.starts, prog.resolveStart(name)) {
		return nil
	}
	if name == "" {
		return fmt.Errorf("no start rule is given")
	}
	if err := EntryError(name); err != nil {
		return err
	}
	if prog.definesRule(name) {
		return fmt.Errorf("rule %s is not a start rule; parsing can begin only at a rule marked @start", name)
	}
	return fmt.Errorf("rule %s is not defined", name)
}

// resolveStart returns the rule name selects: name, or the default start rule
// when name is empty.
func (prog *Program) resolveStart(name string) string {
	if name == "" {
		return prog.defaultStart
	}
	return name
}

// startRule returns the start rule name selects.
func (prog *Program) startRule(name string) (*rule, error) {
	if err := prog.CheckStart(name); err != nil {
		return nil, err
	}
	return prog.byName[prog.resolveStart(name)], nil
}

// definesRule reports whether the grammar defines the rule name, without preparing a backend.
func (prog *Program) definesRule(name string) bool {
	if prog.Grammar != nil {
		return prog.byName[name] != nil
	}
	m := prog.Module()
	return slices.ContainsFunc(m.Rules, func(r RuleInfo) bool { return m.Strings[r.Name] == name })
}
