P = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/'


def sub(f, a, b):
    t = open(P + f).read()
    assert a in t, (f, a[:60])
    open(P + f, 'w').write(t.replace(a, b))


sub('parts/41-clauses.pego', '''def joined_table: TableRef = first:table_primary rest:join_part+
    -> foldl($first, $rest, (acc, i) => new JoinExpr{Jointype: $i.Jointype, IsNatural: $i.IsNatural, Larg: $acc, Rarg: $i.Rarg, UsingClause: $i.UsingClause, JoinUsingAlias: $i.JoinUsingAlias, Quals: $i.Quals})''',
    '''def joined_table: JoinExpr = first:table_primary p:join_part rest:join_part*
    -> foldl(new JoinExpr{Jointype: $p.Jointype, IsNatural: $p.IsNatural, Larg: $first, Rarg: $p.Rarg, UsingClause: $p.UsingClause, JoinUsingAlias: $p.JoinUsingAlias, Quals: $p.Quals},
        $rest, (acc, i) => new JoinExpr{Jointype: $i.Jointype, IsNatural: $i.IsNatural, Larg: $acc, Rarg: $i.Rarg, UsingClause: $i.UsingClause, JoinUsingAlias: $i.JoinUsingAlias, Quals: $i.Quals})''')
sub('parts/41-clauses.pego', 'def paren_join: TableRef = -LP j:joined_table -RP -> $j', 'def paren_join: JoinExpr = -LP j:joined_table -RP -> $j')
sub('cmp.py', """    if raw == 'SQLValueFunction' and 'typmod' not in out:""", """    if raw == 'SelectStmt':
        d = out.pop('?Distinct', None)
        do = out.pop('?DistinctOn', None)
        if do:
            out['distinctClause'] = do
        elif d:
            out['distinctClause'] = [{}]
    if raw == 'SQLValueFunction' and 'typmod' not in out:""")
