import shutil

W = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/'


def sub(f, a, b, count=1):
    t = open(f).read()
    assert a in t, (f, a[:60])
    open(f, 'w').write(t.replace(a, b, count))


# group C2
shutil.copy(W + 'ag-c2/parts/65-generic.pego', W + 'parts/65-generic.pego')
shutil.copy(W + 'ag-c2/ext/c2.py', W + 'ext/c2.py')
sub(W + 'parts/00-types.pego', 'type Stmt = DmlStmt | StmtA | StmtB | StmtC | StmtB2', 'type Stmt = DmlStmt | StmtC2 | StmtA | StmtB | StmtC | StmtB2')
sub(W + 'parts/90-main.pego', 'def stmt: Stmt = stmt_dml / stmt_a / stmt_b / stmt_c / stmt_b2',
    'def stmt: Stmt = stmt_dml / stmt_c2 / stmt_a / stmt_b / stmt_c / stmt_b2')

# B1 uses the DropStmt and the object types of C2
sub(W + 'parts/61-objects.pego', '''// DROP statements of this group (the other DROP statements are the DropStmt of group C2)
type DropObject = NodeList | ObjectWithArgs | TypeName
type DropStmt struct { Objects []DropObject, RemoveType string, Behavior *DropBehavior, MissingOk bool, Concurrent bool }
''', '')
sub(W + 'parts/61-objects.pego', '''// the object type is the keyword(s) that were written: the converter maps them to OBJECT_xxx
type ObjectTypeTok terminal
''', '')
for f in ['61-objects', '61-d-ext']:
    t = open(W + 'parts/%s.pego' % f).read()
    t = t.replace('ObjectTypeTok', 'ObjectType')
    open(W + 'parts/%s.pego' % f, 'w').write(t)

# stubs: only what C1 stubbed for group A remains
c1 = open(W + 'ag-c1/parts/99-stubs.pego').read()
open(W + 'parts/99-stubs.pego', 'w').write(c1)
