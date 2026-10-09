"""Makes ag-a-cmp.py: cmp.py with the option -only FILE. A statement is of region A when the reference accepts it and
its top node is one of the nodes of the region (CreateStmt, ...), or when the reference rejects it and its text matches
the regex of FILE (the statements of region A the reference rejects). Only those are counted."""
W = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work'
s = open(W + '/cmp.py').read()
s = s.replace("    acc_pat = None\n", "    acc_pat = None\n    only = None\n", 1)
s = s.replace("        elif args[0] == '-cat':", "        elif args[0] == '-only':\n            only = re.compile(open(args[1]).read().strip(), re.I | re.S); args = args[2:]\n        elif args[0] == '-cat':", 1)
s = s.replace("        r = recs[o['i']]\n", """        r = recs[o['i']]
        if only and not in_region(r, only):
            continue
""", 1)
s = s.replace("def main():", '''REGION_NODES = {'CreateStmt', 'RefreshMatViewStmt', 'CreateSeqStmt', 'AlterSeqStmt', 'CreateStatsStmt', 'AlterStatsStmt',
                'IndexStmt', 'ViewStmt', 'AlterTableStmt', 'AlterTableMoveAllStmt', 'CreateDomainStmt', 'AlterDomainStmt',
                'CreateForeignTableStmt', 'CreateTableAsStmt'}


def in_region(r, only):
    if r['ok']:
        t = r['tree']
        # the statement is a list of raw statements; the top nodes are the keys of the dicts
        tops = [k for st in (t if isinstance(t, list) else [t]) if isinstance(st, dict) for k in st]
        if 'CreateTableAsStmt' in tops:
            return not any(st['CreateTableAsStmt'].get('is_select_into') for st in (t if isinstance(t, list) else [t]) if 'CreateTableAsStmt' in st)
        return any(k in REGION_NODES for k in tops)
    return bool(only.search(r['sql']))


def main():''', 1)
# a table by the node of the statement: how many the reference accepts, the grammar accepts, the trees equal
s = s.replace("    stats = collections.Counter()\n", "    stats = collections.Counter()\n    bytype = collections.defaultdict(collections.Counter)\n", 1)
s = s.replace("        stats['total'] += 1\n", """        stats['total'] += 1
        _t = r.get('tree')
        _top = next(iter(_t[0])) if r['ok'] and isinstance(_t, list) and _t and isinstance(_t[0], dict) else '(rejected by the reference)'
        if _top == 'CreateTableAsStmt':
            _top = 'CreateTableAsStmt/' + _t[0]['CreateTableAsStmt'].get('objtype', '')
        bytype[_top]['statements'] += 1
        if o['ok']:
            bytype[_top]['grammar accepts'] += 1
""", 1)
s = s.replace("        if d is None:\n            stats['tree_equal'] += 1\n", "        if d is None:\n            stats['tree_equal'] += 1\n            bytype[_top]['trees equal'] += 1\n", 1)
s = s.replace("    print(dict(stats))\n", "    print(dict(stats))\n    for k in sorted(bytype):\n        print('BYTYPE', k, dict(bytype[k]))\n", 1)
assert 'in_region(r, only)' in s and "'-only'" in s and 'BYTYPE' in s and "bytype[_top]['trees equal']" in s
open(W + '/ag-a/ag-a-cmp.py', 'w').write(s)
