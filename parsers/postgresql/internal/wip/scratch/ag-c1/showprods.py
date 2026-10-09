import re,sys
text=open('/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/gram.condensed').read()
out={}
cur=None
for line in text.split('\n'):
    m=re.match(r'^([A-Za-z_][A-Za-z_0-9]*):(.*)$',line)
    if m:
        cur=m.group(1); out[cur]=[line]
    elif cur: out[cur].append(line)
for n in sys.argv[1:]:
    print('\n'.join(l for l in out.get(n,['MISSING '+n]) if l.strip()))
