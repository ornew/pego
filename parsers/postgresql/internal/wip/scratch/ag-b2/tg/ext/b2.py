# Converter extensions of group B2 (executed in the namespace of cmp.py).

# DropBehavior: the terminal holds CASCADE or RESTRICT (with the white space after it)
def _drop_behavior(text, in_list):
    w = re.match(r'[A-Za-z]+', text).group(0).lower()
    return 'DROP_CASCADE' if w == 'cascade' else 'DROP_RESTRICT'


TERMINAL_HOOKS['DropBehavior'] = _drop_behavior


# SignedNum: a sign and a number (NumericOnly, SignedIconst); the raw parser folds the sign into the value
def _signed_num(n, f, in_list):
    num = conv_node(f['Number'], False)
    neg = bool(f.get('Negative'))
    if 'Integer' in num:
        v = num['Integer'].get('ival', 0)
        v = -v if neg else v
        return {'Integer': {'ival': v} if v else {}}
    s = num['Float']['fval']
    return {'Float': {'fval': '-' + s if neg else s}}


NODE_HOOKS['SignedNum'] = _signed_num


# a source name that is the value of an option (a DefElem argument) is a String node
STRING_NODE_FIELDS.add(('DefElem', 'arg'))


# TrgArgNum: the number of a trigger's argument is a string (the decimal value of an integer, the text of a float)
def _trg_arg_num(n, f, in_list):
    v = f['Val']
    if v['type'] == 'Iconst':
        return {'String': {'sval': str(int_value(v['text']))}}
    return {'String': {'sval': v['text']}}


NODE_HOOKS['TrgArgNum'] = _trg_arg_num


# DefElem: the name is a String node when the parser makes it up (DefName = Ident | String), and a Boolean
# argument keeps its value false (the raw tree prints makeBoolean(false) with "boolval": false here)
def _defelem(r):
    e = r.get('DefElem')
    if e is not None:
        if isinstance(e.get('defname'), dict) and 'String' in e['defname']:
            e['defname'] = e['defname']['String'].get('sval', '')
        a = e.get('arg')
        if isinstance(a, dict) and a.get('Boolean') == {}:
            a['Boolean'] = {'boolval': False}
    return None


POST_HOOKS.append(_defelem)


# An enum field that the grammar leaves nil (an optional DropBehavior) gets its default, like an absent one
def _enum_defaults(r):
    for k, v in r.items():
        if isinstance(v, dict) and k in schema and k not in ('A_Const', 'DefElem'):
            for (tt, ff), d in enum_default.items():
                if tt == k and ff not in v:
                    v[ff] = d
    return None


POST_HOOKS.append(_enum_defaults)


# A string constant (Sconst) in a field that is a plain string in the raw tree (servertype, conninfo, location)
if '_conv_field_b2' not in globals():
    _conv_field_b2 = conv_field

    def conv_field(tname, rf, gf, v):
        r = _conv_field_b2(tname, rf, gf, v)
        if isinstance(r, dict) and list(r) == ['String'] and schema.get(tname, {}).get(rf, '') == 'str':
            return r['String'].get('sval') or None  # the raw tree has no empty strings
        return r
