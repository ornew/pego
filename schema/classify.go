package schema

import (
	"fmt"
	"slices"
	"sort"
)

// Class tells whether a change can break an existing reader of the output.
type Class string

const (
	// Breaking marks a change that can stop a reader from compiling against the generated types
	// or from handling a value.
	Breaking Class = "breaking"
	// Compatible marks an addition that an existing reader can ignore.
	Compatible Class = "compatible"
)

// The rules that classify a change. Each Impact names the rule that produced it.
const (
	RuleTypeAdded          = "type-added"
	RuleTypeRemoved        = "type-removed"
	RuleRuleAdded          = "rule-added"
	RuleRuleRemoved        = "rule-removed"
	RuleOriginChanged      = "origin-changed"
	RuleFieldAdded         = "field-added"
	RuleFieldRemoved       = "field-removed"
	RuleCaptureAdded       = "capture-added"
	RuleCaptureRemoved     = "capture-removed"
	RuleCaptureMoved       = "capture-location-changed"
	RuleAlternativeAdded   = "alternative-added"
	RuleAlternativeRemoved = "alternative-removed"
	RuleTupleArityChanged  = "tuple-arity-changed"
	RuleKindChanged        = "kind-changed"
	RuleScalarChanged      = "scalar-changed"
	RuleReferenceChanged   = "reference-changed"
)

// Impact is one reason for the class of a change: the position in the entry's shape ("$" is the
// root, then ".fields.<name>", ".captures.<name>", "[<i>]" for tuple elements, ".element",
// ".value" and ".alternatives"), the class, and the rule that produced it.
type Impact struct {
	Path  string
	Class Class
	Rule  string
}

// classOf returns Breaking if any impact is Breaking, and Compatible otherwise.
func classOf(impacts []Impact) Class {
	for _, i := range impacts {
		if i.Class == Breaking {
			return Breaking
		}
	}
	return Compatible
}

func whole(rule string, class Class) []Impact {
	return []Impact{{Path: "$", Class: class, Rule: rule}}
}

// shapeImpacts lists how the canonical shape new differs from old. Equal subtrees are skipped,
// which keeps the walk proportional to the changed part of a shared graph.
func (c *schemaCanonicalizer) shapeImpacts(old, new *Shape, path string, out []Impact) []Impact {
	if c.compare(old, new) == 0 {
		return out
	}
	add := func(p, rule string, class Class) { out = append(out, Impact{Path: p, Class: class, Rule: rule}) }
	switch {
	case old.Kind != new.Kind:
		add(path, RuleKindChanged, Breaking)
		return out
	case old.Scalar != new.Scalar:
		add(path, RuleScalarChanged, Breaking)
	case old.Ref != new.Ref:
		add(path, RuleReferenceChanged, Breaking)
	}
	switch old.Kind {
	case "list", "optional":
		out = c.shapeImpacts(old.Element, new.Element, path+".element", out)
	case "node":
		out = c.shapeImpacts(old.Value, new.Value, path+".value", out)
		out = c.captureImpacts(old, new, path, out)
	case "tuple":
		if len(old.Elements) != len(new.Elements) {
			add(path, RuleTupleArityChanged, Breaking)
		}
		for i := range min(len(old.Elements), len(new.Elements)) {
			out = c.shapeImpacts(&old.Elements[i], &new.Elements[i], fmt.Sprintf("%s[%d]", path, i), out)
		}
	case "struct":
		out = c.fieldImpacts(old, new, path, out)
	case "join":
		out = c.alternativeImpacts(old, new, path, out)
	}
	return out
}

func (c *schemaCanonicalizer) fieldImpacts(old, new *Shape, path string, out []Impact) []Impact {
	oldFields := make(map[string]*Field, len(old.Fields))
	for i := range old.Fields {
		oldFields[old.Fields[i].Name] = &old.Fields[i]
	}
	seen := make(map[string]bool, len(new.Fields))
	for i := range new.Fields {
		f := &new.Fields[i]
		seen[f.Name] = true
		p := path + ".fields." + f.Name
		if o, ok := oldFields[f.Name]; ok {
			out = c.shapeImpacts(&o.Shape, &f.Shape, p, out)
		} else {
			out = append(out, Impact{Path: p, Class: Compatible, Rule: RuleFieldAdded})
		}
	}
	for i := range old.Fields {
		if name := old.Fields[i].Name; !seen[name] {
			out = append(out, Impact{Path: path + ".fields." + name, Class: Breaking, Rule: RuleFieldRemoved})
		}
	}
	return out
}

func (c *schemaCanonicalizer) captureImpacts(old, new *Shape, path string, out []Impact) []Impact {
	oldCaptures := make(map[string]*Capture, len(old.Captures))
	for i := range old.Captures {
		oldCaptures[old.Captures[i].Name] = &old.Captures[i]
	}
	seen := make(map[string]bool, len(new.Captures))
	for i := range new.Captures {
		n := &new.Captures[i]
		seen[n.Name] = true
		p := path + ".captures." + n.Name
		o, ok := oldCaptures[n.Name]
		if !ok {
			out = append(out, Impact{Path: p, Class: Compatible, Rule: RuleCaptureAdded})
			continue
		}
		if o.Side != n.Side || !slices.EqualFunc(o.Paths, n.Paths, slices.Equal[[]int]) {
			out = append(out, Impact{Path: p, Class: Breaking, Rule: RuleCaptureMoved})
		}
		out = c.shapeImpacts(&o.Shape, &n.Shape, p, out)
	}
	for i := range old.Captures {
		if name := old.Captures[i].Name; !seen[name] {
			out = append(out, Impact{Path: path + ".captures." + name, Class: Breaking, Rule: RuleCaptureRemoved})
		}
	}
	return out
}

// alternativeImpacts matches the canonical alternatives of both joins as multisets: an
// alternative without an equal on the other side was added or removed.
func (c *schemaCanonicalizer) alternativeImpacts(old, new *Shape, path string, out []Impact) []Impact {
	p := path + ".alternatives"
	used := make([]bool, len(old.Alternatives))
	for i := range new.Alternatives {
		found := false
		for j := range old.Alternatives {
			if !used[j] && c.compare(&old.Alternatives[j], &new.Alternatives[i]) == 0 {
				used[j], found = true, true
				break
			}
		}
		if !found {
			out = append(out, Impact{Path: p, Class: Breaking, Rule: RuleAlternativeAdded})
		}
	}
	for j := range old.Alternatives {
		if !used[j] {
			out = append(out, Impact{Path: p, Class: Breaking, Rule: RuleAlternativeRemoved})
		}
	}
	return out
}

func sortImpacts(impacts []Impact) {
	sort.SliceStable(impacts, func(i, j int) bool {
		if impacts[i].Path != impacts[j].Path {
			return impacts[i].Path < impacts[j].Path
		}
		return impacts[i].Rule < impacts[j].Rule
	})
}
