# Extensions of the tree converter for region A (tables, indexes, sequences, views, domains).
# Executed in the namespace of cmp.py.

# ---- a signed number in an option (NumericOnly, SignedIconst): the sign is folded into the value ----
def _signed_num(node, fields, in_list):
    num = fields['Number']
    neg = bool(fields.get('Negative'))
    if num['type'] == 'Iconst':
        v = int_value(num['text'])
        v = -v if neg else v
        return {'Integer': {'ival': v}} if v else {'Integer': {}}
    s = num['text']
    return {'Float': {'fval': ('-' + s) if neg else s}}


NODE_HOOKS.setdefault('SignedNum', _signed_num)


# ---- CASCADE or RESTRICT ----
def _drop_behavior(text, in_list):
    w = (text or '').strip().split()[0].lower() if (text or '').strip() else ''
    return 'DROP_CASCADE' if w == 'cascade' else 'DROP_RESTRICT'


TERMINAL_HOOKS.setdefault('DropBehavior', _drop_behavior)

# ---- names that the parser makes up are String nodes in the grammar, plain strings in the raw tree;
# the names of the storage and compression of an ALTER TABLE command are String nodes in the raw tree ----
STRING_NODE_FIELDS.add(('AlterTableCmd', 'def'))
# a reserved keyword as the value of an option (WITH (x = on)) is a String node too
STRING_NODE_FIELDS.add(('DefElem', 'arg'))

_UNWRAP = {('ColumnDef', 'compression'), ('ColumnDef', 'storage_name'), ('IndexStmt', 'accessMethod'),
           ('Constraint', 'access_method'), ('DefElem', 'defname')}


def _unwrap_strings(r):
    for tname, fields in r.items():
        if isinstance(fields, dict):
            for f in list(fields):
                if (tname, f) in _UNWRAP:
                    v = fields[f]
                    if isinstance(v, dict) and 'String' in v:
                        fields[f] = v['String'].get('sval', '')
    return None


POST_HOOKS.append(_unwrap_strings)


# ---- defaults the raw tree has and the grammar leaves out ----
def _fill(r):
    for tname in ('AlterTableCmd', 'AlterDomainStmt'):
        f = r.get(tname)
        if isinstance(f, dict) and 'behavior' not in f:
            f['behavior'] = 'DROP_RESTRICT'
    # a Boolean that is the value of an option prints its false (an A_Const does not)
    f = r.get('DefElem')
    if isinstance(f, dict) and isinstance(f.get('arg'), dict) and f['arg'].get('Boolean') == {}:
        f['arg'] = {'Boolean': {'boolval': False}}
    return None


POST_HOOKS.append(_fill)
