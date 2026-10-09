"""Applies to a copy of the core (D/chk/parts) the changes that group D proposes for the core (see the report), so that the
cases that depend on them can be run: python3 ag-d-corefixes.py D/chk. Also writes D/core-changes.diff (the changes against main)."""
import sys, difflib, os

W = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work'
C = sys.argv[1]


def sub(fn, old, new, count=1):
    p = C + '/parts/' + fn
    s = open(p).read()
    assert s.count(old) == count, (fn, old, s.count(old))
    open(p, 'w').write(s.replace(old, new))


# 1. a quoted identifier starts a c_ident
sub('31-cexpr.pego', 'def c_ident: Expr = &ident_start x:', 'def c_ident: Expr = &(ident_start / "\\"") x:')
# 2. ROWS FROM ( is not a table named rows
sub('41-clauses.pego', 'def relation_table: RangeVar = r:relation_alias !TABLESAMPLE -> $r',
    'def relation_table: RangeVar = !(ROWS FROM LP) r:relation_alias !TABLESAMPLE -> $r')
# 3. window functions are allowed in the arguments of the special functions of FROM (windowless only for the call itself)
sub('41-clauses.pego', 'def func_expr_w: Expr = func_call_over / func_expr_common_subexpr / json_agg_expr',
    'def func_expr_w: Expr = func_call_over / func_special_w / json_agg_expr\n'
    'def func_special_w: Expr = [windowless = false] f:func_expr_common_subexpr -> $f')
# 4. an alias with a column definition list: try it before the alias with a column list
old = ('( al:alias_clause / -AS -LP cd:table_func_element_list -RP / -AS al:alias_name_only -LP cd:table_func_element_list -RP / '
       'al:alias_name_only -LP cd:table_func_element_list -RP )?')
new = ('( -AS -LP cd:table_func_element_list -RP / -AS al:alias_name_only -LP cd:table_func_element_list -RP / '
       'al:alias_name_only -LP cd:table_func_element_list -RP / al:alias_clause )?')
sub('41-clauses.pego', old, new, 2)
# 5. len() of a node counts its children: a LIMIT with only a constant has none
sub('40-select.pego', 'len($lk2) + len($lc) + len($oc) > 0]', 'len($lk2) + len(text($lc)) + len(text($oc)) > 0]')

out = []
for f in sorted(os.listdir(W + '/main/parts')):
    if f == '63-xmljson.pego':
        continue
    a = open(W + '/main/parts/' + f).read().split('\n')
    p = C + '/parts/' + f
    b = open(p).read().split('\n') if os.path.exists(p) else a
    out += list(difflib.unified_diff(a, b, 'main/parts/' + f, 'D/parts/' + f, lineterm='', n=1))
open(C + '/../core-changes.diff', 'w').write('\n'.join(out) + '\n')
