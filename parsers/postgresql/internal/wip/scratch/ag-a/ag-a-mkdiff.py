"""Writes D/ag-a-core-changes.diff: the changes of region A to the files of the core (main/parts)."""
import difflib, os
W = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work'
out = []
for f in sorted(os.listdir(W + '/main/parts')):
    a = open(W + '/main/parts/' + f).read().split('\n')
    p = W + '/ag-a/parts/' + f
    if not os.path.exists(p):
        continue
    b = open(p).read().split('\n')
    if a != b and f not in ('60-tables.pego',):
        out.extend(difflib.unified_diff(a, b, 'main/parts/' + f, 'ag-a/parts/' + f, lineterm=''))
open(W + '/ag-a/ag-a-core-changes.diff', 'w').write('\n'.join(out) + '\n')
print(len(out), 'lines')
