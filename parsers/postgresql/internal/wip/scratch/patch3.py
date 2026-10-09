P = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/parts/'
t = open(P + '51-shared.pego').read()
a = t.index('// A function argument: [mode] [name] type')
b = t.index('def func_args:')
t = t[:a] + '''// A function argument: [mode] [name] type, or name mode type
def func_arg: FunctionParameter = [m = "FUNC_PARAM_DEFAULT"]
    ( ( IN OUT [m = "FUNC_PARAM_INOUT"] / IN [m = "FUNC_PARAM_IN"] / OUT [m = "FUNC_PARAM_OUT"] / INOUT [m = "FUNC_PARAM_INOUT"] / VARIADIC [m = "FUNC_PARAM_VARIADIC"] )
      ( n:param_name t:func_type / t:func_type )
    / n:param_name ( IN OUT [m = "FUNC_PARAM_INOUT"] / IN [m = "FUNC_PARAM_IN"] / OUT [m = "FUNC_PARAM_OUT"] / INOUT [m = "FUNC_PARAM_INOUT"] / VARIADIC [m = "FUNC_PARAM_VARIADIC"] ) t:func_type
    / n:param_name t:func_type
    / t:func_type )
    -> new FunctionParameter{Name: $n, ArgType: $t, Mode: m}
''' + t[b:]
open(P + '51-shared.pego', 'w').write(t)
m = open(P + '90-main.pego').read()
m = m.replace('''def stmt: Stmt = SelectStmt
def PreparableStmt: Stmt = SelectStmt
''', '''def stmt: Stmt = stmt_dml / stmt_a / stmt_b / stmt_c
def stmt_dml: Stmt = SelectStmt / InsertStmt / UpdateStmt / DeleteStmt / MergeStmt
''')
open(P + '90-main.pego', 'w').write(m)
for n, num, name in (('a', '60', 'tables'), ('b', '61', 'objects'), ('c', '62', 'utility')):
    open(P + '%s-%s.pego' % (num, name), 'w').write('\n// stub: replaced by the statements of group %s\ndef stmt_%s: Stmt = _|_\n' % (n.upper(), n))
d = open(P + '50-dml.pego').read()
d = d.replace('def PreparableStmt: Stmt = SelectStmt / InsertStmt / UpdateStmt / DeleteStmt / MergeStmt', 'def PreparableStmt: Stmt = stmt_dml')
open(P + '50-dml.pego', 'w').write(d)
