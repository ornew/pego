import re, sys
src = open('/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/ref-sql/lib/python3.12/site-packages/pglast/ast.pyi').read()
classes = {}
cur = None
for line in src.split('\n'):
    m = re.match(r'class (\w+)\((\w+)\):', line)
    if m:
        cur = m.group(1)
        classes[cur] = []
        continue
    m = re.match(r'    (\w+): (.*)$', line)
    if m and cur and not m.group(1).startswith('_') and m.group(1) != 'ancestors':
        classes[cur].append((m.group(1), m.group(2)))
if len(sys.argv) > 1:
    names = sys.argv[1:]
else:
    names = list(classes)
for n in names:
    if n not in classes:
        print(n, '??')
        continue
    print(n + ': ' + ', '.join(f + ' ' + t.replace(' | None', '?').replace('enums.', '') for f, t in classes[n]))
