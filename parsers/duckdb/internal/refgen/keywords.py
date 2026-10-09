#!/usr/bin/env python3
"""Writes the keyword part of duckdb.pego: one rule per keyword the grammar uses, and the tries that tell whether a
word is a keyword that a given kind of name may not be.

Usage: keywords.py duckdb.pego [--check]

The lists in keywords/*.list are DuckDB's (third_party/libpg_query/grammar/keywords, tag v1.5.6). The grammar
refers to a keyword by its upper-case spelling (SELECT, GROUP, ORDER); this script defines every such rule between

    // BEGIN GENERATED KEYWORDS
    // END GENERATED KEYWORDS

at the end of the grammar. With --check it only reports whether the file is up to date.

The categories (PostgreSQL's, as DuckDB splits the third one in two):

  unreserved   may be any name
  column_name  may be a column or table name (ColId) but not a function or type name
  type_name    may be a type name or an operator-like word, not a column name
  func_name    may be a function name, not a column name
  reserved     may only be a label after AS or after a dot

Names (ColId and friends) accept a word that is not a keyword of a category that forbids it:

  kw_not_col_id      ColId:             anything but the keywords not in unreserved or column_name
  kw_not_func_name   function_name_token: anything but the keywords not in unreserved or func_name
  kw_not_type_name   type_name_token:   anything but the keywords not in unreserved or type_name
  kw_not_type_func   type_function_name: anything but the keywords in none of unreserved, type_name, func_name
  kw_reserved        NonReservedWord:   reserved keywords only
  kw_any             IDENT:             every keyword
"""
import os
import re
import subprocess
import sys

HERE = os.path.dirname(os.path.realpath(__file__))
BEGIN = '// BEGIN GENERATED KEYWORDS'
END = '// END GENERATED KEYWORDS'


def spelling(name):
    n = name.lower()
    return n[:-2] if n.endswith('_p') else n


def load(f):
    with open(os.path.join(HERE, 'keywords', f)) as fh:
        return {spelling(l.strip()) for l in fh if l.strip()}


def cls(c):
    if c.isalpha():
        return '(?%s%s)' % (c.lower(), c.upper())
    return '"%s"' % c


def build(words):
    root = {}
    for w in words:
        n = root
        for c in w:
            n = n.setdefault(c, {})
        n[''] = True
    return root


def emit(node, indent):
    """Returns the PEG text matching the words of the trie below node, as a sequence of items."""
    kids = sorted(k for k in node if k != '')
    term = '' in node
    alts = []
    for k in kids:
        sub = node[k]
        # a chain of single children is a sequence
        seq = cls(k)
        while True:
            sk = sorted(x for x in sub if x != '')
            if len(sk) == 1 and '' not in sub:
                seq += ' ' + cls(sk[0])
                sub = sub[sk[0]]
                continue
            break
        rest = emit(sub, indent + 2)
        if rest:
            seq += ' ' + rest
        alts.append(seq)
    if not alts:
        return ''
    if len(alts) == 1 and not term:
        return alts[0]
    pad = '\n' + ' ' * (indent + 2)
    body = (pad + '/ ').join(alts)
    out = '(' + body + ')'
    if term:
        out += '?'
    return out


def trie_rule(name, words, doc):
    t = build(sorted(words))
    text = emit(t, 4)
    return '// %s\ndef %s = %s\n    !%s\n' % (doc, name, text.replace('\n', '\n    '), WC)


def main():
    path = sys.argv[1]
    check = '--check' in sys.argv
    U = load('unreserved_keywords.list')
    C = load('column_name_keywords.list')
    T = load('type_name_keywords.list')
    F = load('func_name_keywords.list')
    R = load('reserved_keywords.list')
    K = U | C | T | F | R
    src = open(path, encoding='utf-8').read()
    # the keywords test the character after them with the class of the rule wc, written out: a rule that calls no
    # other rule is not memoized, which saves most of the memory of a parse
    global WC
    WC = re.search(r'^def wc = (\(\?.*\))$', src, re.M).group(1)
    b = src.index(BEGIN)
    e = src.index(END)
    body = src[:b] + src[e + len(END):]
    used = set()
    for m in re.finditer(r'(?<![A-Za-z0-9_"\\])([A-Z][A-Z0-9_]*)(?![A-Za-z0-9_"])', re.sub(r'\(\?\^?[^)]*\)', '', re.sub(r'//[^\n]*', '', body))):
        used.add(m.group(1))
    # an upper-case word that is not a keyword is a type name or a rule of its own: types start with one
    # capital followed by lower-case letters; anything all upper-case must be a keyword
    kws = sorted(u for u in used if u.lower() in K)
    unknown = sorted(u for u in used if u.lower() not in K and len(u) > 1)
    if unknown:
        print('upper-case names that are not keywords:', unknown, file=sys.stderr)
    out = []
    out.append(BEGIN + ' (internal/refgen/keywords.py; do not edit)\n')
    out.append('// The keywords are matched without regard to ASCII case, and only as whole words.\n')
    for k in kws:
        out.append('def %s = %s !%s\n' % (k.upper(), ' '.join(cls(c) for c in k), WC))
    out.append('')
    # The keywords that no kind of name accepts are in tries of their own; the words of the categories overlap, so
    # they are split into disjoint classes first: the keyword rules of the grammar use the same words.
    only = lambda S, *others: S - set().union(*others)
    classes = [
        ('kw_r', 'reserved', R),
        ('kw_c', 'column_name only', only(C, U, T, F, R)),
        ('kw_cf', 'column_name and func_name', (C & F) - T - U - R),
        ('kw_ct', 'column_name and type_name', (C & T) - F - U - R),
        ('kw_t', 'type_name only', only(T, C, F, U, R)),
        ('kw_tf', 'type_name and func_name', (T & F) - C - U - R),
    ]
    assert (set().union(*[c[2] for c in classes]) | U) == K
    for name, doc, words in classes:
        out.append(trie_rule(name, words, doc))
    composites = [
        ('kw_not_col_id', 'ColId may not be one of these keywords', ['kw_r', 'kw_t', 'kw_tf']),
        ('kw_not_func_name', 'function_name_token may not be one of these', ['kw_r', 'kw_c', 'kw_ct', 'kw_t']),
        ('kw_not_type_name', 'type_name_token may not be one of these', ['kw_r', 'kw_c', 'kw_cf']),
        ('kw_not_type_func', 'type_function_name may not be one of these', ['kw_r', 'kw_c']),
        ('kw_reserved', 'the reserved keywords', ['kw_r']),
        ('kw_any', 'every keyword, without regard to ASCII case', ['kw_r', 'kw_c', 'kw_cf', 'kw_ct', 'kw_t', 'kw_tf', 'kw_u']),
    ]
    # An IDENT must exclude every keyword in every ASCII spelling, even when
    # unreserved keywords remain legal in ColId and other name contexts.
    # Share prefixes and test the complete word boundary, as for other classes.
    out.append(trie_rule('kw_u', U, 'the unreserved keywords, without regard to ASCII case'))
    for name, doc, parts in composites:
        out.append('// %s\ndef %s = %s\n' % (doc, name, ' / '.join(parts)))
    out.append(END)
    new = src[:b] + '\n'.join(out) + src[e + len(END):]
    # keywords.go: the category of every keyword. A word in two lists takes the category DuckDB reports.
    gofile = os.path.join(os.path.dirname(os.path.abspath(path)), 'keywords.go')
    gosrc = ['// Code generated by internal/refgen/keywords.py from DuckDB\'s keyword lists. DO NOT EDIT.', '',
             'package duckdb', '',
             '// keywords maps every keyword of DuckDB 1.5.6, in lower case, to its category.',
             'var keywords = map[string]KeywordCategory{']
    cat_of = {}
    for name, S in [('Unreserved', U), ('Reserved', R), ('TypeFunction', T | F), ('ColumnName', C)]:
        for w in S:
            cat_of.setdefault(w, name)
    for w in sorted(cat_of):
        gosrc.append('\t"%s": %s,' % (w, cat_of[w]))
    gosrc.append('}')
    gosrc.append('')
    gonew = subprocess.run(['gofmt'], input='\n'.join(gosrc), text=True,
                           capture_output=True, check=True).stdout
    goold = open(gofile, encoding='utf-8').read() if os.path.exists(gofile) else ''
    if check:
        print('up to date' if new == src and gonew == goold else 'out of date')
        sys.exit(0 if new == src and gonew == goold else 1)
    if new != src:
        open(path, 'w', encoding='utf-8').write(new)
        print('rewrote', path, len(kws), 'keywords used', file=sys.stderr)
    if gonew != goold:
        open(gofile, 'w', encoding='utf-8').write(gonew)


if __name__ == '__main__':
    main()
