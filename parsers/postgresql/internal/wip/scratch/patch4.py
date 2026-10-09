P = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/parts/'


def sub(f, a, b):
    t = open(P + f).read()
    assert a in t, (f, a[:60])
    open(P + f, 'w').write(t.replace(a, b))


sub('01-types2.pego', 'type SignedNum struct { Negative bool, Number Iconst | Fconst }', 'type Num = Iconst | Fconst\ntype SignedNum struct { Negative bool, Number Num }')
sub('00-types.pego', 'TreatExpr | GroupingSet', 'TreatExpr | GroupingSet | MultiAssignRef | CurrentOfExpr')
sub('00-types.pego', 'type Stmt = SelectStmt', '''type DmlStmt = SelectStmt | InsertStmt | UpdateStmt | DeleteStmt | MergeStmt
// Stmt: every statement. StmtA, StmtB and StmtC are the unions of the statements of the other files.
type Stmt = DmlStmt | StmtA | StmtB | StmtC''')
sub('00-types.pego', 'Ctematerialized string, Ctequery Stmt,', 'Ctematerialized string, Ctequery DmlStmt,')
sub('90-main.pego', 'def stmt_dml: Stmt = SelectStmt / InsertStmt / UpdateStmt / DeleteStmt / MergeStmt', 'def stmt_dml: DmlStmt = SelectStmt / InsertStmt / UpdateStmt / DeleteStmt / MergeStmt')
sub('50-dml.pego', 'def PreparableStmt: Stmt = stmt_dml', 'def PreparableStmt: DmlStmt = stmt_dml')
for num, name, n in (('60', 'tables', 'A'), ('61', 'objects', 'B'), ('62', 'utility', 'C')):
    open(P + '%s-%s.pego' % (num, name), 'w').write('\n// stub: replaced by the statements of group %s\ntype StmtStub%s struct { }\ntype Stmt%s = StmtStub%s\ndef stmt_%s: Stmt%s = _|_\n' % (n, n, n, n, n.lower(), n))
