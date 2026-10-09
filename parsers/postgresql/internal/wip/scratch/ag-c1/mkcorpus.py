"""Writes ag-c1/c1corpus.jsonl.gz: the statements of the corpus that belong to group C1 (by their first words).
usage: python3 mkcorpus.py"""
import gzip, json, re

W = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/'
pat = re.compile(r'^\s*(?:(?:--[^\n]*\n|/\*.*?\*/)\s*)*(?i:'
                 r'set|reset|show|checkpoint|discard|begin|start\s+transaction|commit|rollback|end|abort|savepoint|release|'
                 r'prepare|copy|explain|vacuum|analyze|analyse|cluster|reindex|lock|execute|deallocate|declare|fetch|move|'
                 r'close|listen|unlisten|notify|load|do|call|truncate|alter\s+system|create\s+(?:\w+\s+)?table\s+\S+\s+as\s+execute)\b',
                 re.S)
n = 0
tot = 0
with gzip.open(W + 'ag-c1/c1corpus.jsonl.gz', 'wt') as out:
    for line in gzip.open(W + 'full.jsonl.gz', 'rt'):
        tot += 1
        r = json.loads(line)
        if pat.match(r['sql']):
            n += 1
            out.write(line)
print(n, 'of', tot)
