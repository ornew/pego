import shutil

W = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/'


def sub(f, a, b):
    t = open(f).read()
    assert a in t, (f, a[:60])
    open(f, 'w').write(t.replace(a, b, 1))


# group B1
for f in ['61-objects', '61-b-func', '61-c-define', '61-d-ext', '51-shared', '52-shared2']:
    shutil.copy(W + 'ag-b1/parts/%s.pego' % f, W + 'parts/%s.pego' % f)
shutil.copy(W + 'ag-b1/ext/b1-convert.py', W + 'ext/b1-convert.py')

# group C1
for f in ['62-utility', '62-utility-b']:
    shutil.copy(W + 'ag-c1/parts/%s.pego' % f, W + 'parts/%s.pego' % f)
shutil.copy(W + 'ag-c1/ext/c1.py', W + 'ext/c1.py')

sub(W + 'parts/00-types.pego', 'Xconst | Boolean\n', 'Xconst | Boolean | SignedNum\n') if False else None
t = open(W + 'parts/00-types.pego').read()
a = 'type ConstVal = String | Ident | Integer | Iconst | Fconst | Sconst | Bconst | Xconst | Boolean'
assert a in t
t = t.replace(a, a + ' | SignedNum', 1)
a = 'type IntoClause struct { Rel RangeVar, OnCommit string }'
assert a in t
t = t.replace(a, '''type IntoClause struct {
    Rel RangeVar, ColNames []Ident, AccessMethod *Ident, Options []DefElem, OnCommit string, TableSpaceName *Ident,
    SkipData bool
}''', 1)
open(W + 'parts/00-types.pego', 'w').write(t)

sub(W + 'parts/01-types2.pego', 'type DefArg = TypeName | Ident | NodeList | NumericOnly | Sconst | String | Boolean | Integer',
    'type DefArg = TypeName | Ident | NodeList | NumericOnly | Sconst | String | Boolean | Integer | A_Const | A_Star')
sub(W + 'parts/50-dml.pego', 'def where_or_current_clause: Expr = where_clause / where_current_of',
    'def where_or_current_clause: Expr = where_current_of / where_clause')

# stubs: what B1 stubbed for C2, and what C1 stubbed for A
b1 = open(W + 'ag-b1/parts/99-stubs.pego').read()
i = b1.index('// ---- Region C2')
c1 = open(W + 'ag-c1/parts/99-stubs.pego').read()
open(W + 'parts/99-stubs.pego', 'w').write(c1 + '\n' + b1[i:])
