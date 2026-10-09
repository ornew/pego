"""Compares the trees of pgdev (engine nodes) with the reference trees.

usage: cmp.py CORPUS.jsonl.gz OUT.jsonl [-v N] [-cat PATTERN]
"""
import gzip, json, re, sys, collections

sys.path.insert(0, '/Users/s27814/src/github.com/ornew/pego/.claude/worktrees/agent-a84132ee088937fd9/.refsql/lib/python3.12/site-packages')
import pglast.enums as E

PYI = '/Users/s27814/src/github.com/ornew/pego/.claude/worktrees/agent-a84132ee088937fd9/.refsql/lib/python3.12/site-packages/pglast/ast.pyi'

# ---- the schema of the raw tree: fields of each node and their types ----
schema = {}
cur = None
for line in open(PYI):
    m = re.match(r'class (\w+)\((\w+)\):', line)
    if m:
        cur = m.group(1)
        schema[cur] = {}
        continue
    m = re.match(r'    (\w+): (.*)$', line)
    if m and cur and not m.group(1).startswith('_') and m.group(1) != 'ancestors':
        name = m.group(1)
        typ = m.group(2).replace(' | None', '').replace('enums.', '')
        schema[cur][name] = typ


def camel(r):
    r = r.rstrip('_') if r.endswith('_') and r not in ('_',) else r
    parts = r.split('_')
    return ''.join(p[:1].upper() + p[1:] for p in parts)


go2raw = {t: {camel(f): f.rstrip('_') for f in fs} for t, fs in schema.items()}
enum_default = {}
for t, fs in schema.items():
    for f, typ in fs.items():
        for modname in ('parsenodes', 'primnodes', 'nodes', 'lockoptions', 'xml', 'cmptype'):
            mod = getattr(E, modname, None)
            if mod and hasattr(mod, typ) and isinstance(getattr(mod, typ), type):
                en = getattr(mod, typ)
                try:
                    first = list(en)[0]
                    enum_default[(t, f.rstrip('_'))] = first.name
                except Exception:
                    pass
                break

IGNORED = {'location', 'rexpr_list_start', 'rexpr_list_end', 'list_start', 'list_end', 'stmt_len', 'stmt_location',
           'conninfo_location', 'name_location', 'arg_location', 'payload_location'}


# ---- decoding of tokens ----
def ident_value(text):
    if text[:1] == '"':
        return text[1:-1].replace('""', '"')
    if text[:2] in ('U&', 'u&'):
        return text  # not decoded here
    s = ''.join(c.lower() if 'A' <= c <= 'Z' else c for c in text)
    b = s.encode()
    if len(b) > 63:
        s = b[:63].decode(errors='ignore')
    return s


def sconst_value(text):
    # concatenates the parts; handles ', E'', $$ and U&'' (without escapes of U&)
    res = []
    i = 0
    n = len(text)
    while i < n:
        c = text[i]
        if c in 'eE' and text[i + 1:i + 2] == "'":
            j, s = read_e(text, i + 2)
            res.append(s)
            i = j
        elif c == "'":
            j, s = read_q(text, i + 1)
            res.append(s)
            i = j
        elif c in 'uU' and text[i + 1:i + 3] == "&'":
            j, s = read_q(text, i + 3)
            res.append(s)  # TODO escapes
            i = j
        elif c == '$':
            m = re.match(r'\$([A-Za-z_\x80-\U0010ffff][A-Za-z0-9_\x80-\U0010ffff]*)?\$', text[i:])
            tag = m.group(0)
            k = text.index(tag, i + len(tag))
            res.append(text[i + len(tag):k])
            i = k + len(tag)
        else:
            # white space and comments between the parts, or UESCAPE
            if text[i:].lstrip()[:7].lower() == 'uescape':
                break
            i += 1
    return ''.join(res)


def read_q(t, i):
    out = []
    while True:
        j = t.index("'", i)
        out.append(t[i:j])
        if t[j + 1:j + 2] == "'":
            out.append("'")
            i = j + 2
        else:
            return j + 1, ''.join(out)


def read_e(t, i):
    out = []
    n = len(t)
    while i < n:
        c = t[i]
        if c == "'":
            if t[i + 1:i + 2] == "'":
                out.append("'")
                i += 2
                continue
            return i + 1, ''.join(out)
        if c == '\\':
            d = t[i + 1]
            if d in '01234567':
                m = re.match(r'[0-7]{1,3}', t[i + 1:])
                out.append(chr(int(m.group(0), 8)))
                i += 1 + len(m.group(0))
            elif d == 'x' and re.match(r'[0-9a-fA-F]', t[i + 2:i + 3]):
                m = re.match(r'[0-9a-fA-F]{1,2}', t[i + 2:])
                out.append(chr(int(m.group(0), 16)))
                i += 2 + len(m.group(0))
            elif d == 'u':
                out.append(chr(int(t[i + 2:i + 6], 16)))
                i += 6
            elif d == 'U':
                out.append(chr(int(t[i + 2:i + 10], 16)))
                i += 10
            else:
                out.append({'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t'}.get(d, d))
                i += 2
            continue
        out.append(c)
        i += 1
    raise ValueError('unterminated')


def int_value(text):
    t = text.replace('_', '')
    if t[:2] in ('0x', '0X'):
        return int(t[2:], 16)
    if t[:2] in ('0o', '0O'):
        return int(t[2:], 8)
    if t[:2] in ('0b', '0B'):
        return int(t[2:], 2)
    return int(t)


# Fields of the AST that hold an Ident but are String nodes in the raw tree (generic nodes).
STRING_NODE_FIELDS = set()


def conv_value(v, tname=None, fname=None):
    """converts a field value"""
    if v is None:
        return None
    if isinstance(v, dict):
        return conv_node(v, in_list=False, tname=tname, fname=fname)
    return v


# Extension points, filled by the files of the ext directory next to this script (see AGENTS.md):
#   TERMINAL_HOOKS[type] = fn(text, in_list) -> value        (a terminal of a grammar type)
#   NODE_HOOKS[type] = fn(node, fields, in_list) -> value     (a struct node: fields is node['fields'])
#   POST_HOOKS.append(fn(dict) -> dict or None)               (applied to every converted dict, children first)
TERMINAL_HOOKS = {}
NODE_HOOKS = {}
POST_HOOKS = []


def conv_node(n, in_list, tname=None, fname=None):
    t = n['type']
    if t in TERMINAL_HOOKS:
        return TERMINAL_HOOKS[t](n.get('text'), in_list)
    if t in NODE_HOOKS:
        return NODE_HOOKS[t](n, n.get('fields', {}), in_list)
    if t == 'List':
        return [conv_node(c, True) if isinstance(c, dict) else c for c in n.get('children', [])]
    text = n.get('text')
    if t == 'Ident':
        if in_list or (tname, fname) in STRING_NODE_FIELDS:
            return {'String': {'sval': ident_value(text)}}
        return ident_value(text)
    if t == 'OpTok':
        v = '<>' if text == '!=' else text
        return {'String': {'sval': v}}
    if t == 'Sconst':
        return {'String': {'sval': sconst_value(text)}}
    if t == 'Iconst':
        v = int_value(text)
        return {'Integer': {'ival': v} if v else {}}
    if t == 'Fconst':
        return {'Float': {'fval': text}}
    if t == 'Bconst':
        return {'BitString': {'bsval': 'b' + text[2:-1]}}
    if t == 'Xconst':
        return {'BitString': {'bsval': 'x' + text[2:-1]}}
    if t == 'Param':
        return int(text[1:])
    fields = n.get('fields', {})
    if t == 'String':
        return {'String': {'sval': fields.get('Sval')}} if fields.get('Sval') else {'String': {}}
    if t == 'Integer':
        return {'Integer': {'ival': fields.get('Ival')}} if fields.get('Ival') else {'Integer': {}}
    if t == 'Boolean':
        return {'Boolean': {'boolval': True}} if fields.get('Boolval') else {'Boolean': {}}
    if t == 'A_Star':
        return {'A_Star': {}}
    if t == 'NodeList':
        return {'List': {'items': [conv_node(c, True) for c in fields.get('Items', {}).get('children', [])]}}
    if t == 'RangeFuncItem':
        cd = fields.get('Coldeflist')
        items = [conv_node(fields['Func'], True)]
        if cd and cd.get('children'):
            items.append({'List': {'items': [conv_node(c, True) for c in cd['children']]}})
        else:
            items.append({})
        return {'List': {'items': items}}
    if t == 'Seq' or t == 'Match' or t == 'Operator' or t == 'Error':
        return {'?' + t: n.get('text')}
    raw = t
    out = {}
    for gf, v in fields.items():
        rf = go2raw.get(raw, {}).get(gf)
        if rf is None:
            out['?' + gf] = conv_field(raw, gf, gf, v)
            continue
        if v == '':
            continue
        out[rf] = conv_field(raw, rf, gf, v)
    if raw == 'SelectStmt':
        d = out.pop('?Distinct', None)
        do = out.pop('?DistinctOn', None)
        if do:
            out['distinctClause'] = do
        elif d:
            out['distinctClause'] = [{}]
        if out.pop('?LimitWithTies', None):
            out['limitOption'] = 'LIMIT_OPTION_WITH_TIES'
        elif out.get('limitCount') or out.get('limitOffset'):
            out['limitOption'] = 'LIMIT_OPTION_COUNT'
    if raw == 'SQLValueFunction' and 'typmod' not in out:
        out['typmod'] = -1
    if raw == 'MergeSupportFunc':
        out['msftype'] = 25
    # enum defaults
    for (tt, ff), d in enum_default.items():
        if tt == raw and ff not in out:
            out[ff] = d
    return {raw: {k: v for k, v in out.items() if not empty(v)}}


def empty(v):
    return v is None or v is False or (v == 0 and not isinstance(v, bool)) or v == [] or v == {}


def conv_field(tname, rf, gf, v):
    if v is None:
        return None
    if isinstance(v, dict):
        if v.get('type') == 'List':
            return [conv_node(c, True, tname, gf) if isinstance(c, dict) else c for c in v.get('children', [])]
        if v.get('type') == 'Iconst' and schema.get(tname, {}).get(rf, '') in ('int', 'int | None'):
            return int_value(v['text'])
        r = conv_node(v, False, tname, rf)
        ft = schema.get(tname, {}).get(rf, '')
        if ft in schema and ft not in ('Node', 'Expr') and isinstance(r, dict) and len(r) == 1:
            return list(r.values())[0]
        return r
    return v


# ---- A_Const flattening and the other fixes of the raw tree ----
def post(x):
    if isinstance(x, list):
        return [post(i) for i in x]
    if isinstance(x, dict):
        r = {}
        for k, v in x.items():
            if k == 'A_Const':
                r[k] = fix_const(v)
            else:
                r[k] = post(v)
        if 'A_Expr' in r:
            e = r['A_Expr']
            if e.get('kind') == 'AEXPR_OP' and e.get('name') == [{'String': {'sval': '-'}}] and 'lexpr' not in e \
                    and isinstance(e.get('rexpr'), dict) and 'A_Const' in e['rexpr']:
                c = e['rexpr']['A_Const']
                if 'ival' in c:
                    v = c['ival'].get('ival', 0)
                    return {'A_Const': {'ival': {'ival': -v} if v else {}}}
                if 'fval' in c:
                    s = c['fval']['fval']
                    if s[:1] == '+':
                        s = s[1:]
                    s = s[1:] if s[:1] == '-' else '-' + s
                    return {'A_Const': {'fval': {'fval': s}}}
        if 'BoolExpr' in r:
            b = r['BoolExpr']
            if b.get('boolop') in ('AND_EXPR', 'OR_EXPR') and len(b.get('args', [])) == 2:
                a0 = b['args'][0]
                if isinstance(a0, dict) and 'BoolExpr' in a0 and a0['BoolExpr'].get('boolop') == b['boolop']:
                    b['args'] = a0['BoolExpr']['args'] + [b['args'][1]]
        if 'SelectStmt' in r:
            s = r['SelectStmt']
            if s.get('limitWithTies'):
                s['limitOption'] = 'LIMIT_OPTION_WITH_TIES'
            elif 'limitCount' in s or 'limitOffset' in s:
                s['limitOption'] = 'LIMIT_OPTION_COUNT'
            s.pop('limitWithTies', None)
        for h in POST_HOOKS:
            r2 = h(r)
            if r2 is not None:
                r = r2
        return r
    return x


def fix_const(c):
    c = dict(c)
    v = c.pop('val', None)
    if isinstance(v, str):
        c['sval'] = {'sval': v}
    elif v is not None:
        (k, body), = v.items()
        key = {'Integer': 'ival', 'Float': 'fval', 'String': 'sval', 'BitString': 'bsval', 'Boolean': 'boolval'}[k]
        c[key] = body
    return c


def strip_ref(x):
    if isinstance(x, list):
        return [strip_ref(i) for i in x]
    if isinstance(x, dict):
        return {k: strip_ref(v) for k, v in x.items() if k not in IGNORED and k != 'location'}
    return x


def first_diff(a, b, path=''):
    if type(a) != type(b):
        return path, a, b
    if isinstance(a, dict):
        for k in sorted(set(a) | set(b)):
            if k not in a:
                return path + '/' + k, None, b[k]
            if k not in b:
                return path + '/' + k, a[k], None
            d = first_diff(a[k], b[k], path + '/' + k)
            if d:
                return d
        return None
    if isinstance(a, list):
        if len(a) != len(b):
            return path + '[len]', len(a), len(b)
        for i, (x, y) in enumerate(zip(a, b)):
            d = first_diff(x, y, path + '[%d]' % i)
            if d:
                return d
        return None
    return None if a == b else (path, a, b)


def load_ext():
    import glob, os
    dirs = [os.path.join(os.path.dirname(os.path.abspath(__file__)), 'ext')]
    if os.environ.get('PGDIR'):
        dirs.append(os.path.join(os.environ['PGDIR'], 'ext'))
    for d in dirs:
        for f in sorted(glob.glob(os.path.join(d, '*.py'))):
            exec(open(f).read(), globals())


def main():
    load_ext()
    corpus = sys.argv[1]
    out = sys.argv[2]
    verbose = 0
    pat = None
    show_acc = 0
    shown_acc = 0
    acc_pat = None
    mode = 'rej'
    args = sys.argv[3:]
    while args:
        if args[0] == '-v':
            verbose = int(args[1]); args = args[2:]
        elif args[0] == '-rj':
            show_acc = int(args[1]); args = args[2:]
        elif args[0] == '-rjpat':
            acc_pat = re.compile(args[1]); args = args[2:]
        elif args[0] == '-rjmode':
            mode = args[1]; args = args[2:]
        elif args[0] == '-cat':
            pat = re.compile(args[1]); args = args[2:]
        else:
            args = args[1:]
    recs = [json.loads(l) for l in gzip.open(corpus, 'rt')]
    stats = collections.Counter()
    cats = collections.Counter()
    shown = 0
    for l in open(out):
        o = json.loads(l)
        r = recs[o['i']]
        stats['total'] += 1
        if o['ok'] != r['ok']:
            stats['accept_mismatch_' + ('mine_ok_ref_rej' if o['ok'] else 'mine_rej_ref_ok')] += 1
            if show_acc and shown_acc < show_acc:
                if (acc_pat is None or acc_pat.search(r['sql'])) and ((mode == 'rej') == (not o['ok'])):
                    shown_acc += 1
                    print('ACC', r['f'], r['l'], r['sql'][:200].replace('\n', ' '), '||', (r.get('err') or '')[:60], '||', (o.get('err') or '')[:100])
            continue
        if not o['ok']:
            stats['both_reject'] += 1
            continue
        stats['both_accept'] += 1
        if 'tree' not in o:
            continue
        mine = post(conv_node(o['tree'], False)) if False else None
        # the root is a Script of RawStmt
        root = o['tree']
        stmts = [s['fields']['Stmt'] for s in root['fields']['Stmts']['children']]
        mine = [post(conv_node(s, False)) for s in stmts]
        ref = strip_ref(r['tree'])
        mine = strip_ref(mine)
        d = first_diff(mine, ref)
        if d is None:
            stats['tree_equal'] += 1
        else:
            stats['tree_diff'] += 1
            key = re.sub(r'\[\d+\]', '[]', d[0])
            cats[key] += 1
            if verbose and shown < verbose and (pat is None or pat.search(key)):
                shown += 1
                print('---', r['f'], r['l'], r['sql'][:300].replace('\n', ' '))
                print('   at', d[0])
                print('   mine', json.dumps(d[1])[:300])
                print('   ref ', json.dumps(d[2])[:300])
    print(dict(stats))
    for k, v in cats.most_common(25):
        print(v, k)


main()
