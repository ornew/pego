P = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/'


def sub(f, a, b):
    t = open(P + f).read()
    assert a in t, (f, a[:60])
    open(P + f, 'w').write(t.replace(a, b))


sub('cmp.py', """        elif d:
            out['distinctClause'] = [{}]""", """        elif d:
            out['distinctClause'] = [{}]
        if out.pop('?LimitWithTies', None):
            out['limitOption'] = 'LIMIT_OPTION_WITH_TIES'
        elif 'limitCount' in out or 'limitOffset' in out:
            out['limitOption'] = 'LIMIT_OPTION_COUNT'""")
sub('cmp.py', "return v is None or v is False or v == 0 and not isinstance(v, bool) and False or v == [] or v == {}", "return v is None or v is False or (v == 0 and not isinstance(v, bool)) or v == [] or v == {}")
