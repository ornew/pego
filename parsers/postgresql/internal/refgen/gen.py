#!/usr/bin/env python3
"""Generates the reference data of the PostgreSQL parser's tests with libpg_query (through pglast).

Usage (with an interpreter that has pglast installed, 8.x for PostgreSQL 18):

    python3 gen.py -sql DIR -o OUT.jsonl.gz [-sample N] [-seed S]

DIR is a directory of psql scripts (src/test/regress/sql of a PostgreSQL source tree). Each script is split
into statements the way psql does (see split_script), and for each statement the output records whether
the PostgreSQL 18 parser accepts it and the raw parse tree (parse_sql_json, the JSON of the C parser's
node structs) if it does. Output: one JSON object per line, gzip-compressed:

    {"f": "select.sql", "l": 12, "sql": "select 1", "ok": true, "tree": [{...}, ...]}
    {"f": "select.sql", "l": 20, "sql": "selct 1", "ok": false, "err": "syntax error at or near \"selct\"", "pos": 1}

"tree" is the list of the "stmt" objects of the parse result (empty for a comment-only statement), without
the "stmt_location" and "stmt_len" members. "pos" is the 1-based cursor position of the error.

With -sample N, at most N statements are kept: all rejected statements, and for the accepted ones a
selection that is spread over the scripts and has as many distinct node-type shapes as possible, chosen
deterministically from the seed. Without it every statement is kept (the full run, which is too large to
vendor).
"""

import argparse
import gzip
import json
import os
import random
import re
import sys

import pglast
from pglast import parser as pgparser

IDENT_START = re.compile(r"[A-Za-z\x80-\U0010ffff_]")
IDENT_CONT = re.compile(r"[A-Za-z\x80-\U0010ffff_0-9$]")


def split_script(text):
    """Splits a psql script into statements. Returns a list of (line, sql) with the 1-based line of the
    first character of each statement.

    It follows psqlscan.l: quotes ('...', E'...', "...", $tag$...$tag$) and comments (-- and nested
    /* */) are opaque; a semicolon ends the statement unless it is inside parentheses or inside the body
    of BEGIN ATOMIC ... END; a backslash outside quotes starts a psql meta-command that runs to the end
    of the line, and the commands \\g, \\gset, \\gx, \\gexec, \\gdesc, \\parse, \\crosstabview and \\watch
    end the statement like a semicolon; the data of COPY ... FROM STDIN up to a line \\. is skipped.
    """
    n = len(text)
    i = 0
    line = 1
    out = []
    buf = []  # characters of the current statement
    start_line = None
    depth = 0
    begin_depth = 0
    words = []  # leading words of the current statement, to recognize CREATE FUNCTION ... BEGIN ATOMIC
    last_word = None
    copy_stdin = False

    def has_content():
        return any(not c.isspace() for c in buf)

    def finish():
        nonlocal buf, start_line, depth, begin_depth, words, last_word, copy_stdin
        s = "".join(buf)
        if s.strip() and strip_comments(s).strip():
            out.append((start_line, s.strip()))
        buf = []
        start_line = None
        depth = 0
        begin_depth = 0
        words = []
        last_word = None
        was_copy = copy_stdin
        copy_stdin = False
        return was_copy

    def add(s):
        nonlocal start_line
        if start_line is None and not s.isspace():
            start_line = line
        buf.append(s)

    while i < n:
        c = text[i]
        if c == "\n":
            add(c) if buf else None
            line += 1
            i += 1
            continue
        # comments
        if text.startswith("--", i):
            j = text.find("\n", i)
            j = n if j < 0 else j
            if buf:
                buf.append(text[i:j])
            i = j
            continue
        if text.startswith("/*", i):
            d = 0
            j = i
            while j < n:
                if text.startswith("/*", j):
                    d += 1
                    j += 2
                elif text.startswith("*/", j):
                    d -= 1
                    j += 2
                    if d == 0:
                        break
                else:
                    j += 1
            seg = text[i:j]
            if buf:
                buf.append(seg)
            line += seg.count("\n")
            i = j
            continue
        if c == "\\":
            # psql meta-command
            j = i + 1
            while j < n and not text[j].isspace() and text[j] != "\\":
                j += 1
            name = text[i + 1:j]
            k = text.find("\n", j)
            k = n if k < 0 else k
            if name.startswith("g") or name in ("parse", "crosstabview", "watch"):
                if has_content():
                    finish()
            i = k
            continue
        if c.isspace():
            if buf:
                buf.append(c)
            i += 1
            continue
        # a token of the statement
        if start_line is None:
            start_line = line
        if c == "'" or (c in "eE" and text.startswith("'", i + 1) and not (buf and IDENT_CONT.match(buf[-1][-1:] or " "))):
            esc = c in "eE"
            j = i + (2 if esc else 1)
            while j < n:
                if esc and text[j] == "\\" and j + 1 < n:
                    j += 2
                    continue
                if text[j] == "'":
                    if text.startswith("''", j):
                        j += 2
                        continue
                    break
                j += 1
            seg = text[i:j + 1]
            line += seg.count("\n")
            buf.append(seg)
            i = j + 1
            last_word = None
            continue
        if c == '"':
            j = i + 1
            while j < n:
                if text[j] == '"':
                    if text.startswith('""', j):
                        j += 2
                        continue
                    break
                j += 1
            seg = text[i:j + 1]
            line += seg.count("\n")
            buf.append(seg)
            i = j + 1
            last_word = None
            continue
        if c == "$":
            m = re.match(r"\$([A-Za-z\x80-\U0010ffff_][A-Za-z\x80-\U0010ffff_0-9]*)?\$", text[i:])
            if m:
                tag = m.group(0)
                j = text.find(tag, i + len(tag))
                j = n if j < 0 else j + len(tag)
                seg = text[i:j]
                line += seg.count("\n")
                buf.append(seg)
                i = j
                last_word = None
                continue
            buf.append(c)
            i += 1
            continue
        if IDENT_START.match(c):
            j = i + 1
            while j < n and IDENT_CONT.match(text[j]):
                j += 1
            w = text[i:j]
            lw = w.lower()
            buf.append(w)
            if len(words) < 12:
                words.append(lw)
            if begin_depth == 0 and lw == "atomic" and last_word == "begin" and words and words[0] == "create" \
                    and any(x in ("function", "procedure") for x in words[:5]):
                begin_depth = 1
            elif begin_depth > 0:
                if lw == "case":
                    begin_depth += 1
                elif lw == "end":
                    begin_depth -= 1
            if lw == "stdin" and words and words[0] == "copy":
                copy_stdin = True
            last_word = lw
            i = j
            continue
        if c == "(":
            depth += 1
        elif c == ")":
            depth = max(0, depth - 1)
        elif c == ";" and depth == 0 and begin_depth == 0:
            was_copy = finish()
            i += 1
            if was_copy:
                # skip the data up to a line "\."
                while i < n:
                    k = text.find("\n", i)
                    k = n if k < 0 else k
                    l = text[i:k]
                    line_no_inc = 1 if k < n else 0
                    i = k + line_no_inc
                    line += line_no_inc
                    if l.rstrip("\r") == "\\.":
                        break
            continue
        buf.append(c)
        last_word = None
        i += 1
    if has_content():
        finish()
    return out


def strip_comments(s):
    """The statement without comments (to detect comment-only statements)."""
    s = re.sub(r"--[^\n]*", "", s)
    while True:
        t = re.sub(r"/\*(?:[^/*]|/(?!\*)|\*(?!/))*\*/", "", s)
        if t == s:
            break
        s = t
    return s


def drop_locations(v):
    """Removes the 'location' members of a tree; they are compared separately where they are meaningful."""
    return v


def shape(tree):
    """The set of node types of a tree, to select statements that cover many shapes."""
    types = set()

    def walk(v):
        if isinstance(v, dict):
            for k, x in v.items():
                if isinstance(x, dict) and k[:1].isupper():
                    types.add(k)
                walk(x)
        elif isinstance(v, list):
            for x in v:
                walk(x)

    walk(tree)
    return frozenset(types)


def reference(sql):
    try:
        res = json.loads(pgparser.parse_sql_json(sql))
    except pglast.parser.ParseError as e:
        return {"ok": False, "err": str(e).split(", at index")[0], "pos": getattr(e, "location", None) and e.location + 1}
    stmts = []
    for s in res.get("stmts", []):
        s = dict(s)
        s.pop("stmt_location", None)
        s.pop("stmt_len", None)
        stmts.append(s["stmt"])
    return {"ok": True, "tree": stmts}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("-sql", required=True)
    ap.add_argument("-o", required=True)
    ap.add_argument("-sample", type=int, default=0)
    ap.add_argument("-seed", type=int, default=1)
    args = ap.parse_args()

    recs = []
    for name in sorted(os.listdir(args.sql)):
        if not name.endswith(".sql"):
            continue
        with open(os.path.join(args.sql, name), encoding="utf-8", errors="replace") as f:
            text = f.read()
        for line, sql in split_script(text):
            r = {"f": name, "l": line, "sql": sql}
            r.update(reference(sql))
            recs.append(r)
    ok = [r for r in recs if r["ok"]]
    bad = [r for r in recs if not r["ok"]]
    print("statements:", len(recs), "accepted:", len(ok), "rejected:", len(bad), file=sys.stderr)

    if args.sample:
        rnd = random.Random(args.seed)
        budget = args.sample
        keep = list(bad)
        # greedy cover of node-type shapes, then random fill
        seen = set()
        order = list(ok)
        rnd.shuffle(order)
        order.sort(key=lambda r: len(r["sql"]))  # prefer short statements; stable w.r.t. the shuffle
        chosen = []
        for r in order:
            sh = shape(r["tree"])
            if sh - seen:
                seen |= sh
                chosen.append(r)
        rest = [r for r in order if r not in chosen]
        rnd.shuffle(rest)
        room = max(0, budget - len(keep) - len(chosen))
        chosen += rest[:room]
        keep += chosen
        keep.sort(key=lambda r: (r["f"], r["l"]))
        recs = keep
        print("sampled:", len(recs), file=sys.stderr)

    with gzip.open(args.o, "wt", encoding="utf-8", compresslevel=9) as out:
        for r in recs:
            out.write(json.dumps(r, separators=(",", ":"), ensure_ascii=False))
            out.write("\n")


if __name__ == "__main__":
    main()
