P = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/parts/'


def sub(f, a, b):
    t = open(P + f).read()
    assert a in t, (f, a[:60])
    open(P + f, 'w').write(t.replace(a, b))


sub('00-types.pego', 'type ColumnDef struct { Colname Ident, TypeName TypeName, CollClause *CollateClause }\n', '')
sub('01-types2.pego', 'type ObjectWithArgs struct { Objname []Ident, Objfuncargs []FunctionParameter, ArgsUnspecified bool }',
    '''type ObjectWithArgs struct {
    Objname []Str, Objargs []ObjArg, Objfuncargs []FunctionParameter, ArgsUnspecified bool, Aggr *AggrArgs
}''')
sub('41-clauses.pego', 'def table_func_element: ColumnDef = n:ColId t:Typename c:opt_collate_clause? -> new ColumnDef{Colname: $n, TypeName: $t, CollClause: $c}',
    'def table_func_element: ColumnDef = n:ColId t:Typename c:opt_collate_clause? -> new ColumnDef{Colname: $n, TypeName: $t, CollClause: $c, IsLocal: true}')
open(P + '60-tables.pego', 'w').write('''
// stub: replaced by the statements of group A
type StmtStubA struct { }
type StmtA = StmtStubA
type ColumnDef struct { Colname Ident, TypeName TypeName, CollClause *CollateClause, IsLocal bool }
def stmt_a: StmtA = "@@never@@" -> new StmtStubA{}
''')
