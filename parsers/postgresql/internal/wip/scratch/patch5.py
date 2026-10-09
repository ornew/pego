P = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/parts/'
for num, name, n in (('60', 'tables', 'A'), ('61', 'objects', 'B'), ('62', 'utility', 'C')):
    open(P + '%s-%s.pego' % (num, name), 'w').write(
        '\n// stub: replaced by the statements of group %s\ntype StmtStub%s struct { }\ntype Stmt%s = StmtStub%s\n'
        'def stmt_%s: Stmt%s = "@@never@@" -> new StmtStub%s{}\n' % (n, n, n, n, n.lower(), n, n))
