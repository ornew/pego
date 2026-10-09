W = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/'
R = '/Users/s27814/src/github.com/ornew/pego/.claude/worktrees/agent-a84132ee088937fd9/'


def sub(f, a, b):
    t = open(f).read()
    assert a in t, (f, a[:60])
    open(f, 'w').write(t.replace(a, b, 1))


sub(W + 'parts/32-special-funcs.pego', '''def substr_for: []Expr = a:a_expr FOR b:a_expr -> list($a, new A_Const{Val: new Integer{Ival: 1}}, $b)
def substr_similar: []Expr = a:a_expr SIMILAR b:a_expr ESCAPE c:a_expr
    -> list($a, new FuncCall{Funcname: list(new String{Sval: "pg_catalog"}, new String{Sval: "similar_to_escape"}), Args: list($b, $c), Funcformat: "COERCE_EXPLICIT_CALL"})''',
    '''def substr_for: []Expr = a:a_expr FOR b:a_expr
    -> list($a, new A_Const{Val: new Integer{Ival: 1}}, new TypeCast{Arg: $b, TypeName: new TypeName{Names: list(new String{Sval: "pg_catalog"}, new String{Sval: "int4"}), Typemod: -1}})
def substr_similar: []Expr = a:a_expr SIMILAR b:a_expr ESCAPE c:a_expr -> list($a, $b, $c)''')
p = R + 'parsers/postgresql/rawjson.go'
sub(p, '''func strNode(s string) any {
	if s == "" {
		return map[string]any{"String": map[string]any{}}
	}
	return map[string]any{"String": map[string]any{"sval": s}}
}''', '''func strNode(s string) any {
	return map[string]any{"String": map[string]any{"sval": s}}
}''')
sub(p, 'var postHooks map[string]func(c *converter, body, extras map[string]any) any', 'var postHooks map[string]func(c *converter, v reflect.Value, body, extras map[string]any) any')
sub(p, 'postHooks = map[string]func(c *converter, body, extras map[string]any) any{', 'postHooks = map[string]func(c *converter, v reflect.Value, body, extras map[string]any) any{')
t = open(p).read()
t = t.replace('func(c *converter, body, extras map[string]any) any {', 'func(c *converter, v reflect.Value, body, extras map[string]any) any {')
t = t.replace('func hookAExpr(c *converter, body, extras map[string]any) any {', 'func hookAExpr(c *converter, v reflect.Value, body, extras map[string]any) any {')
t = t.replace('func hookBoolExpr(c *converter, body, extras map[string]any) any {', 'func hookBoolExpr(c *converter, v reflect.Value, body, extras map[string]any) any {')
t = t.replace('''		"SQLValueFunction": func(c *converter, v reflect.Value, body, extras map[string]any) any {
			if _, ok := body["typmod"]; !ok {
				body["typmod"] = int64(-1)
			}
			return nil
		},''', '''		"SQLValueFunction": func(c *converter, v reflect.Value, body, extras map[string]any) any {
			if v.FieldByName("Typmod").IsNil() {
				body["typmod"] = int64(-1)
			}
			return nil
		},''')
t = t.replace('h(c, body, extras)', 'h(c, v, body, extras)')
open(p, 'w').write(t)
