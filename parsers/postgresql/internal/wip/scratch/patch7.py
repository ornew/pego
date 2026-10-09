P = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/parts/'
t = open(P + '41-clauses.pego').read()
a = t.index('// A table reference. A join takes')
b = t.index('def table_primary:')
new = '''// A table reference: a table_primary followed by joins, folded to the left. The right operand of
// CROSS JOIN and NATURAL JOIN is a table_primary, that of a join with ON or USING any table_ref (in
// "a JOIN b JOIN c ON x ON y" the second JOIN belongs to b). A join part is a JoinExpr without its left
// operand.
def table_ref: TableRef = first:table_primary rest:join_part*
    -> foldl($first, $rest, (acc, i) => new JoinExpr{Jointype: $i.Jointype, IsNatural: $i.IsNatural, Larg: $acc, Rarg: $i.Rarg, UsingClause: $i.UsingClause, JoinUsingAlias: $i.JoinUsingAlias, Quals: $i.Quals})
// joined_table: a table_ref that has at least one join
def joined_table: TableRef = first:table_primary rest:join_part+
    -> foldl($first, $rest, (acc, i) => new JoinExpr{Jointype: $i.Jointype, IsNatural: $i.IsNatural, Larg: $acc, Rarg: $i.Rarg, UsingClause: $i.UsingClause, JoinUsingAlias: $i.JoinUsingAlias, Quals: $i.Quals})
def join_part: JoinExpr = join_cross / join_natural / join_qual
def join_cross: JoinExpr = CROSS JOIN r:table_primary -> new JoinExpr{Jointype: "JOIN_INNER", Rarg: $r}
def join_qual: JoinExpr = [jt = "JOIN_INNER"]
    (FULL [jt = "JOIN_FULL"] OUTER? / LEFT [jt = "JOIN_LEFT"] OUTER? / RIGHT [jt = "JOIN_RIGHT"] OUTER? / INNER)? JOIN r:table_ref
    (ON qe:a_expr / USING -LP un:name_list -RP ua:join_using_alias?)
    -> new JoinExpr{Jointype: jt, Rarg: $r, UsingClause: concat($un), JoinUsingAlias: $ua, Quals: $qe}
def join_using_alias: Alias = AS n:ColId -> new Alias{Aliasname: $n}
def join_natural: JoinExpr = [jt = "JOIN_INNER"] NATURAL
    (FULL [jt = "JOIN_FULL"] OUTER? / LEFT [jt = "JOIN_LEFT"] OUTER? / RIGHT [jt = "JOIN_RIGHT"] OUTER? / INNER)? JOIN r:table_primary
    -> new JoinExpr{Jointype: jt, IsNatural: true, Rarg: $r}

'''
t = t[:a] + new + t[b:]
t = t.replace('def paren_join: JoinExpr = -LP j:joined_table -RP -> $j', 'def paren_join: TableRef = -LP j:joined_table -RP -> $j')
open(P + '41-clauses.pego', 'w').write(t)
