# Extensions of the tree converter for group C1 (utility statements). Executed in the namespace of cmp.py.


def _default(tname, node, fields, in_list):
    """converts a node by the generic rules of cmp.py with the given (preprocessed) fields"""
    h = NODE_HOOKS.pop(tname)
    try:
        n = dict(node)
        n['fields'] = fields
        return conv_node(n, in_list)
    finally:
        NODE_HOOKS[tname] = h


def _signed_int(v):
    """the integer of an Iconst, a SignedNum or an Integer node of the engine's tree"""
    t = v['type']
    if t == 'Iconst':
        return int_value(v['text'])
    if t == 'Integer':
        return v['fields'].get('Ival', 0)
    f = v['fields']
    n = int_value(f['Number']['text'])
    return -n if f.get('Negative') else n


# the number of NumericOnly and SignedIconst: the sign is folded into the value
def _signed_num(node, fields, in_list):
    num = fields['Number']
    neg = fields.get('Negative')
    if num['type'] == 'Iconst':
        v = int_value(num['text'])
        v = -v if neg else v
        return {'Integer': {'ival': v} if v else {}}
    s = num['text']
    if neg:
        s = s[1:] if s[:1] == '-' else '-' + s
    return {'Float': {'fval': s}}


NODE_HOOKS['SignedNum'] = _signed_num

# CASCADE or RESTRICT
TERMINAL_HOOKS['DropBehavior'] = lambda text, in_list: \
    'DROP_CASCADE' if text.strip().lower().startswith('cascade') else 'DROP_RESTRICT'

# a string constant that is a plain string in the raw tree
TERMINAL_HOOKS['SconstStr'] = lambda text, in_list: \
    {'String': {'sval': sconst_value(text)}} if in_list else sconst_value(text)

# the value of an option is a String node, also when the grammar has a name
STRING_NODE_FIELDS.add(('DefElem', 'arg'))


def _defelem(node, fields, in_list):
    n = fields.get('Defname')
    if isinstance(n, dict) and n.get('type') == 'String':
        fields = dict(fields)
        fields['Defname'] = n['fields'].get('Sval', '')
    return _default('DefElem', node, fields, in_list)


NODE_HOOKS['DefElem'] = _defelem


def _dotted(tname):
    def hook(node, fields, in_list):
        n = fields.get('Name')
        if isinstance(n, dict) and n.get('type') == 'List':
            fields = dict(fields)
            parts = [ident_value(c['text']) if c['type'] == 'Ident' else c['fields'].get('Sval', '')
                     for c in n.get('children', [])]
            fields['Name'] = '.'.join(parts)
        return _default(tname, node, fields, in_list)
    return hook


NODE_HOOKS['VariableSetStmt'] = _dotted('VariableSetStmt')
NODE_HOOKS['VariableShowStmt'] = _dotted('VariableShowStmt')


def _fetch(node, fields, in_list):
    c = fields.get('Count')
    if c:
        fields = dict(fields)
        del fields['Count']
        fields['HowMany'] = _signed_int(c)
    else:
        fields = {k: v for k, v in fields.items() if k != 'Count'}
    return _default('FetchStmt', node, fields, in_list)


NODE_HOOKS['FetchStmt'] = _fetch


# DROP_RESTRICT, the first enumerator of DropBehavior, is not filled in by cmp.py
def _truncate(node, fields, in_list):
    if not fields.get('Behavior'):
        fields = dict(fields)
        fields['Behavior'] = 'DROP_RESTRICT'
    return _default('TruncateStmt', node, fields, in_list)


NODE_HOOKS['TruncateStmt'] = _truncate
