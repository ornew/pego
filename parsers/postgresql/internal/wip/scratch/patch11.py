P = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/'


def sub(f, a, b):
    t = open(P + f).read()
    assert a in t, (f, a[:60])
    open(P + f, 'w').write(t.replace(a, b))


sub('parts/31-cexpr.pego', 'wg:within_group_clause? fc:filter_clause? ov:over_clause?', 'wg:within_group_ok? fc:filter_ok? ov:over_ok?')
sub('parts/31-cexpr.pego', 'def func_arg_expr: Expr = named_arg / a_expr', '''def func_arg_expr: Expr = [windowless = false] x:(named_arg / a_expr) -> $x
// WITHIN GROUP, FILTER and OVER are not allowed where a function call is windowless
def within_group_ok: []SortBy = [!windowless] x:within_group_clause -> $x
def filter_ok: Expr = [!windowless] x:filter_clause -> $x
def over_ok: WindowDef = [!windowless] x:over_clause -> $x''')
