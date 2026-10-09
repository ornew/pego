import re

W = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/'


def sub(f, a, b):
    t = open(f).read()
    assert a in t, (f, a[:60])
    open(f, 'w').write(t.replace(a, b, 1))


# merge group D
import shutil
shutil.copy(W + 'ag-d/parts/63-xmljson.pego', W + 'parts/63-xmljson.pego')
shutil.copy(W + 'ag-d/ext/ag-d-xmljson.py', W + 'ext/ag-d-xmljson.py')

d = open(W + 'ag-d/parts/30-expr.pego').read().split('\n')
a_lines = []
b_lines = []
# the pasted lines of D's 30-expr.pego: those that begin the IS DOCUMENT/NORMALIZED/JSON postfix operators
for i, l in enumerate(d):
    s = l.strip()
    if s.startswith('postfix IS DOCUMENT') or s.startswith('postfix IS NOT DOCUMENT') or s.startswith('postfix IS NORMALIZED') \
       or s.startswith('postfix IS f:unicode_normal_form') or s.startswith('postfix IS NOT NORMALIZED') \
       or s.startswith('postfix IS NOT f:unicode_normal_form') or s.startswith('postfix IS t:json_is_tail') \
       or s.startswith('postfix IS NOT t:json_is_tail'):
        (a_lines if len(a_lines) < 8 else b_lines).append(l)
assert len(a_lines) == 8 and len(b_lines) == 2, (len(a_lines), len(b_lines))
t = open(W + 'parts/30-expr.pego').read()
k1 = '        infix none IS NOT DISTINCT FROM -> new A_Expr{Kind: "AEXPR_NOT_DISTINCT"'
parts = t.split(k1)
assert len(parts) == 3, len(parts)
# first occurrence is a_expr, the second b_expr; append the lines after the line that holds the marker
def add(chunk_after, lines):
    nl = chunk_after.index('\n')
    return chunk_after[:nl + 1] + '\n'.join(lines) + '\n' + chunk_after[nl + 1:]
t = parts[0] + k1 + add(parts[1], a_lines) + k1 + add(parts[2], b_lines)
open(W + 'parts/30-expr.pego', 'w').write(t)

sub(W + 'parts/10-lexical.pego', 'def bare_text: Ident = quoted_ident / uident / !label_only_word word',
    '// FORMAT followed by JSON is the token FORMAT_LA of gram.y (the start of FORMAT JSON), not a label\ndef bare_text: Ident = quoted_ident / uident / !label_only_word !(FORMAT JSON) word')
sub(W + 'parts/40-select.pego', 'len($lc) + len($oc) > 0', 'len(text($lc)) + len(text($oc)) > 0')
sub(W + 'parts/41-clauses.pego', 'def relation_table: RangeVar = r:relation_alias !TABLESAMPLE -> $r',
    'def relation_table: RangeVar = !(ROWS FROM LP) r:relation_alias !TABLESAMPLE -> $r')
old_alias = '( al:alias_clause / -AS -LP cd:table_func_element_list -RP / -AS al:alias_name_only -LP cd:table_func_element_list -RP / al:alias_name_only -LP cd:table_func_element_list -RP )?'
new_alias = '( -AS -LP cd:table_func_element_list -RP / -AS al:alias_name_only -LP cd:table_func_element_list -RP / al:alias_name_only -LP cd:table_func_element_list -RP / al:alias_clause )?'
t = open(W + 'parts/41-clauses.pego').read()
assert t.count(old_alias) == 2
t = t.replace(old_alias, new_alias)
open(W + 'parts/41-clauses.pego', 'w').write(t)
sub(W + 'parts/41-clauses.pego', 'def func_expr_w: Expr = func_call_over / func_expr_common_subexpr / json_agg_expr',
    'def func_expr_w: Expr = func_call_over / func_special_w / json_agg_expr\ndef func_special_w: Expr = [windowless = false] f:func_expr_common_subexpr -> $f')
