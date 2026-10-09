# usage: mutate.py SEED N OUT.jsonl.gz
# Mutates the valid statements of cases.jsonl.gz (and of the C2 statements of the corpus): deletes, duplicates,
# swaps, replaces and inserts tokens; asks the reference about each mutant; writes a corpus for pgrun.
import gzip, json, random, re, sys
import pglast
from pglast import parser as pgparser

W = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work'
seed, n, out = int(sys.argv[1]), int(sys.argv[2]), sys.argv[3]
rnd = random.Random(seed)
TOK = re.compile(r"""\s+|--[^\n]*|/\*.*?\*/|'(?:[^']|'')*'|"(?:[^"]|"")*"|\$\$.*?\$\$|[A-Za-z_][A-Za-z_0-9$]*|\d+(?:\.\d+)?|[(),.;*+\-/<>=!@#%^&|`?~\[\]:]""", re.S)
MY = ('DropStmt', 'RenameStmt', 'AlterObjectSchemaStmt', 'AlterOwnerStmt', 'AlterObjectDependsStmt', 'CommentStmt',
      'SecLabelStmt', 'GrantStmt', 'GrantRoleStmt', 'AlterDefaultPrivilegesStmt')
seeds = []
for l in gzip.open(W + '/ag-c2/cases.jsonl.gz', 'rt'):
    r = json.loads(l)
    if r['ok'] and r['tree'] and list(r['tree'][0].keys())[0] in MY:
        seeds.append(r['sql'])
for l in gzip.open(W + '/full.jsonl.gz', 'rt'):
    r = json.loads(l)
    if r['ok'] and r['tree'] and list(r['tree'][0].keys())[0] in MY and rnd.random() < 0.08:
        seeds.append(r['sql'])
seeds = [s for s in seeds if ';' not in s and len(s) < 400]


def toks(s):
    return [t for t in TOK.findall(s) if not t.isspace() and not t.startswith('--') and not t.startswith('/*')]


vocab = set()
for s in seeds:
    vocab.update(toks(s))
vocab = sorted(t for t in vocab if len(t) < 20 and not t.startswith("'"))
KW = ['IF', 'EXISTS', 'ON', 'TO', 'FROM', 'AS', 'IS', 'NULL', 'TABLE', 'TYPE', 'DOMAIN', 'SCHEMA', 'FUNCTION', 'CASCADE', 'RESTRICT',
      'GRANT', 'OPTION', 'FOR', 'ALL', 'PUBLIC', 'GROUP', 'USER', 'ROLE', 'COLUMN', 'CONSTRAINT', 'NO', 'DEPENDS', 'EXTENSION',
      'OWNER', 'SET', 'RENAME', 'WITH', 'BY', 'USING', 'IN', 'SELECT', 'DEFAULT', 'CONCURRENTLY', 'ONLY', 'ORDER', 'VARIADIC',
      'OUT', 'INOUT', 'NONE', 'LANGUAGE', 'PROCEDURAL', 'LARGE', 'OBJECT', 'DATA', 'WRAPPER', 'SEARCH', 'TEXT', 'MATERIALIZED',
      'VIEW', 'FOREIGN', 'SERVER', 'TABLES', 'SEQUENCES', 'FUNCTIONS', 'ROUTINES', 'PRIVILEGES', 'GRANTED', 'ADMIN', 'PARAMETER',
      '(', ')', ',', '.', '*']
vocab = vocab + KW * 3


def mutate(ts):
    t = list(ts)
    op = rnd.randrange(7)
    i = rnd.randrange(len(t))
    if op == 0 and len(t) > 1:
        del t[i]
    elif op == 1:
        t.insert(i, t[i])
    elif op == 2 and i + 1 < len(t):
        t[i], t[i + 1] = t[i + 1], t[i]
    elif op == 3:
        t[i] = rnd.choice(vocab)
    elif op == 4:
        t.insert(i, rnd.choice(vocab))
    elif op == 5 and len(t) > 2:
        del t[i]
        j = rnd.randrange(len(t))
        del t[j]
    else:
        t[i] = rnd.choice(KW)
    return t


seen = set()
recs = []
tries = 0
while len(recs) < n and tries < n * 20:
    tries += 1
    ts = toks(rnd.choice(seeds))
    for _ in range(rnd.choice((1, 1, 1, 2))):
        ts = mutate(ts)
    sql = ' '.join(ts)
    if sql in seen or ';' in sql:
        continue
    seen.add(sql)
    try:
        j = json.loads(pgparser.parse_sql_json(sql))
        tree = [s['stmt'] for s in j['stmts']]
        recs.append({'f': 'mut%d.sql' % seed, 'l': len(recs) + 1, 'sql': sql, 'ok': True, 'tree': tree})
    except Exception as e:
        recs.append({'f': 'mut%d.sql' % seed, 'l': len(recs) + 1, 'sql': sql, 'ok': False, 'err': str(e)[:100], 'pos': 0})
with gzip.open(out, 'wt') as f:
    for r in recs:
        f.write(json.dumps(r) + '\n')
print(len(recs), 'mutants', sum(r['ok'] for r in recs), 'accepted by the reference')
