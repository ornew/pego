P = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/'
t = open(P + 'cmp.py').read()
a = t.index('def load_ext():')
b = t.index('def main():')
t = t[:a] + '''def load_ext():
    import glob, os
    dirs = [os.path.join(os.path.dirname(os.path.abspath(__file__)), 'ext')]
    if os.environ.get('PGDIR'):
        dirs.append(os.path.join(os.environ['PGDIR'], 'ext'))
    for d in dirs:
        for f in sorted(glob.glob(os.path.join(d, '*.py'))):
            exec(open(f).read(), globals())


''' + t[b:]
open(P + 'cmp.py', 'w').write(t)
