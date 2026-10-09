p = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/cmp.py'
t = open(p).read()
t = t.replace("""def post(x):
    if isinstance(x, list):
        return [post(i) for i in x]
    if isinstance(x, dict):
        r = {}
        for k, v in x.items():
            if k == 'A_Const':
                r[k] = fix_const(v)
            else:
                r[k] = post(v)
        return r
    return x
""", """def post(x):
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
            if e.get('kind') == 'AEXPR_OP' and e.get('name') == [{'String': {'sval': '-'}}] and 'lexpr' not in e \\
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
        return r
    return x
""")
t = t.replace("""            stats['accept_mismatch_' + ('mine_ok_ref_rej' if o['ok'] else 'mine_rej_ref_ok')] += 1
            continue""", """            stats['accept_mismatch_' + ('mine_ok_ref_rej' if o['ok'] else 'mine_rej_ref_ok')] += 1
            if show_acc and shown_acc < show_acc:
                if (acc_pat is None or acc_pat.search(r['sql'])) and ((mode == 'rej') == (not o['ok'])):
                    shown_acc += 1
                    print('ACC', r['f'], r['l'], r['sql'][:200].replace('\\n', ' '), '||', (r.get('err') or '')[:60], '||', (o.get('err') or '')[:100])
            continue""")
t = t.replace("""    verbose = 0
    pat = None""", """    verbose = 0
    pat = None
    show_acc = 0
    shown_acc = 0
    acc_pat = None
    mode = 'rej'""")
t = t.replace("""        elif args[0] == '-cat':""", """        elif args[0] == '-acc':
            show_acc = int(args[1]); args = args[2:]
        elif args[0] == '-accpat':
            acc_pat = re.compile(args[1]); args = args[2:]
        elif args[0] == '-accmode':
            mode = args[1]; args = args[2:]
        elif args[0] == '-cat':""")
open(p, 'w').write(t)
