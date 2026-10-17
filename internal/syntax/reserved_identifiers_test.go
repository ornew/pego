package syntax

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestReservedIdentifierPrefix(t *testing.T) {
	for _, src := range []string{
		`package _example; rule main = "x"`,
		`rule _main = "x"`, `rule _ = "x"`, `type _Node terminal`,
		`rule main = _helper`, `rule main = _capture:"x"`,
		`rule main = _:"x"`,
		`rule main = __proto__:"x"`,
		`rule main = "x" @_attr`, `rule main = "x" @error(_message="x")`,
		`rule main = {_indent := 0}`, `rule main = {_indent > 0}`, `rule main = {_ := 0}`,
		`rule main = "x" => $_capture`, `rule main = v1:"x" => _helper($v1)`,
		`rule main = v1:"x" => Node{_field: $v1}`, `rule main = v1:"x" => $v1._field`,
		`rule main = v1:"x" => map($v1, (_item) => _item)`,
		`rule main = v1:"x" => map($v1, (_) => $v1)`,
		`rule main = level(expr, _tight)`, `rule main = level(_expr, tight)`,
		`rule expr = operators { operand "x"; level _tight { postfix "!" } }`,
		`rule expr = operators { operand "x"; level _ { postfix "!" } }`,
	} {
		t.Run(src, func(t *testing.T) {
			g, err := Parse(src)
			if err != nil {
				t.Fatalf("reserved name must remain parseable: %s: %v", src, err)
			}
			if err := grammar.Validate(g); err == nil || !strings.Contains(err.Error(), "reserved") {
				t.Fatalf("reserved name must fail validation: %s: %v", src, err)
			}
			if _, errs := ParsePartial(src); len(errs) != 0 {
				t.Fatalf("reservation must not produce syntax diagnostics: %v", errs)
			}
		})
	}
	for _, src := range []string{`rule main = _`, `rule main = _ #atomic`, `rule main = _|_`, `rule main = "_private"`, `rule main = [_a]`, `rule user_name = "x"`, `rule main = {user_name := 0} {user_name == 0}`} {
		if _, err := Parse(src); err != nil {
			t.Fatalf("%s: %v", src, err)
		}
	}
	for _, name := range []string{"_", "__", "_x", "_1", "_名前"} {
		if IsIdentifier(name) {
			t.Errorf("available reserved name: %s", name)
		}
	}
	if !IsIdentifier("a_b") {
		t.Fatal("internal underscore rejected")
	}
}
