"""Counts, for the statements of the corpus that use the constructs of group D, how many the reference accepts, how
many the grammar accepts and how many trees are equal.   usage: python ag-d-regionstats.py D [CORPUS.jsonl.gz]
Reads D/out.jsonl (written by the last run of pgrun.sh D all) and uses the converter of cmp.py (with the extensions of D/ext)."""
import sys, os, re, gzip, json, collections

W = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work'
D = sys.argv[1]
corpus = sys.argv[2] if len(sys.argv) > 2 else W + '/full.jsonl.gz'
src = open(W + '/cmp.py').read()
src = src[:src.rindex('\nmain()')]
os.environ['PGDIR'] = D
sys.argv = ['cmp.py']
g = {'__name__': 'cmp', '__file__': W + '/cmp.py'}
exec(compile(src, W + '/cmp.py', 'exec'), g)
g['load_ext']()
conv_node, post, strip_ref, first_diff = g['conv_node'], g['post'], g['strip_ref'], g['first_diff']

pat = re.compile(r'(?i)(\bxmlconcat\b|\bxmlelement\b|\bxmlexists\b|\bxmlforest\b|\bxmlparse\b|\bxmlpi\b|\bxmlroot\b|\bxmlserialize\b|'
                 r'\bjson_object\b|\bjson_array\b|\bjson_scalar\b|\bjson_serialize\b|\bjson_query\b|\bjson_exists\b|\bjson_value\b|'
                 r'\bjson_objectagg\b|\bjson_arrayagg\b|\bxmltable\b|\bjson_table\b|\bis\s+(not\s+)?document\b|'
                 r'\bis\s+(not\s+)?(nfc\s+|nfd\s+|nfkc\s+|nfkd\s+)?normalized\b|\bis\s+(not\s+)?json\b|\bjson\s*\()')
recs = [json.loads(l) for l in gzip.open(corpus, 'rt')]
outs = {}
for l in open(D + '/out.jsonl'):
    o = json.loads(l)
    outs[o['i']] = o
groups = {'select': collections.Counter(), 'other': collections.Counter()}
files = collections.defaultdict(collections.Counter)
bad = []
for i, r in enumerate(recs):
    if not pat.search(r['sql']):
        continue
    k = 'select' if re.match(r'(?is)\s*(select|with|values|table|\()', r['sql']) else 'other'
    c = groups[k]
    o = outs[i]
    c['total'] += 1
    c['ref_accepts'] += 1 if r['ok'] else 0
    c['mine_accepts'] += 1 if o['ok'] else 0
    if o['ok'] != r['ok']:
        c['accept_mismatch'] += 1
        if k == 'select':
            bad.append((r['f'], r['l'], r['sql'][:150].replace('\n', ' '), o.get('err', '')[:80]))
        continue
    if not o['ok']:
        c['both_reject'] += 1
        continue
    root = o['tree']
    stmts = [s['fields']['Stmt'] for s in root['fields']['Stmts']['children']]
    mine = strip_ref([post(conv_node(s, False)) for s in stmts])
    ref = strip_ref(r['tree'])
    if first_diff(mine, ref) is None:
        c['tree_equal'] += 1
    else:
        c['tree_diff'] += 1
        if k == 'select':
            bad.append((r['f'], r['l'], r['sql'][:150].replace('\n', ' '), 'TREE DIFF ' + str(first_diff(mine, ref))[:200]))
for k, c in groups.items():
    print(k, dict(c))
for b in bad:
    print('  ', b)
