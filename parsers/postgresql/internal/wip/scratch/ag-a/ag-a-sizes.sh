#!/bin/sh
# prints the generated lines per rule of my parts (top 30)
W=/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work
D=$W/ag-a
$W/pego gen -g $D/postgresql.pego -pkg postgresql -types -recognize -nodoc -o $D/size-tmp.go
python3 - <<'EOF'
import re
W = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/ag-a'
src = open(W + '/size-tmp.go').read().split('\n')
funcs = []
cur = None
for i, l in enumerate(src):
    m = re.match(r'^func (\(\w+ \*?\w+\) )?(\w+)\(', l)
    if m:
        if cur:
            funcs.append((i - cur[1], cur[0]))
        cur = (m.group(2), i)
if cur:
    funcs.append((len(src) - cur[1], cur[0]))
mine = set()
for f in ('60-a-types', '60-a-create', '60-a-alter', '60-tables'):
    for l in open(W + '/parts/' + f + '.pego'):
        m = re.match(r'^def (\w+)', l)
        if m:
            mine.add(m.group(1))
tot = 0
rows = []
for n, name in funcs:
    base = re.sub(r'^(rule_|parse_|match_)', '', name)
    if base in mine or any(name.endswith(x) or name == x for x in mine):
        rows.append((n, name))
        tot += n
rows.sort(reverse=True)
print('my rules:', len(mine), 'matched functions:', len(rows), 'lines:', tot)
for n, name in rows[:30]:
    print(n, name)
EOF
rm -f $D/size-tmp.go
