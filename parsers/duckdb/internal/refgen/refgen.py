#!/usr/bin/env python3
"""Asks DuckDB's parser about SQL texts and writes the reference data the tests of parsers/duckdb compare with.

Usage: refgen.py IN.jsonl OUT.jsonl[.gz]

IN holds one JSON object per line with a "sql" text (extract.py writes it, mutate.py and exprs.py too). For each
text the output line has, besides the input fields:

  ok      whether DuckDB's parser accepts the text (no ParserException; exceptions that DuckDB raises after parsing,
          while handling PRAGMA statements for example, count as accepted)
  kind    for a rejected text, "syntax" if the grammar or the scanner rejected it, "semantic" if the transformer did
  err     the first line of the error message
  stmts   for an accepted text, the statements as DuckDB splits them: {"type": StatementType name, "text": the
          text of the statement}; DuckDB rewrites some statements (PRAGMA is rewritten to a SELECT) so its text can
          differ from the input
  shapes  for each statement of type SELECT whose text is a SELECT DuckDB can serialize, the AST that
          json_serialize_sql returns, reduced to a canonical form (see reduce)

The Python module duckdb must be the version the parser is measured against (1.5.6).
"""
import gzip
import io
import json
import multiprocessing
import re
import sys

import duckdb

SYNTAX_PREFIXES = ('Parser Error: syntax error at', 'Parser Error: zero-length delimited identifier',
                   'Parser Error: unterminated', 'Parser Error: invalid Unicode', 'Parser Error: operator too long',
                   'Parser Error: nonstandard use', 'Parser Error: Unicode escape')

PIVOT_ENUM = re.compile(r'__pivot_enum_[0-9a-f-]+')  # a UUID, perhaps cut short

# keys that only record where something was or that DuckDB fills in by itself
DROP_KEYS = {'query_location'}


def reduce(v):
    """Reduces the JSON of json_serialize_sql to a canonical form: no locations, and every key whose value is
    null, "", false, 0-length list or empty object is left out. The Go side builds the same form from its AST."""
    if isinstance(v, dict):
        out = {}
        for k, x in v.items():
            if k in DROP_KEYS:
                continue
            x = reduce(x)
            if x is None or x == '' or x is False or x == [] or x == {}:
                continue
            out[k] = x
        return out
    if isinstance(v, list):
        return [reduce(x) for x in v]
    if isinstance(v, float) and (v != v or v in (float('inf'), float('-inf'))):
        return str(v)  # NaN and the infinities are not JSON
    return v


def classify(e):
    msg = str(e).split('\n')[0]
    name = type(e).__name__
    if name == 'ParserException':
        if msg.startswith(SYNTAX_PREFIXES):
            return 'syntax', msg
        return 'semantic', msg
    return 'accepted', msg


def process(sql, con, shapes=True):
    r = {}
    try:
        stmts = con.extract_statements(sql)
        r['ok'] = True
        r['stmts'] = [{'type': str(s.type).split('.')[1]} if len(stmts) == 1 and s.query == sql
                      else {'type': str(s.type).split('.')[1], 'text': s.query} for s in stmts]
    except Exception as e:  # noqa
        kind, msg = classify(e)
        if kind == 'accepted':
            # raised after parsing: DuckDB's parser accepted the text but we do not know the statements
            r['ok'] = True
            r['err'] = msg
            r['stmts'] = None
        else:
            r['ok'] = False
            r['kind'] = kind
            r['err'] = msg
        return r
    if shapes:
        sh = []
        for s in r['stmts']:
            if s['type'] != 'SELECT':
                sh.append(None)
                continue
            try:
                j = json.loads(con.sql('select json_serialize_sql(?)', params=[s.get('text', sql)]).fetchone()[0])
            except Exception:  # noqa
                sh.append(None)
                continue
            if j.get('error') or len(j.get('statements', [])) != 1:
                sh.append(None)
                continue
            sh.append(reduce(j['statements'][0]['node']))
        if any(x is not None for x in sh):
            r['shapes'] = sh
    return r


def work(args):
    chunk, shapes = args
    con = duckdb.connect()
    out = []
    for d in chunk:
        r = process(d['sql'], con, shapes)
        d = dict(d)
        d.update(r)
        out.append(d)
    return out


def main():
    inp, outp = sys.argv[1], sys.argv[2]
    shapes = '--no-shapes' not in sys.argv
    rows = [json.loads(l) for l in open(inp, encoding='utf-8')]
    n = 500
    chunks = [(rows[i:i + n], shapes) for i in range(0, len(rows), n)]
    with multiprocessing.Pool() as pool:
        results = pool.map(work, chunks)
    if outp.endswith('.gz'):
        # no time in the header: the same data gives the same file
        raw = gzip.GzipFile(outp, 'wb', mtime=0)
        raw.name = ''
        out_file = io.TextIOWrapper(raw, encoding='utf-8')
    else:
        out_file = open(outp, 'w', encoding='utf-8')
    with out_file as out:
        for chunk in results:
            for d in chunk:
                line = json.dumps(d, ensure_ascii=False, separators=(',', ':'))
                # PIVOT makes a type with a name made of a random UUID: the data does not change from run to run
                line = PIVOT_ENUM.sub('__pivot_enum_UUID', line)
                out.write(line + '\n')
    print(len(rows), 'texts', file=sys.stderr)


if __name__ == '__main__':
    main()
