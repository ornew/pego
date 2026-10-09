P = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/parts/'


def sub(f, a, b):
    t = open(P + f).read()
    assert a in t, (f, a[:60])
    open(P + f, 'w').write(t.replace(a, b, 1))


open(P + '63-xmljson.pego', 'w').write('''
// stub: replaced by the XML and JSON expressions of group D
type ExprStubD struct { }
type ExprD = ExprStubD
type TableRefStubD struct { }
type TableRefD = TableRefStubD
// the XML and JSON functions with special syntax (func_expr_common_subexpr of gram.y)
def xmljson_expr: ExprD = "@@never@@" -> new ExprStubD{}
// json_aggregate_func with its FILTER and OVER clauses
def json_agg_expr: ExprD = "@@never@@" -> new ExprStubD{}
// xmltable and json_table with their alias, as the alternatives of table_ref
def xmljson_table: TableRefD = "@@never@@" -> new TableRefStubD{}
''')
sub('00-types.pego', 'TreatExpr | GroupingSet | MultiAssignRef | CurrentOfExpr', 'TreatExpr | GroupingSet | MultiAssignRef | CurrentOfExpr | ExprD')
sub('00-types.pego', 'type TableRef = RangeVar | RangeSubselect | RangeFunction | JoinExpr | RangeTableSample', 'type TableRef = RangeVar | RangeSubselect | RangeFunction | JoinExpr | RangeTableSample | TableRefD')
sub('32-special-funcs.pego', '    / merge_action_expr', '    / merge_action_expr / xmljson_expr')
sub('31-cexpr.pego', 'def func_expr: Expr = func_call_over', 'def func_expr: Expr = func_call_over / json_agg_expr')
sub('41-clauses.pego', 'def table_primary: TableRef = relation_table / sample_table / func_table_ref / subselect_table / paren_join_alias / paren_join',
    'def table_primary: TableRef = relation_table / sample_table / func_table_ref / subselect_table / paren_join_alias / paren_join / xmljson_table')
sub('41-clauses.pego', 'def func_expr_w: Expr = func_call_over / func_expr_common_subexpr', 'def func_expr_w: Expr = func_call_over / func_expr_common_subexpr / json_agg_expr')
