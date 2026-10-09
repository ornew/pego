import glob, os, re
D = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/ag-c2/cases'
n = 0
for f in sorted(glob.glob(D + '/*.sql')):
    if os.path.basename(f).startswith('unbal-'):
        continue
    keep = []
    for line in open(f).read().split('\n'):
        stripped = re.sub(r"'[^']*'|\"[^\"]*\"", '', line)
        if not line.startswith('--') and stripped.count('(') != stripped.count(')') and line.rstrip().endswith(';') and ('/*' not in line):
            n += 1
            open(D + '/unbal-%02d.sql' % n, 'w').write(line + '\n')
        else:
            keep.append(line)
    open(f, 'w').write('\n'.join(keep))
print(n)
