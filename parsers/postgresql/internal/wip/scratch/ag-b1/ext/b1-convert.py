# Extensions of the tree converter for group B1 (functions, casts, transforms, types, operator classes, ...).
# Executed in the namespace of cmp.py. See the hooks at the top of cmp.py.
import re as _re

# ---- Idents that are String nodes in the raw tree (generic Node fields) ----
STRING_NODE_FIELDS.add(('DefElem', 'arg'))                      # reserved_keyword, NonReservedWord_or_Sconst, ColId
STRING_NODE_FIELDS.add(('AlterExtensionContentsStmt', 'object'))  # name


def _children(x):
    return x.get('children', []) if isinstance(x, dict) else []


# ---- SignedNum: NumericOnly with a sign (the raw parser folds the sign into the number) ----
def _signed_num(node, fields, in_list):
    num = conv_node(fields['Number'], False)
    if not fields.get('Negative'):
        return num
    if 'Integer' in num:
        v = num['Integer'].get('ival', 0)
        return {'Integer': {'ival': -v} if v else {}}
    return {'Float': {'fval': '-' + num['Float']['fval']}}


NODE_HOOKS['SignedNum'] = _signed_num


# ---- DropBehavior: the keyword CASCADE or RESTRICT ----
TERMINAL_HOOKS['DropBehavior'] = lambda text, in_list: 'DROP_CASCADE' if text.lstrip().lower().startswith('cascade') else 'DROP_RESTRICT'

# ---- NONE in the argument types of an operator (a NULL list element: {}), and an empty list node (NIL: {}) ----
NODE_HOOKS['NoneArg'] = lambda node, fields, in_list: {}


def _node_list(node, fields, in_list):
    items = _children(fields.get('Items'))
    if not items:
        return {}
    return {'List': {'items': [conv_node(c, True) for c in items]}}


NODE_HOOKS['NodeList'] = _node_list


# ---- DefElemK: a DefElem with a constant name ----
def _defelemk(node, fields, in_list):
    n2 = dict(node)
    n2['type'] = 'DefElem'
    r = conv_node(n2, in_list)
    a = r['DefElem'].get('arg')
    if a == {'Boolean': {}}:   # a Node field prints false as {"boolval": false} (inside an A_Const it is {})
        r['DefElem']['arg'] = {'Boolean': {'boolval': False}}
    return r


NODE_HOOKS['DefElemK'] = _defelemk


# ---- Aggregate arguments: aggr_args and extractArgTypes ----
def _aggr_list(af):
    """(the first list of aggr_args as a list of FunctionParameter nodes, the integer of the second element)"""
    args = _children(af.get('Args'))
    ordered = _children(af.get('OrderedArgs'))
    if af.get('Star'):
        return [], -1
    if not af.get('OrderBy'):
        return args, -1
    if not args:
        return ordered, 0
    # makeOrderedSetArgs: a VARIADIC last direct argument absorbs the single VARIADIC ordered one
    if args[-1]['fields'].get('Mode') == 'FUNC_PARAM_VARIADIC':
        return args, len(args)
    return args + ordered, len(args)


def _arg_types(params):
    """extractArgTypes: the types of the parameters that are not OUT or TABLE"""
    return [conv_node(p['fields']['ArgType'], True) for p in params if p['fields'].get('Mode') not in ('FUNC_PARAM_OUT', 'FUNC_PARAM_TABLE')]


def _aggr_args(node, fields, in_list):
    flist, n = _aggr_list(fields)
    first = {'List': {'items': [conv_node(p, True) for p in flist]}} if flist else {}
    return [first, {'Integer': {'ival': n}} if n else {'Integer': {}}]


NODE_HOOKS['AggrArgs'] = _aggr_args


# ---- ObjectWithArgs: objargs is computed from the arguments ----
def _object_with_args(node, fields, in_list):
    out = {}
    if fields.get('Objname'):
        out['objname'] = conv_field('ObjectWithArgs', 'objname', 'Objname', fields['Objname'])
    if fields.get('Aggr'):
        params, _ = _aggr_list(fields['Aggr']['fields'])
        objargs = _arg_types(params)
        funcargs = params
    else:
        funcargs = _children(fields.get('Objfuncargs'))
        if fields.get('Objargs'):
            objargs = [conv_node(c, True) for c in _children(fields['Objargs'])]
        else:
            objargs = _arg_types(funcargs)
    if objargs:
        out['objargs'] = objargs
    if funcargs:
        out['objfuncargs'] = [conv_node(p, True) for p in funcargs]
    if fields.get('ArgsUnspecified'):
        out['args_unspecified'] = True
    return {'ObjectWithArgs': out}


NODE_HOOKS['ObjectWithArgs'] = _object_with_args


# ---- Fields of a node that are converted before the generic conversion ----
def _prepare(tname, fix):
    """NODE_HOOKS[tname]: fix(fields) changes a copy of the fields, then the node is converted generically"""
    def hook(node, fields, in_list):
        n2 = dict(node)
        f2 = dict(fields)
        fix(f2)
        n2['fields'] = f2
        h = NODE_HOOKS.pop(tname)
        try:
            return conv_node(n2, in_list)
        finally:
            NODE_HOOKS[tname] = h
    NODE_HOOKS[tname] = hook


def _drop_default(f):
    if not f.get('Behavior'):
        f['Behavior'] = 'DROP_RESTRICT'


_prepare('DropStmt', _drop_default)


# ---- Sconst fields that are plain strings in the raw tree ----
def _plain_sconst(tname, names):
    def hook(node, fields, in_list):
        n2 = dict(node)
        f2 = dict(fields)
        for k in names:
            v = f2.get(k)
            if isinstance(v, dict) and v.get('type') == 'Sconst':
                f2[k] = sconst_value(v['text'])
        n2['fields'] = f2
        h = NODE_HOOKS.pop(tname)
        try:
            return conv_node(n2, in_list)
        finally:
            NODE_HOOKS[tname] = h
    NODE_HOOKS[tname] = hook


_plain_sconst('CreateConversionStmt', ['ForEncodingName', 'ToEncodingName'])
_plain_sconst('AlterEnumStmt', ['OldVal', 'NewVal', 'NewValNeighbor'])


# ---- The text of keywords: object types, variable names ----
def _words(text):
    text = _re.sub(r'/\*.*?\*/', ' ', text, flags=_re.S)
    text = _re.sub(r'--[^\n]*', ' ', text)
    return ' '.join(text.lower().split())


_OBJTYPES = {
    'table': 'TABLE', 'sequence': 'SEQUENCE', 'view': 'VIEW', 'materialized view': 'MATVIEW', 'index': 'INDEX',
    'foreign table': 'FOREIGN_TABLE', 'collation': 'COLLATION', 'conversion': 'CONVERSION', 'statistics': 'STATISTIC_EXT',
    'text search parser': 'TSPARSER', 'text search dictionary': 'TSDICTIONARY', 'text search template': 'TSTEMPLATE',
    'text search configuration': 'TSCONFIGURATION', 'access method': 'ACCESS_METHOD', 'event trigger': 'EVENT_TRIGGER',
    'extension': 'EXTENSION', 'foreign data wrapper': 'FDW', 'language': 'LANGUAGE', 'procedural language': 'LANGUAGE',
    'publication': 'PUBLICATION', 'schema': 'SCHEMA', 'server': 'FOREIGN_SERVER', 'database': 'DATABASE', 'role': 'ROLE',
    'subscription': 'SUBSCRIPTION', 'tablespace': 'TABLESPACE', 'aggregate': 'AGGREGATE', 'cast': 'CAST',
    'domain': 'DOMAIN', 'function': 'FUNCTION', 'operator': 'OPERATOR', 'operator class': 'OPCLASS',
    'operator family': 'OPFAMILY', 'procedure': 'PROCEDURE', 'routine': 'ROUTINE', 'transform': 'TRANSFORM', 'type': 'TYPE',
}
TERMINAL_HOOKS['ObjectTypeTok'] = lambda text, in_list: 'OBJECT_' + _OBJTYPES[_words(text)]


def _var_name(text, in_list):
    text = _re.sub(r'/\*.*?\*/', ' ', text, flags=_re.S)
    text = _re.sub(r'--[^\n]*', ' ', text)
    parts = _re.findall(r'"(?:[^"]|"")*"|[^\s.]+', text)
    return '.'.join(ident_value(p) for p in parts)


TERMINAL_HOOKS['VarName'] = _var_name
