package compile

import (
	"slices"
	"testing"
)

func TestStartRules(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src    string
		starts []string
		def    string
	}{
		{"@start\nrule a = b\nrule b = \"x\"\n", []string{"a"}, "a"},
		{"rule a = \"x\"\n@start\nrule b = a\n", []string{"b"}, "b"},
		{"@start(default)\nrule a = b\nrule b = \"x\"\n", []string{"a"}, "a"},
		{"@start\nrule a = b\n@start(default)\nrule b = \"x\"\n", []string{"a", "b"}, "b"},
		{"@start(default)\nrule a = b c\n@start\nrule b = \"x\"\n@start\nrule c = \"y\"\n", []string{"a", "b", "c"}, "a"},
		// Comments may stand between the attribute and the rule.
		{"@start // the entry\n// The rule.\nrule a = \"x\"\n", []string{"a"}, "a"},
	} {
		prog, err := compileStarts(t, tc.src)
		if err != nil {
			t.Errorf("%q: %v", tc.src, err)
			continue
		}
		if !slices.Equal(prog.StartRules, tc.starts) || prog.DefaultStartRule != tc.def {
			t.Errorf("%q: starts %v default %q, want %v %q", tc.src, prog.StartRules, prog.DefaultStartRule, tc.starts, tc.def)
		}
	}
}

func TestStartRuleErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, want string }{
		{"rule a = b\nrule b = \"x\"\n", "1:1: the grammar has no start rule: mark each rule where parsing may begin with @start"},
		{"package p\n", "the grammar has no start rule: mark each rule where parsing may begin with @start"},
		{"@start\nrule a = b\n@start\nrule b = \"x\"\n", "3:1: several start rules (a, b) and no default: mark one with @start(default)"},
		{"@start\nrule a = b c\n@start\nrule b = \"x\"\n@start\nrule c = \"y\"\n", "3:1: several start rules (a, b, c) and no default: mark one with @start(default)"},
		{"@start(default)\nrule a = b\n@start(default)\nrule b = \"x\"\n", "3:1: more than one default start rule: a and b"},
		{"@start @start\nrule a = \"x\"\n", "1:8: duplicate @start on rule a"},
		{"@start\n@start(default)\nrule a = \"x\"\n", "2:1: duplicate @start on rule a"},
		{"@start(main)\nrule a = \"x\"\n", "1:1: @start: the only argument is default"},
		{"@start(\"default\")\nrule a = \"x\"\n", "1:1: @start: the only argument is default"},
		{"@start(default, default)\nrule a = \"x\"\n", "1:1: @start: the only argument is default"},
		{"@start(default=default)\nrule a = \"x\"\n", "1:1: @start: the only argument is default"},
		{"@start\n@entry\nrule a = \"x\"\n", "2:1: unknown declaration attribute @entry"},
		// Other errors come first, and the start rules are reported only
		// once the grammar is otherwise valid.
		{"rule a = b\n", "1:10: undefined rule b"},
	} {
		_, err := compileStarts(t, tc.src)
		if err == nil || err.Error() != tc.want {
			t.Errorf("%q\n got %v\nwant %s", tc.src, err, tc.want)
		}
	}
}
