"""Patches t/parts/62-utility.pego for the tests: the surrogates of t/stubs are statements of the group."""
p = 't/parts/62-utility.pego'
s = open(p).read()
s = s.replace('type StmtC = ', 'type StmtC = AlterDatabaseSetStmt | AlterFunctionStmt | ', 1)
s = s.replace('def stmt_c: StmtC =\n', 'def stmt_c: StmtC =\n    AlterDatabaseSetStmt / AlterFunctionStmt /\n', 1)
open(p, 'w').write(s)
