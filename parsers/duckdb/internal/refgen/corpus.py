#!/usr/bin/env python3
"""Generates the texts that are not in DuckDB's tests, to be run through refgen.py:

  mutants   statements of the tests with a token deleted, duplicated, swapped, replaced or inserted, or cut short
  keywords  every keyword where a name may stand (column, table, function, type, alias ...), in three spellings
  exprs     expressions with operators in every pair, so that precedence and associativity show
  lexical   literals, operators, comments and whitespace of the scanner
  found     the texts of found.txt: those on which an earlier version of the parser and DuckDB's disagreed

Usage: corpus.py KIND BLOCKS.jsonl OUT.jsonl [COUNT [SEED]]

COUNT is the number of mutants (10000), or of random expressions (3000), and SEED the seed of the generator.

BLOCKS.jsonl is extract.py's output (for mutants). Everything is deterministic: the random generator is seeded.
"""
import json
import os
import random
import re
import sys

HERE = os.path.dirname(os.path.realpath(__file__))

TOKEN = re.compile(r"""
    \s+ | --[^\n]*\n? | /\*.*?\*/ |
    '(?:[^']|'')*' | "(?:[^"]|"")*" | \$\$.*?\$\$ |
    [A-Za-z_\u0080-\U0010ffff][A-Za-z0-9_$\u0080-\U0010ffff]* |
    [0-9]+(?:\.[0-9]*)?(?:[eE][-+]?[0-9]+)? | \.[0-9]+ |
    ::|:=|=>|<=|>=|<>|!=|->>|->|\|\||\*\*|// |
    [-+*/%^<>=~!@&|`]+ | .""", re.X | re.S)


def tokens(sql):
    return [m.group(0) for m in TOKEN.finditer(sql)]


def load_keywords():
    kws = set()
    for f in os.listdir(os.path.join(HERE, 'keywords')):
        for l in open(os.path.join(HERE, 'keywords', f)):
            l = l.strip().lower()
            if l:
                kws.add(l[:-2] if l.endswith('_p') else l)
    return sorted(kws)


def mutants(blocks, out, count, seed=20260101):
    rnd = random.Random(seed)
    rows = [json.loads(l) for l in open(blocks, encoding='utf-8')]
    rows = [r for r in rows if len(r['sql']) < 600 and '${' not in r['sql']]
    kws = load_keywords()
    puncts = [',', '(', ')', ';', '.', '*', '=', '+', '-', '[', ']', '{', '}', ':', '::', '|', 'NOT', 'AND', 'AS', 'ON', 'IN']
    seen = set()
    n = 0
    while n < count:
        r = rnd.choice(rows)
        toks = tokens(r['sql'])
        idx = [i for i, t in enumerate(toks) if not t.isspace() and not t.startswith(('--', '/*'))]
        if len(idx) < 3:
            continue
        op = rnd.choice(['cut', 'delete', 'dup', 'swap', 'replace', 'insert', 'kw', 'delete2'])
        t = list(toks)
        i = rnd.choice(idx)
        if op == 'cut':
            t = t[:i]
        elif op == 'delete':
            del t[i]
        elif op == 'delete2':
            j = rnd.choice(idx)
            for k in sorted({i, j}, reverse=True):
                del t[k]
        elif op == 'dup':
            t.insert(i, t[i] + ' ' if t[i].isalnum() else t[i])
        elif op == 'swap':
            j = rnd.choice(idx)
            t[i], t[j] = t[j], t[i]
        elif op == 'replace':
            t[i] = rnd.choice(puncts)
        elif op == 'insert':
            t.insert(i, rnd.choice(puncts) + ' ')
        elif op == 'kw':
            t[i] = rnd.choice(kws) + ' '
        sql = ''.join(t)
        if sql in seen or not sql.strip():
            continue
        seen.add(sql)
        out.append({'sql': sql, 'file': 'mutant of ' + r['file'], 'gen': op})
        n += 1


def keywords(out):
    kws = load_keywords()
    contexts = [
        'SELECT {k}', 'SELECT 1 AS {k}', 'SELECT 1 {k}', 'SELECT {k}(1)', 'SELECT 1::{k}', 'SELECT CAST(1 AS {k})',
        'CREATE TABLE {k} (a INT)', 'CREATE TABLE t ({k} INT)', 'SELECT * FROM {k}', 'SELECT x.{k} FROM t',
        'SELECT * FROM t {k}', 'SELECT * FROM t AS {k}', 'SELECT {k}.x FROM t', 'SELECT * FROM {k}(1)',
        'SELECT a FROM t WHERE {k}', 'INSERT INTO t ({k}) VALUES (1)', 'SELECT 1 FROM t GROUP BY {k}',
        'SELECT {k} FROM t', 'SELECT {k}, {k}', 'UPDATE t SET {k} = 1', 'CREATE INDEX {k} ON t (a)',
        'DROP TABLE {k}', 'SET {k} = 1', 'PRAGMA {k}', 'CALL {k}()', 'SELECT a.{k}.b', 'SELECT {k} \'x\'',
        'CREATE SCHEMA {k}', 'CREATE VIEW {k} AS SELECT 1', 'SELECT * FROM t JOIN u ON t.a = u.a AND {k}',
        'SELECT x::INT AS {k}', 'ALTER TABLE t ADD COLUMN {k} INT', 'ALTER TABLE t RENAME TO {k}',
        'COMMENT ON TABLE {k} IS \'x\'', 'SELECT f(x := {k})', 'SELECT {k}::INT', 'SELECT [{k}]', 'SELECT {{\'a\': {k}}}',
        'EXPLAIN {k}', 'SELECT * FROM t, {k}', 'WITH {k} AS (SELECT 1) SELECT * FROM {k}', 'SELECT 1 FROM t ORDER BY {k}',
        'SELECT * FROM (SELECT 1) {k}', 'SELECT * FROM t PIVOT (sum(a) FOR {k} IN (1))', 'COPY t TO \'f\' ({k})',
        'CREATE TYPE {k} AS ENUM (\'a\')', 'CREATE SEQUENCE {k}', 'CREATE MACRO {k}(a) AS a',
        'SELECT 1 FROM t WINDOW {k} AS ()', 'SELECT sum(a) OVER {k} FROM t', 'DESCRIBE {k}', 'USE {k}',
        'ATTACH \'x\' AS {k}', 'DETACH {k}', 'PREPARE {k} AS SELECT 1', 'EXECUTE {k}', 'DEALLOCATE {k}',
    ]
    for k in kws:
        for form in dict.fromkeys([k, k.upper(), k.capitalize()]):
            for c in contexts:
                out.append({'sql': c.replace('{k}', form).replace('{{', '{').replace('}}', '}'), 'file': 'keyword ' + k, 'gen': 'keyword'})


UNARY = ['-', '+', 'NOT', '~', '@', '!!']
BINARY = ['+', '-', '*', '/', '//', '%', '^', '**', '<', '>', '=', '<=', '>=', '<>', '!=', '||', '->', '->>', '~~', '!~~', '@>', '<@',
          '&&', '&', '|', '~', 'AND', 'OR', 'IS DISTINCT FROM', 'IS NOT DISTINCT FROM', 'LIKE', 'NOT LIKE', 'ILIKE', 'GLOB',
          'SIMILAR TO', 'IN', 'NOT IN', 'COLLATE', 'AT TIME ZONE', '::', 'OVERLAPS']
POSTFIX = ['IS NULL', 'IS NOT NULL', 'ISNULL', 'NOTNULL', 'NOT NULL', 'IS TRUE', 'IS NOT FALSE', 'IS UNKNOWN', '!']


def exprs(out, count=3000, seed=7):
    rnd = random.Random(seed)
    atoms = ['a', 'b', 'c', '1', "'x'", 'f(x)', '(a)', 'NULL', 'x.y', '[1]']

    def atom(op):
        a = rnd.choice(atoms)
        if op == '::':
            return rnd.choice(['int', 'varchar', 'int[]', 'decimal(5,2)'])
        if op in ('IN', 'NOT IN'):
            return rnd.choice(['(1, 2)', '(SELECT 1)', 'x', '[1]'])
        if op == 'COLLATE':
            return 'nocase'
        if op == 'AT TIME ZONE':
            return "'UTC'"
        return a
    seen = set()
    # every pair of binary operators, both nestings come from precedence
    for o1 in BINARY:
        for o2 in BINARY:
            for form in (0, 1):
                a, b, c = rnd.choice(atoms), rnd.choice(atoms), rnd.choice(atoms)
                b1, c2 = atom(o1), atom(o2)
                if form == 0:
                    e = '%s %s %s %s %s' % (a, o1, b1, o2, c2)
                else:
                    e = '%s %s %s %s %s' % (a, o1, b if o1 not in ('::', 'COLLATE', 'AT TIME ZONE', 'IN', 'NOT IN') else b1, o2, c2)
                if e not in seen:
                    seen.add(e)
                    out.append({'sql': 'SELECT ' + e, 'file': 'exprs', 'gen': 'pair'})
    for u in UNARY:
        for o in BINARY:
            e = '%s a %s %s' % (u, o, atom(o))
            out.append({'sql': 'SELECT ' + e, 'file': 'exprs', 'gen': 'unary'})
            e = 'a %s %s %s b' % (o, atom(o), u)
            out.append({'sql': 'SELECT ' + e, 'file': 'exprs', 'gen': 'unary'})
    for p in POSTFIX:
        for o in BINARY:
            e = 'a %s %s' % (p, o) + ' ' + atom(o)
            out.append({'sql': 'SELECT ' + e, 'file': 'exprs', 'gen': 'postfix'})
            e = 'a %s %s %s' % (o, atom(o), p)
            out.append({'sql': 'SELECT ' + e, 'file': 'exprs', 'gen': 'postfix'})
    # BETWEEN, CASE, subscripts, nested
    for o in BINARY:
        out.append({'sql': 'SELECT a BETWEEN b %s c AND d' % o if o not in ('::', 'IN', 'NOT IN', 'COLLATE', 'AT TIME ZONE') else 'SELECT a BETWEEN b AND d', 'file': 'exprs', 'gen': 'between'})
        out.append({'sql': 'SELECT a BETWEEN b AND c %s %s' % (o, atom(o)), 'file': 'exprs', 'gen': 'between'})
        out.append({'sql': 'SELECT NOT a BETWEEN b AND c %s %s' % (o, atom(o)), 'file': 'exprs', 'gen': 'between'})
    # random trees
    ops = BINARY + POSTFIX + UNARY
    for n in range(count):
        parts = []
        k = rnd.randint(2, 5)
        for i in range(k):
            parts.append(rnd.choice(atoms))
            if i < k - 1:
                op = rnd.choice(BINARY)
                parts.append(op)
                if op in ('::', 'COLLATE', 'AT TIME ZONE', 'IN', 'NOT IN'):
                    parts[-1] = op + ' ' + atom(op)
                    parts.pop(-2) if False else None
        out.append({'sql': 'SELECT ' + ' '.join(parts), 'file': 'exprs', 'gen': 'random'})


def lexical(out):
    nums = ['1', '1.', '.5', '1.5', '1e5', '1E+5', '1e', '1e+', '1_000', '1__0', '1_', '0x1F', '0b101', '1..2', '1.e3', '1.5e-3',
            '2147483647', '2147483648', '9223372036854775808', '99999999999999999999999999999999999999999', '-1', '- 1']
    for n in nums:
        out.append({'sql': 'SELECT ' + n, 'file': 'lexical', 'gen': 'number'})
        out.append({'sql': 'SELECT ' + n + ' AS x', 'file': 'lexical', 'gen': 'number'})
        out.append({'sql': 'SELECT 1 + ' + n, 'file': 'lexical', 'gen': 'number'})
        out.append({'sql': 'SELECT x[' + n + ']', 'file': 'lexical', 'gen': 'number'})
        out.append({'sql': 'SELECT 1::int[' + n + ']', 'file': 'lexical', 'gen': 'number'})
        out.append({'sql': 'SELECT * FROM t LIMIT ' + n, 'file': 'lexical', 'gen': 'number'})
        out.append({'sql': 'SELECT * FROM t LIMIT ' + n + ' %', 'file': 'lexical', 'gen': 'number'})
        out.append({'sql': 'SELECT * FROM t LIMIT ' + n + ' PERCENT', 'file': 'lexical', 'gen': 'number'})
        out.append({'sql': 'SELECT #' + n, 'file': 'lexical', 'gen': 'number'})
    strs = ["'a'", "''", "''''", "'a''b'", "'a\\nb'", "E'a\\nb'", "E'\\x41'", "E'\\101'", "E'\\u0041'", "E'\\u004'", "E'\\U00000041'",
            "E'\\''", "E'\\\\'", "e'a'", "'a'\n'b'", "'a' 'b'", "'a'\n\n'b'", "'a' -- c\n'b'", "'a'\n-- c\n'b'", "E'a'\n'b'", "$$a$$", "$$$$",
            "$a$b$a$", "$a$b$c$a$", "$a$ b $", "$$ $a$ $$", "$1$ x $1$", "$a", "$ a", "$_x$y$_x$", "B'101'", "b'1'", "X'FF'", "x'ff'", "N'x'",
            "U&'a'", "u&'\\0061'", "U&'a' UESCAPE '!'", "'a", "'", "E'", "$$", "$$ a", "'a' 'b' 'c'", "''''''", "'\\'", "E'\\'"]
    for s in strs:
        out.append({'sql': 'SELECT ' + s, 'file': 'lexical', 'gen': 'string'})
        out.append({'sql': 'SELECT ' + s + ' AS x', 'file': 'lexical', 'gen': 'string'})
        out.append({'sql': 'SELECT date ' + s, 'file': 'lexical', 'gen': 'string'})
        out.append({'sql': 'SELECT ' + s + '::int', 'file': 'lexical', 'gen': 'string'})
    idents = ['a', 'A', '_a', 'a$', 'a$b', '$a', '"a"', '"A b"', '""', '"a""b"', '"', 'U&"a"', 'u&"\\0061"', 'ä', 'a b', 'a​b', '日本', 'x1', '1x', 'a.b', '"a".b', 'a."b"', 'a . b', 'a.', '.a']
    for i in idents:
        out.append({'sql': 'SELECT ' + i, 'file': 'lexical', 'gen': 'ident'})
        out.append({'sql': 'SELECT 1 AS ' + i, 'file': 'lexical', 'gen': 'ident'})
        out.append({'sql': 'SELECT * FROM ' + i, 'file': 'lexical', 'gen': 'ident'})
        out.append({'sql': 'CREATE TABLE ' + i + ' (' + i + ' INT)', 'file': 'lexical', 'gen': 'ident'})
    ops = ['+', '-', '*', '/', '%', '^', '<', '>', '=', '<=', '>=', '<>', '!=', '=>', '->', '->>', '**', '//', '||', '~', '~~', '!~~', '@', '!', '&', '|', '`', '#', '?', '??', ':', '::', ':=',
           '+-', '-+', '*-', '*+', '=-', '<-', '>-', '<=-', '>=-', '=>-', '<>-', '!=-', '**-', '//-', '->-', '->>-', '^-', '%-', '@-', '~-', '!-', '+/*c*/', '-- c', '/**/', '/* a /* b */ c */', '/*', '*/', '+*', '**+', '*/*c*/', '||-', '&&', '<@', '@>', '?|']
    for o in ops:
        for form in ['SELECT 1 {o} 2', 'SELECT {o} 1', 'SELECT 1 {o}', 'SELECT a {o}b', 'SELECT a{o} b', 'SELECT a{o}b', 'SELECT 1 {o}-1', 'SELECT 1 {o} - 1', 'SELECT 1 {o}+1']:
            out.append({'sql': form.replace('{o}', o), 'file': 'lexical', 'gen': 'operator'})
    ws = [' ', '\t', '\n', '\r', '\f', '\v', ' ', ' ', '​', '﻿', '\u0085', '　', '/**/', '/* x */', '-- c\n', '-- c', '/*', '/* /* */ */', '/* */ */']
    for w in ws:
        out.append({'sql': 'SELECT' + w + '1', 'file': 'lexical', 'gen': 'space'})
        out.append({'sql': 'SELECT 1' + w, 'file': 'lexical', 'gen': 'space'})
        out.append({'sql': w + 'SELECT 1', 'file': 'lexical', 'gen': 'space'})
        out.append({'sql': 'SELECT 1' + w + ';' + w + 'SELECT 2', 'file': 'lexical', 'gen': 'space'})
        out.append({'sql': 'SELECT 1' + w + 'AS' + w + 'x', 'file': 'lexical', 'gen': 'space'})
    for s in [';', ';;', '; ;', 'SELECT 1;', 'SELECT 1;;', ';SELECT 1', ' ; SELECT 1 ; ; SELECT 2 ; ', 'SELECT 1 SELECT 2', 'SELECT 1;SELECT', '', ' ', '-- only', '/* only */', ';-- c\n;']:
        out.append({'sql': s, 'file': 'lexical', 'gen': 'script'})
    for kw in ['select', 'SELECT', 'Select', 'sElEcT', 'selecT', 'SeLeCt']:
        out.append({'sql': kw + ' 1', 'file': 'lexical', 'gen': 'case'})
        out.append({'sql': kw + ' 1 as ' + kw, 'file': 'lexical', 'gen': 'case'})
        out.append({'sql': 'SELECT 1 ' + kw, 'file': 'lexical', 'gen': 'case'})


def found(out):
    for l in open(os.path.join(HERE, 'found.txt'), encoding='utf-8'):
        l = l.rstrip('\n')
        if l and not l.startswith('#'):
            out.append({'sql': l, 'file': 'found', 'gen': 'found'})


def main():
    kind, blocks, outp = sys.argv[1], sys.argv[2], sys.argv[3]
    count = int(sys.argv[4]) if len(sys.argv) > 4 else None
    seed = int(sys.argv[5]) if len(sys.argv) > 5 else None
    out = []
    if kind == 'mutants':
        mutants(blocks, out, count or 10000, seed or 20260101)
    elif kind == 'keywords':
        keywords(out)
    elif kind == 'exprs':
        exprs(out, count or 3000, seed or 7)
    elif kind == 'lexical':
        lexical(out)
    elif kind == 'found':
        found(out)
    else:
        sys.exit('unknown kind')
    seen = set()
    with open(outp, 'w', encoding='utf-8') as f:
        for o in out:
            if o['sql'] in seen:
                continue
            seen.add(o['sql'])
            f.write(json.dumps(o, ensure_ascii=False) + '\n')
    print(kind, len(seen), file=sys.stderr)


if __name__ == '__main__':
    main()
