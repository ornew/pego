"""Writes mutated statements of group C1 to fz/cases/fz.sql for a differential test against the reference.
usage: python3 fuzz.py SEED COUNT
The statements are those of cases/*.sql and of the C1 part of the corpus; each is mutated by dropping, duplicating,
swapping or replacing one or two tokens."""
import gzip, json, random, re, sys, os

W = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/ag-c1/'
seed = int(sys.argv[1])
count = int(sys.argv[2])
random.seed(seed)

sys.path.insert(0, '/Users/s27814/src/github.com/ornew/pego/.claude/worktrees/agent-a84132ee088937fd9/parsers/postgresql/internal/refgen')
import gen

pool = []
for f in sorted(os.listdir(W + 'cases')) + (['../t/cases/' + x for x in sorted(os.listdir(W + 't/cases'))] if os.environ.get('FZT') else []):
    text = open(W + 'cases/' + f).read()
    for line, sql in gen.split_script(text):
        pool.append(sql)
for line in gzip.open(W + 'c1corpus.jsonl.gz', 'rt'):
    r = json.loads(line)
    if len(r['sql']) < 400 and r['ok']:
        pool.append(r['sql'])
pool = list(dict.fromkeys(pool))
random.shuffle(pool)
corp = pool[:]

tok = re.compile(r"""'(?:[^']|'')*'|"(?:[^"]|"")*"|\$\$.*?\$\$|[A-Za-z_][A-Za-z_0-9$]*|\d+(?:\.\d+)?|::|<>|<=|>=|=>|[^\sA-Za-z_0-9]""", re.S)
words = """SET RESET SHOW LOCAL SESSION TO FROM CURRENT TIME ZONE DEFAULT NAMES ROLE TRANSACTION ISOLATION LEVEL READ ONLY
WRITE DEFERRABLE NOT WORK AND CHAIN NO SAVEPOINT RELEASE PREPARED PREPARE COPY STDIN STDOUT PROGRAM BINARY CSV HEADER
FREEZE FORCE QUOTE NULL ENCODING DELIMITER DELIMITERS USING WITH WHERE EXPLAIN ANALYZE ANALYSE VERBOSE FULL VACUUM CLUSTER
REINDEX INDEX TABLE SCHEMA DATABASE SYSTEM CONCURRENTLY LOCK IN SHARE ROW EXCLUSIVE ACCESS MODE NOWAIT TRUNCATE RESTART
CONTINUE IDENTITY CASCADE RESTRICT EXECUTE DEALLOCATE ALL DECLARE CURSOR SCROLL INSENSITIVE ASENSITIVE HOLD WITHOUT FOR FETCH
MOVE NEXT PRIOR FIRST LAST ABSOLUTE RELATIVE FORWARD BACKWARD CLOSE LISTEN UNLISTEN NOTIFY LOAD DO LANGUAGE CALL DISCARD
TEMP PLANS SEQUENCES CHECKPOINT ALTER AUTHORIZATION CATALOG SCHEMA XML OPTION DOCUMENT CONTENT SNAPSHOT CONSTRAINTS DEFERRED
IMMEDIATE ON OFF TRUE FALSE INTERVAL HOUR MINUTE SECOND TIMESTAMP SELECT AS BEGIN END START COMMIT ROLLBACK ABORT FORMAT JSON
x y 1 -1 2 'a' "B" ( ) , * . ; DATA OF CURRENT_USER USER VALUES SKIP LOCKED""".split()
words = [w for w in words if w != ';']


def balanced(s):
    d = 0
    for t in tok.findall(s):
        if t == '(':
            d += 1
        elif t == ')':
            d -= 1
            if d < 0:
                return False
    return d == 0


out = []
n = 0
tries = 0
while n < count and tries < count * 20:
    tries += 1
    s = random.choice(corp)
    ts = tok.findall(s)
    if not ts or len(ts) > 60:
        continue
    for _ in range(random.choice([1, 1, 1, 2])):
        k = random.randrange(5)
        i = random.randrange(len(ts))
        if k == 0 and len(ts) > 1:
            del ts[i]
        elif k == 1:
            ts.insert(i, ts[i])
        elif k == 2 and i + 1 < len(ts):
            ts[i], ts[i + 1] = ts[i + 1], ts[i]
        elif k == 3:
            ts[i] = random.choice(words)
        else:
            ts.insert(i, random.choice(words))
    m = ' '.join(ts)
    if not balanced(m) or m.count("'") % 2 or m.count('"') % 2 or '$' in m.replace('$$', '') and m.count('$$') % 2:
        continue
    if '\\' in m:
        continue
    if re.search(r'(?i)\bbegin\b.*\batomic\b', m):
        continue
    out.append(m + ';')
    if re.match(r'(?i)\s*copy\b', m) and re.search(r'(?i)\bstdin\b', m):
        out.append('\\.')
    n += 1
os.makedirs(W + 'fz/cases', exist_ok=True)
open(W + 'fz/cases/fz.sql', 'w').write('\n'.join(out) + '\n')
print(n, 'statements')
