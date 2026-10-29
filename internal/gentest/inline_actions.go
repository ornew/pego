package gentest

// An inline action (e => A) is an anonymous rule embedded at its position. Each case gives the
// grammar written with inline actions and the same grammar with every inline action written as a
// rule, named as the compiler names the hidden rule (r_inline_n). The two must behave the same
// on every input, on every backend.
type InlineCase struct {
	Name     string
	Inline   string
	Explicit string
	Inputs   []string
}

func InlineActionCases() []InlineCase {
	return []InlineCase{
		{"sequence", `type A = { X string }
rule main = (x:"a" "b" => A{X: text($x)}) "c"`, `type A = { X string }
rule main = _main_1 "c"
rule _main_1 = x:"a" "b" => A{X: text($x)}`, []string{"abc", "ab", "ac", "xbc", ""}},
		{"whole rule", `rule main = (x:"a" => $x)`, `rule main = _main_1
rule _main_1 = x:"a" => $x`, []string{"a", "b", "aa", ""}},
		{"choice", `rule main = (x:("a" / "b") => $x)`, `rule main = _main_1
rule _main_1 = x:("a" / "b") => $x`, []string{"a", "b", "c", ""}},
		{"choice of inline actions", `rule main = (x:"a" => $x) / (y:"b" => $y)`, `rule main = _main_1 / _main_2
rule _main_1 = x:"a" => $x
rule _main_2 = y:"b" => $y`, []string{"a", "b", "c", ""}},
		{"nested", `rule main = (x:(y:"a" => $y) "b" => $x)`, `rule main = _main_2
rule _main_1 = y:"a" => $y
rule _main_2 = x:_main_1 "b" => $x`, []string{"ab", "a", "b", ""}},
		{"outer capture stays", `type P = { L string; R string }
rule main = l:"a" r:(x:"b" => text($x)) => P{L: text($l), R: $r}`, `type P = { L string; R string }
rule main = l:"a" r:_main_1 => P{L: text($l), R: $r}
rule _main_1 = x:"b" => text($x)`, []string{"ab", "a", "b", ""}},
		{"default action", `rule main = (x:"a" "b" => ...) "c"`, `rule main = _main_1 "c"
rule _main_1 = x:"a" "b" => ...`, []string{"abc", "ab", ""}},
		{"repetition", `rule main = (x:"a" => $x)* "b"`, `rule main = _main_1* "b"
rule _main_1 = x:"a" => $x`, []string{"b", "ab", "aaab", "aaa", ""}},
		{"optional", `rule main = (x:"a" => $x)? "b"`, `rule main = _main_1? "b"
rule _main_1 = x:"a" => $x`, []string{"b", "ab", "aab", ""}},
		{"left recursion", `type Add = { L Expr; R string }
type Num = { T string }
type Expr ~ Add | Num
rule main = e $$
rule e Expr = (l:e "+" r:"a" => Add{L: $l, R: text($r)}) / "a" => Num{T: "a"}`, `type Add = { L Expr; R string }
type Num = { T string }
type Expr ~ Add | Num
rule main = e $$
rule e Expr = _e_1 / "a" => Num{T: "a"}
rule _e_1 = l:e "+" r:"a" => Add{L: $l, R: text($r)}`, []string{"a", "a+a", "a+a+a", "a+", ""}},
		{"cut", `rule main = (x:"a" #cut "b" => $x) / "a" "c"`, `rule main = _main_1 / "a" "c"
rule _main_1 = x:"a" #cut "b" => $x`, []string{"ab", "ac", "a", ""}},
		{"recovery", `rule main = (x:[a-z]+ #atomic ";" => $x)* $$ #recover(skip=[^;]+ ";")`, `rule main = _main_1* $$ #recover(skip=[^;]+ ";")
rule _main_1 = x:[a-z]+ #atomic ";" => $x`, []string{"ab;cd;", "ab;1x;cd;", "1x;", "ab"}},
		{"recovery inside", `rule main = (x:[a-z]+ #atomic ";" #recover(skip=[^;]+ ";") => $x)+ $$`, `rule main = _main_1+ $$
rule _main_1 = x:[a-z]+ #atomic ";" #recover(skip=[^;]+ ";") => $x`, []string{"ab;cd;", "ab;1x;cd;", "1x;", "ab"}},
		{"action error", `rule main = (x:"a" => 1 / 0) "b"`, `rule main = _main_1 "b"
rule _main_1 = x:"a" => 1 / 0`, []string{"ab", "a"}},
		{"recover argument", `rule main = (x:[a-z]+ #atomic ";")* $$ #recover(skip=(y:[^;]+ => text($y)) ";")`, `rule main = (x:[a-z]+ #atomic ";")* $$ #recover(skip=_main_1 ";")
rule _main_1 = y:[^;]+ => text($y)`, []string{"ab;cd;", "ab;1x;cd;", "1x;", "ab"}},
		{"stream", `rule main = (x:[a-z]+ #atomic ";" => text($x))* #stream`, `rule main = _main_1* #stream
rule _main_1 = x:[a-z]+ #atomic ";" => text($x)`, []string{"", "ab;", "ab;cd;", "ab"}},
		{"operators rule operand", `type N = { T string }
rule main = #operators {
    operand (t:[0-9]+ #atomic => N{T: text($t)})
    level { infix left "+" }
}`, `type N = { T string }
rule main = #operators {
    operand _main_1
    level { infix left "+" }
}
rule _main_1 = t:[0-9]+ #atomic => N{T: text($t)}`, []string{"1", "1+2", "1+2+3", "+", ""}},
		{"nested in a whole rule", `rule main = (x:(y:"a" => text($y)) "b" => $x)`, `rule main = _main_2
rule _main_1 = y:"a" => text($y)
rule _main_2 = x:_main_1 "b" => $x`, []string{"ab", "a", ""}},
		{"captures of the enclosing rule after", `rule main = a:"a" (b:"b" => $b) => $a`, `rule main = a:"a" _main_1 => $a
rule _main_1 = b:"b" => $b`, []string{"ab", "a", "b"}},
	}
}

// InlineActionGenCases returns the inline cases for the corpus that compares the backends and the
// generated parsers.
func InlineActionGenCases() []Case {
	var cases []Case
	for _, tc := range InlineActionCases() {
		cases = append(cases, Case{"inline action: " + tc.Name, tc.Inline, tc.Inputs})
	}
	return cases
}
