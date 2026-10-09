# usage: stats.py CORPUS.jsonl.gz OUT.jsonl
# Counts, for the statements of group C2 (by the root node of the reference tree, or by the first keywords when the
# reference rejects), how many the reference accepts, how many the grammar accepts and how many trees are equal.
import sys, gzip, json, collections, os, re

W = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work'
corpus, outf = sys.argv[1], sys.argv[2]
verbose = int(sys.argv[3]) if len(sys.argv) > 3 else 0
src = open(W + '/cmp.py').read().rsplit('\nmain()', 1)[0]
sys.argv = ['cmp.py']
g = {'__name__': 'cmpmod', '__file__': W + '/cmp.py'}
exec(compile(src, 'cmp.py', 'exec'), g)
os.environ['PGDIR'] = W + '/ag-c2'
g['load_ext']()
MY = {'DropStmt', 'RenameStmt', 'AlterObjectSchemaStmt', 'AlterOwnerStmt', 'AlterObjectDependsStmt', 'CommentStmt',
      'SecLabelStmt', 'GrantStmt', 'GrantRoleStmt', 'AlterDefaultPrivilegesStmt'}
recs = [json.loads(l) for l in gzip.open(corpus, 'rt')]
cnt = collections.Counter()
per = collections.defaultdict(collections.Counter)
shown = 0
for l in open(outf):
    o = json.loads(l)
    r = recs[o['i']]
    if not r['ok']:
        continue
    root = list(r['tree'][0].keys())[0] if isinstance(r['tree'], list) and r['tree'] else None
    if root is None:
        t = r['tree']
        root = list(t[0].keys())[0] if isinstance(t, list) else None
    if root not in MY:
        continue
    # DROP CAST, DROP TRANSFORM and DROP OPERATOR CLASS/FAMILY are DropStmt nodes of other regions
    if re.match(r'\s*drop\s+(operator\s+(class|family)|cast|transform)\b', r['sql'], re.I) or \
            (root == 'AlterOwnerStmt' and False):
        cnt['other_region_dropstmt'] += 1
        continue
    cnt['ref_accepts'] += 1
    per[root]['ref'] += 1
    if not o['ok']:
        cnt['mine_rejects'] += 1
        per[root]['rej'] += 1
        if verbose and shown < verbose:
            shown += 1
            print('REJ', r['f'], r['l'], r['sql'][:200].replace('\n', ' '), '||', (o.get('err') or '')[:100])
        continue
    cnt['mine_accepts'] += 1
    stmts = [s['fields']['Stmt'] for s in o['tree']['fields']['Stmts']['children']]
    mine = [g['post'](g['conv_node'](s, False)) for s in stmts]
    ref = g['strip_ref'](r['tree'])
    mine = g['strip_ref'](mine)
    d = g['first_diff'](mine, ref)
    if d is None:
        cnt['tree_equal'] += 1
        per[root]['eq'] += 1
    else:
        cnt['tree_diff'] += 1
        per[root]['diff'] += 1
        if verbose and shown < verbose:
            shown += 1
            print('DIFF', r['f'], r['l'], r['sql'][:200].replace('\n', ' '))
            print('   at', d[0], '\n   mine', json.dumps(d[1])[:200], '\n   ref ', json.dumps(d[2])[:200])
print(dict(cnt))
for k, v in sorted(per.items()):
    print(k, dict(v))
