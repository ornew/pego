#!/usr/bin/env python3
"""Turns the output of refgen.py into the reference data that testdata keeps, which has none of the text of DuckDB's tests.

Usage: pack.py tests|mutants IN.jsonl DUCKDB_ROOT OUT.jsonl.gz

IN is the output of refgen.py for the texts that extract.py (tests) or corpus.py mutants (mutants) made, DUCKDB_ROOT
the checkout of DuckDB they were read from. A line of OUT is the line of IN without "sql", and with

  h       the first 16 hexadecimal digits of the SHA-256 of the text, which the tests check against the text they make
  o, n    for a test, the offset and size in bytes of its lines in "file", a file of DUCKDB_ROOT (extract.py)
  p       where the text is not those lines as they are (a loop variable replaced), or for a mutant always: the pieces
          that make it, [offset, size] for a copy of a part of the lines (of the block, for a mutant) and a string for
          text of its own
  base    for a mutant, h of the test it is made from

A statement of "stmts" has "p" instead of "text": the pieces of the statement's text in the text of the line, since
DuckDB rewrites some statements. The tests read the files of the fetched corpus "duckdb" and make the texts again.
"""
import difflib
import gzip
import hashlib
import io
import json
import os
import sys


def pieces(base, target):
    """Returns the pieces ([offset, size] of a copy from base, or a string) that make target from base, both bytes."""
    a = base.decode('utf-8', 'surrogateescape')
    b = target.decode('utf-8', 'surrogateescape')
    offs = [0]
    for ch in a:
        offs.append(offs[-1] + (1 if '\udc80' <= ch <= '\udcff' else len(ch.encode('utf-8'))))
    out = []
    j = 0
    for i, k, size in difflib.SequenceMatcher(None, a, b, autojunk=False).get_matching_blocks():
        if k > j:
            out.append(b[j:k])
        if size:
            out.append([offs[i], offs[i + size] - offs[i]])
        j = k + size
    return out


def digest(text):
    return hashlib.sha256(text).hexdigest()[:16]


def main():
    kind, inp, root, outp = sys.argv[1:5]
    files = {}
    rows = []
    for line in open(inp, encoding='utf-8'):
        r = json.loads(line)
        sql = r.pop('sql').encode('utf-8')
        new = {}
        if kind == 'tests':
            f = r['file']
            if f not in files:
                files[f] = open(os.path.join(root, f), 'rb').read()
            base = files[f][r['o']:r['o'] + r['n']]
            new['file'], new['o'], new['n'] = r.pop('file'), r.pop('o'), r.pop('n')
            if base != sql:
                new['p'] = pieces(base, sql)
        else:
            new['file'] = r.pop('file')
            new['base'] = r.pop('base')
            new['p'] = r.pop('p')
        new['h'] = digest(sql)
        for k, v in r.items():
            if k == 'stmts' and v:
                v = [{'type': s['type'], 'p': pieces(sql, s['text'].encode('utf-8'))} if 'text' in s else s for s in v]
            new[k] = v
        rows.append(new)
    raw = gzip.GzipFile(outp, 'wb', mtime=0)
    raw.name = ''
    with io.TextIOWrapper(raw, encoding='utf-8') as out:
        for r in rows:
            out.write(json.dumps(r, ensure_ascii=False, separators=(',', ':')) + '\n')
    print(len(rows), 'texts', file=sys.stderr)


if __name__ == '__main__':
    main()
