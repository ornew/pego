# Converter extensions of group C2 (generic object statements): DROP, ALTER ... RENAME / OWNER / SET SCHEMA /
# DEPENDS, COMMENT, SECURITY LABEL, GRANT, REVOKE, ALTER DEFAULT PRIVILEGES. Executed in the namespace of cmp.py.


def _c2_words(text):
    """The words of a terminal that holds keywords: comments dropped, white space collapsed, lower case."""
    out = []
    i, n = 0, len(text or '')
    while i < n:
        if text.startswith('--', i):
            j = text.find('\n', i)
            i = n if j < 0 else j
        elif text.startswith('/*', i):
            d = 1
            i += 2
            while i < n and d:
                if text.startswith('/*', i):
                    d += 1
                    i += 2
                elif text.startswith('*/', i):
                    d -= 1
                    i += 2
                else:
                    i += 1
            out.append(' ')
        else:
            out.append(text[i])
            i += 1
    return ' '.join(''.join(out).lower().split())


# The words that name an object type, and the enumerator the raw parser makes of them (the others are
# OBJECT_ and the upper-cased word).
_C2_OBJECT_TYPES = {
    '': 'OBJECT_TABLE',  # GRANT ... ON name: a table
    'materialized view': 'OBJECT_MATVIEW',
    'foreign table': 'OBJECT_FOREIGN_TABLE',
    'conversion': 'OBJECT_CONVERSION',
    'statistics': 'OBJECT_STATISTIC_EXT',
    'text search parser': 'OBJECT_TSPARSER',
    'text search dictionary': 'OBJECT_TSDICTIONARY',
    'text search template': 'OBJECT_TSTEMPLATE',
    'text search configuration': 'OBJECT_TSCONFIGURATION',
    'access method': 'OBJECT_ACCESS_METHOD',
    'event trigger': 'OBJECT_EVENT_TRIGGER',
    'foreign data wrapper': 'OBJECT_FDW',
    'procedural language': 'OBJECT_LANGUAGE',
    'server': 'OBJECT_FOREIGN_SERVER',
    'foreign server': 'OBJECT_FOREIGN_SERVER',
    'operator class': 'OBJECT_OPCLASS',
    'operator family': 'OBJECT_OPFAMILY',
    'large object': 'OBJECT_LARGEOBJECT',
    'group': 'OBJECT_ROLE',
    'user': 'OBJECT_ROLE',
    'parameter': 'OBJECT_PARAMETER_ACL',
    # the kinds of ALTER DEFAULT PRIVILEGES
    'tables': 'OBJECT_TABLE',
    'functions': 'OBJECT_FUNCTION',
    'routines': 'OBJECT_FUNCTION',
    'sequences': 'OBJECT_SEQUENCE',
    'types': 'OBJECT_TYPE',
    'schemas': 'OBJECT_SCHEMA',
    'large objects': 'OBJECT_LARGEOBJECT',
    # GRANT ... ON ALL ... IN SCHEMA
    'all tables': 'OBJECT_TABLE',
    'all sequences': 'OBJECT_SEQUENCE',
    'all functions': 'OBJECT_FUNCTION',
    'all procedures': 'OBJECT_PROCEDURE',
    'all routines': 'OBJECT_ROUTINE',
}


def _c2_objtype(text, in_list):
    w = _c2_words(text)
    if w in _C2_OBJECT_TYPES:
        return _C2_OBJECT_TYPES[w]
    return 'OBJECT_' + w.upper().replace(' ', '_')


TERMINAL_HOOKS['ObjectType'] = _c2_objtype
TERMINAL_HOOKS['DropBehavior'] = lambda text, in_list: 'DROP_CASCADE' if _c2_words(text) == 'cascade' else 'DROP_RESTRICT'

# names that the raw parser stores as a String node and not as a plain string
for _f in ('CommentStmt', 'SecLabelStmt', 'RenameStmt', 'AlterOwnerStmt', 'AlterObjectSchemaStmt'):
    STRING_NODE_FIELDS.add((_f, 'object'))


# NONE in the argument types of an operator is a NULL in the list
NODE_HOOKS['NoneArg'] = lambda n, f, in_list: {}


def _c2_signednum(n, f, in_list):
    num = f['Number']
    neg = bool(f.get('Negative'))
    if num['type'] == 'Iconst':
        v = int_value(num['text'])
        v = -v if neg else v
        return {'Integer': {'ival': v} if v else {}}
    return {'Float': {'fval': ('-' + num['text']) if neg else num['text']}}


NODE_HOOKS['SignedNum'] = _c2_signednum


def _c2_paramname(n, f, in_list):
    parts = [ident_value(c['text']) for c in f['Names']['children']]
    return {'String': {'sval': '.'.join(parts)}}


NODE_HOOKS['ParamName'] = _c2_paramname


def _c2_defelem(name, arg):
    return {'DefElem': {'defname': name, 'arg': arg, 'defaction': 'DEFELEM_UNSPEC'}}


def _c2_roleopt(n, f, in_list):
    return _c2_defelem(ident_value(f['Name']['text']), {'Boolean': {'boolval': bool(f.get('Value'))}})


NODE_HOOKS['RoleOpt'] = _c2_roleopt


def _c2_defaclopt(n, f, in_list):
    s = (f.get('Schemas') or {}).get('children') or []
    if s:
        return _c2_defelem('schemas', {'List': {'items': [conv_node(c, True) for c in s]}})
    return _c2_defelem('roles', {'List': {'items': [conv_node(c, True) for c in f['Roles']['children']]}})


NODE_HOOKS['DefaclOpt'] = _c2_defaclopt


def _c2_objwithargs(n, f, in_list):
    """ObjectWithArgs: objargs is computed (extractArgTypes, extractAggrArgTypes), objfuncargs of an aggregate
    is its direct and ordered arguments (without the duplicate VARIADIC one of an ordered-set aggregate)."""
    t = 'ObjectWithArgs'

    def lst(node, tname, fname):
        if not node:
            return []
        return [conv_node(c, True, tname, fname) for c in node.get('children', [])]

    out = {}
    out['objname'] = lst(f.get('Objname'), t, 'Objname')
    funcargs = lst(f.get('Objfuncargs'), t, 'Objfuncargs')
    objargs = None
    aggr = f.get('Aggr')
    if aggr:
        af = aggr['fields']
        direct = lst(af.get('Args'), 'AggrArgs', 'Args')
        ordered = lst(af.get('OrderedArgs'), 'AggrArgs', 'OrderedArgs')
        if direct and ordered and direct[-1]['FunctionParameter'].get('mode') == 'FUNC_PARAM_VARIADIC':
            ordered = []
        funcargs = direct + ordered
    if f.get('Objargs'):
        objargs = lst(f['Objargs'], t, 'Objargs')
    else:
        objargs = [{'TypeName': p['FunctionParameter']['argType']} for p in funcargs
                   if p['FunctionParameter'].get('mode') not in ('FUNC_PARAM_OUT', 'FUNC_PARAM_TABLE')]
    if objargs:
        out['objargs'] = objargs
    if funcargs:
        out['objfuncargs'] = funcargs
    if f.get('ArgsUnspecified'):
        out['args_unspecified'] = True
    return {t: out}


NODE_HOOKS['ObjectWithArgs'] = _c2_objwithargs


def _c2_unwrap(r, key, fields):
    """String-valued fields of the raw tree that the grammar holds as a string constant (a String node)."""
    c = r.get(key)
    if c is None:
        return
    for fld in fields:
        v = c.get(fld)
        if isinstance(v, dict) and 'String' in v:
            if v['String'].get('sval'):
                c[fld] = v['String']['sval']
            else:
                del c[fld]  # an empty string is left out of the tree


def _c2_post(r):
    # the enumerators that the grammar leaves out (nil) are the first of their enumeration
    for k in ('DropStmt', 'GrantStmt', 'GrantRoleStmt', 'RenameStmt'):
        if k in r:
            r[k].setdefault('behavior', 'DROP_RESTRICT')
    if 'RenameStmt' in r:
        r['RenameStmt'].setdefault('relationType', 'OBJECT_ACCESS_METHOD')
    if 'AlterDefaultPrivilegesStmt' in r:
        a = r['AlterDefaultPrivilegesStmt'].get('action')
        if isinstance(a, dict):
            a.setdefault('behavior', 'DROP_RESTRICT')
    if 'CommentStmt' in r:
        _c2_unwrap(r, 'CommentStmt', ('comment',))
    if 'SecLabelStmt' in r:
        _c2_unwrap(r, 'SecLabelStmt', ('label', 'provider'))
    if 'AlterObjectDependsStmt' in r:
        c = r['AlterObjectDependsStmt']
        if isinstance(c.get('extname'), str):
            c['extname'] = {'sval': c['extname']}
    return None


POST_HOOKS.append(_c2_post)
