# Extensions of the converter for the XML and JSON nodes (group D).
#
# The raw tree keeps an empty JsonAggConstructor ("constructor": {}) of JSON_OBJECTAGG and JSON_ARRAYAGG, but the
# generic conversion drops every empty value: put it back.

def _ag_d_constructor(d):
    for k in ('JsonObjectAgg', 'JsonArrayAgg'):
        if k in d:
            d[k].setdefault('constructor', {})
    return d

POST_HOOKS.append(_ag_d_constructor)


# A parenthesized query followed by clauses is a SelectStmt with only Larg in the AST ("(select 1) order by 1");
# the raw parser puts the clauses into the inner SelectStmt (insertSelectOptions). This is not specific to
# XML or JSON (it is seen inside JSON_ARRAY(query)); the hook is a no-op when the merge was done already.
def _ag_d_select_paren(d):
    s = d.get('SelectStmt') if len(d) == 1 else None
    if s is None or 'larg' not in s or 'rarg' in s or s.get('op', 'SETOP_NONE') != 'SETOP_NONE':
        return None
    inner = s['larg']   # a field of a node type is not wrapped
    if not isinstance(inner, dict):
        return None
    r = dict(inner)
    for k, v in s.items():
        if k in ('larg', 'op', 'limitOption'):
            continue
        r[k] = v
    if s.get('limitOption', 'LIMIT_OPTION_DEFAULT') != 'LIMIT_OPTION_DEFAULT':
        r['limitOption'] = s['limitOption']
    return {'SelectStmt': r}

POST_HOOKS.append(_ag_d_select_paren)
