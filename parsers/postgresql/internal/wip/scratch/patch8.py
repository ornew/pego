P = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/parts/'


def sub(f, a, b):
    t = open(P + f).read()
    assert a in t, (f, a[:60])
    open(P + f, 'w').write(t.replace(a, b))


sub('41-clauses.pego', 'def paren_join_alias: ParenJoin = -LP j:joined_table -RP a:alias_clause -> new ParenJoin{Join: $j, Alias: $a}',
    '''def paren_join_alias: JoinExpr = -LP j:joined_table -RP a:alias_clause
    -> new JoinExpr{Jointype: $j.Jointype, IsNatural: $j.IsNatural, Larg: $j.Larg, Rarg: $j.Rarg, UsingClause: $j.UsingClause, JoinUsingAlias: $j.JoinUsingAlias, Quals: $j.Quals, Alias: $a}''')
sub('00-types.pego', 'type ParenJoin struct { Join JoinExpr, Alias Alias }\n', '')
sub('00-types.pego', ' | RangeTableSample | ParenJoin', ' | RangeTableSample')
