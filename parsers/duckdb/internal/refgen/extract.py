#!/usr/bin/env python3
"""Extracts the SQL blocks of DuckDB's sqllogictest files (test/sql/**/*.test*).

Usage: extract.py SUITE_ROOT OUT.jsonl

SUITE_ROOT is a checkout of the DuckDB sources that holds test/sql. Every `statement` and `query` block of every
file is read; `loop` and `foreach` blocks are expanded (the first values only, so the output stays small); a block
that still refers to a ${variable} is dropped. The unique SQL texts are written one JSON object per line:
{"sql": ..., "file": the first file the text occurs in}.

Whether DuckDB's parser accepts a text is not read from the test (statement error often fails when binding or
executing, not when parsing): refgen.py asks the parser.
"""
import json
import os
import re
import sys

MAX_VALUES = 4  # values of a loop or foreach that are expanded


def blocks(lines):
    """Yields ('sql', text) for each statement or query block and nested loops as ('loop', ...)."""
    out = []
    i = 0
    n = len(lines)
    stack = []
    cur = out
    while i < n:
        line = lines[i]
        s = line.strip()
        if s == '' or s.startswith('#'):
            i += 1
            continue
        w = s.split()
        cmd = w[0]
        if cmd in ('loop', 'concurrentloop'):
            node = ('loop', w[1], w[2:], [])
            cur.append(node)
            stack.append(cur)
            cur = node[3]
            i += 1
        elif cmd in ('foreach', 'concurrentforeach'):
            node = ('foreach', w[1], w[2:], [])
            cur.append(node)
            stack.append(cur)
            cur = node[3]
            i += 1
        elif cmd == 'endloop':
            if stack:
                cur = stack.pop()
            i += 1
        elif cmd in ('statement', 'query'):
            i += 1
            body = []
            while i < n:
                l = lines[i]
                if l.strip() == '':
                    break
                if cmd == 'query' and l.strip() == '----':
                    break
                if cmd == 'statement' and l.strip() == '----':
                    break
                body.append(l)
                i += 1
            # skip the expected results
            while i < n and lines[i].strip() != '':
                i += 1
            cur.append(('sql', '\n'.join(body)))
        else:
            # require, mode, skipif, onlyif, load, restart, reset, hash-threshold, ...
            i += 1
    return out


VAR = re.compile(r'\$\{(\w+)\}')


def subst(text, env):
    return VAR.sub(lambda m: env.get(m.group(1), m.group(0)), text)


def expand(items, env, emit):
    for it in items:
        if it[0] == 'sql':
            emit(subst(it[1], env))
        elif it[0] == 'loop':
            _, var, args, body = it
            try:
                lo, hi = int(subst(args[0], env)), int(subst(args[1], env))
            except (ValueError, IndexError):
                continue
            for v in range(lo, min(hi, lo + MAX_VALUES)):
                e = dict(env)
                e[var] = str(v)
                expand(body, e, emit)
        else:
            _, var, args, body = it
            vals = [subst(a, env) for a in args]
            vals = [v for v in vals if not v.startswith('<') and '${' not in v]
            for v in vals[:MAX_VALUES]:
                e = dict(env)
                e[var] = v
                expand(body, e, emit)


def main():
    root, outp = sys.argv[1], sys.argv[2]
    seen = {}
    files = []
    for d, _, fs in os.walk(os.path.join(root, 'test', 'sql')):
        for f in fs:
            if '.test' in f:
                files.append(os.path.join(d, f))
    files.sort()
    env0 = {'__TEST_DIR__': 'test_dir', '__WORKING_DIRECTORY__': 'work', 'TEST_DIR': 'test_dir',
            'DATA_DIR': 'data', 'TEMP_DIR': 'temp'}

    def mk(path):
        rel = os.path.relpath(path, root)

        def emit(sql):
            if '${' in sql or sql.strip() == '':
                return
            if sql not in seen:
                seen[sql] = rel
        return emit

    for path in files:
        with open(path, encoding='utf-8', errors='replace') as fh:
            lines = fh.read().split('\n')
        expand(blocks(lines), env0, mk(path))
    with open(outp, 'w', encoding='utf-8') as out:
        for sql, f in seen.items():
            out.write(json.dumps({'sql': sql, 'file': f}, ensure_ascii=False) + '\n')
    print(len(files), 'files,', len(seen), 'unique blocks', file=sys.stderr)


if __name__ == '__main__':
    main()
