W = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/'
t = open(W + 'AGENTS.md').read()
a = t.index('    sh $W/t.sh RULE')
b = t.index('`$W/pego parse -g D/postgresql.pego -s RULE')
t = t[:a] + t[b:]
open(W + 'AGENTS.md', 'w').write(t)
p = open(W + 'parts/52-shared2.pego').read()
p += '''
// NonReservedWord_or_Sconst: a name or a string constant
type NameOrString = Ident | Sconst
def NonReservedWord_or_Sconst: NameOrString = NonReservedWord / Sconst
'''
open(W + 'parts/52-shared2.pego', 'w').write(p)
