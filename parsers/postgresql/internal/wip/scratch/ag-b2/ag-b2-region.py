"""Counts, for the statements of the corpus that match a regex, how many the reference accepts, how many the
grammar accepts and how many trees are equal.

usage: python ag-b2-region.py CORPUS.jsonl.gz OUT.jsonl REGEX [-show N]
(run with the python of the reference; PGDIR must name the directory with ext/)
"""
import sys, os, re, json, gzip

W = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work'
src = open(W + '/cmp.py').read().rsplit('\nmain()', 1)[0]
corpus, out, rx = sys.argv[1], sys.argv[2], re.compile(sys.argv[3])
show = int(sys.argv[5]) if len(sys.argv) > 5 and sys.argv[4] == '-show' else 0
sys.argv = ['cmp.py']
exec(src)
load_ext()
recs = [json.loads(l) for l in gzip.open(corpus, 'rt')]
n = ref_ok = mine_ok = both_ok = equal = 0
shown = 0
for l in open(out):
    o = json.loads(l)
    r = recs[o['i']]
    if not rx.search(r['sql']):
        continue
    n += 1
    ref_ok += bool(r['ok'])
    mine_ok += bool(o['ok'])
    if r['ok'] and o['ok']:
        both_ok += 1
        stmts = [s['fields']['Stmt'] for s in o['tree']['fields']['Stmts']['children']]
        mine = strip_ref([post(conv_node(s, False)) for s in stmts])
        d = first_diff(mine, strip_ref(r['tree']))
        if d is None:
            equal += 1
        elif shown < show:
            shown += 1
            print('DIFF', r['f'], r['l'], r['sql'][:200].replace('\n', ' '), d[0], json.dumps(d[1])[:150], json.dumps(d[2])[:150])
    elif r['ok'] != o['ok'] and shown < show:
        shown += 1
        print('ACC', r['f'], r['l'], 'ref', r['ok'], 'mine', o['ok'], r['sql'][:200].replace('\n', ' '), (o.get('err') or '')[:80])
print('statements %d: reference accepts %d, grammar accepts %d, both accept %d, trees equal %d' % (n, ref_ok, mine_ok, both_ok, equal))
