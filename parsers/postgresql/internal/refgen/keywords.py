#!/usr/bin/env python3
"""Maintains the keyword tables of postgresql.pego.

The grammar has, between the markers

    // BEGIN GENERATED KEYWORDS
    // END GENERATED KEYWORDS

the lexical rules that depend on the keyword lists of PostgreSQL (src/include/parser/kwlist.h): one rule per
keyword that the grammar uses (named like the token of gram.y without the suffix _P: SELECT, IF, ...), and
the rules that exclude whole categories of keywords from names (reserved, column_name_keyword, ...).

    python3 keywords.py table OUT.txt      # with pglast: writes the table of keywords and categories
    python3 keywords.py update GRAMMAR.pego [TABLE.txt]   # rewrites the generated section

The table (keywords.txt next to this script) has one line per keyword: the word, its category (R reserved,
U unreserved, C column name, T type or function name) and B if it can be a column label without AS (the
BARE_LABEL keywords). The table is produced from pglast's keyword lists and the reference parser (a keyword
is a bare label if "select 1 <word>" is accepted).
"""

import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))


def make_table(out):
    import json
    from pglast import keywords, parser

    cat = {}
    for c, words in (("R", keywords.RESERVED_KEYWORDS), ("U", keywords.UNRESERVED_KEYWORDS),
                     ("C", keywords.COL_NAME_KEYWORDS), ("T", keywords.TYPE_FUNC_NAME_KEYWORDS)):
        for w in words:
            cat[w] = c
    lines = []
    for w in sorted(cat):
        bare = True
        try:
            parser.parse_sql_json("select 1 " + w)
        except Exception:
            bare = False
        lines.append("%s %s%s" % (w, cat[w], " B" if bare else ""))
    with open(out, "w") as f:
        f.write("\n".join(lines) + "\n")


def read_table(path):
    kws = {}
    for line in open(path):
        parts = line.split()
        if parts:
            kws[parts[0]] = (parts[1], len(parts) > 2)
    return kws


def ci(word):
    """The case-insensitive match of a word, as character classes."""
    out = []
    for ch in word:
        if ch.isalpha():
            out.append("(?%s%s)" % (ch.lower(), ch.upper()))
        elif ch == "_":
            out.append("\"_\"")
        else:
            raise ValueError(word)
    return "".join(out)


def trie(words, prefix_len=0):
    """A choice that matches any of the words (case-insensitively, as a prefix), grouped by first letters so
    that a word that is not in the set is rejected after a few comparisons. The caller checks the end of
    the word."""
    words = sorted(set(words))
    groups = {}
    for w in words:
        groups.setdefault(w[prefix_len:prefix_len + 1], []).append(w)
    alts = []
    for ch, ws in sorted(groups.items()):
        if ch == "":
            alts.append("_")  # the word ends here
            continue
        cls = "(?%s%s)" % (ch.lower(), ch.upper()) if ch.isalpha() else "\"%s\"" % ch
        if len(ws) == 1:
            alts.append(cls + ci(ws[0][prefix_len + 1:]) if ws[0][prefix_len + 1:] else cls)
        else:
            sub = trie(ws, prefix_len + 1)
            alts.append(cls + " (" + sub + ")")
    # "_" (empty) must come last in a choice
    alts.sort(key=lambda a: a == "_")
    return " / ".join(alts)


def tokname(w):
    return w.upper()


def grouped(words):
    """A choice of the keyword rules of the words, grouped by first letter: the class of the letter is
    tested once for a group, so that a word that is not in the set is rejected after a few steps."""
    groups = {}
    for w in sorted(words):
        groups.setdefault(w[0], []).append(w)
    alts = []
    for ch, ws in sorted(groups.items()):
        alts.append("&(?%s%s) (%s)" % (ch.lower(), ch.upper(), " / ".join(tokname(w) for w in ws)))
    return " / ".join(alts)


def member(words):
    """A boolean expression that tells whether $w.V is one of the words."""
    return " || ".join('$w.V == "%s"' % w for w in sorted(words))


def bucketed(name, words):
    """A rule that tells whether the word at the input is one of the words: the alternatives are the letters
    the words begin with, so that a word is compared only with the words that begin like it."""
    alts = []
    for c in sorted({w[0] for w in words}):
        alts.append("&(?%s%s) w:lcword [%s]" % (c.upper(), c, member([w for w in words if w[0] == c])))
    return "def %s = %s" % (name, "\n    / ".join(alts))


FOLD = """// A word folded to lower case: the keywords are matched by comparing it with their lower-case spelling. The
// word is read once: the rule is memoized at its position, so all the keywords that are tried there share it.
// lowercase runs are appended as they are, the upper-case letters one by one, a non-ASCII letter as a mark
// that no keyword has.
type LcWord struct { V string }
def lcword: LcWord = w:lcword_read -> $w
def lcword_read: LcWord = &(?A-Za-z_\\u{80}-\\u{10FFFF}) [lw = ""]
    ( r:@((?a-z0-9_$)+) [lw = lw + text($r)]
%s
    / (?\\u{80}-\\u{10FFFF}) [lw = lw + "\\u{80}"] )+
    -> new LcWord{V: lw}
// A keyword rule sets the variable kwid to the keyword, and kw matches the word that follows if it is the one.
def kw = w:lcword [$w.V == kwid] s"""


def generate(grammar_text, kws):
    used = set(re.findall(r"\b([A-Z][A-Z_0-9]*)\b", re.sub(r"//[^\n]*", "", strip_generated(grammar_text))))
    out = ["// BEGIN GENERATED KEYWORDS (python3 internal/refgen/keywords.py update postgresql.pego)"]
    upper = []
    for i in range(26):
        c = chr(65 + i)
        upper.append('    / (?%s) [lw = lw + "%s"]' % (c, c.lower()))
    lines = []
    cur = ""
    for u in upper:
        if len(cur) + len(u) > 100:
            lines.append(cur)
            cur = ""
        cur += u.lstrip() + " " if cur else u
    lines.append(cur)
    out.append(FOLD % "\n".join(l.rstrip() for l in lines))
    sets = {
        "reserved_word": [w for w, (c, b) in kws.items() if c == "R"],
        "type_func_name_word": [w for w, (c, b) in kws.items() if c == "T"],
        "col_name_word": [w for w, (c, b) in kws.items() if c == "C"],
        "label_only_word": [w for w, (c, b) in kws.items() if not b],
    }
    out.append("")
    for w in sorted(kws):
        if tokname(w) in used:
            out.append('def %s = &(?%s%s) [kwid = "%s"] kw' % (tokname(w), w[0].upper(), w[0], w))
    out.append("")
    out.append("// The categories of keywords that cannot be names: tests on the lower-case word.")
    for name, words in sets.items():
        out.append(bucketed(name, words))
    both = lambda *cs: [w for w, (c, b) in kws.items() if c in cs]
    out.append(bucketed("reserved_or_type_func_word", both("R", "T")))
    out.append(bucketed("reserved_or_col_name_word", both("R", "C")))
    out.append("// END GENERATED KEYWORDS")
    return "\n".join(out)


def strip_generated(text):
    text = re.sub(r"// BEGIN GENERATED KEYWORDS.*?// END GENERATED KEYWORDS", "", text, flags=re.S)
    return re.sub(r"// BEGIN GENERATED ANY.*?// END GENERATED ANY", "", text, flags=re.S)


def generate_any(text):
    """type Any = ...: the union of every struct and terminal type of the grammar."""
    names = re.findall(r"^type (\w+) (?:struct|terminal)\b", strip_generated(text), flags=re.M)
    names = [n for n in names if n != "Any"]
    lines = ["// BEGIN GENERATED ANY (python3 internal/refgen/keywords.py update postgresql.pego)",
             "// Any is the union of all node types: the type of the nodes in lists of unknown contents."]
    out = []
    cur = "type Any = " + names[0]
    for n in names[1:]:
        if len(cur) + len(n) + 3 > 110:
            out.append(cur + " |")
            cur = "    " + n
        else:
            cur += " | " + n
    out.append(cur)
    lines += out
    lines.append("// END GENERATED ANY")
    return "\n".join(lines)


def update(path, table):
    kws = read_table(table)
    text = open(path).read()
    gen = generate(text, kws)
    if "// BEGIN GENERATED KEYWORDS" in text:
        text = re.sub(r"// BEGIN GENERATED KEYWORDS.*?// END GENERATED KEYWORDS", lambda m: gen, text, flags=re.S)
    else:
        text = text.rstrip("\n") + "\n\n" + gen + "\n"
    anyu = generate_any(text)
    if "// BEGIN GENERATED ANY" in text:
        text = re.sub(r"// BEGIN GENERATED ANY.*?// END GENERATED ANY", lambda m: anyu, text, flags=re.S)
    else:
        text = text.rstrip("\n") + "\n\n" + anyu + "\n"
    open(path, "w").write(text)


if __name__ == "__main__":
    if sys.argv[1] == "table":
        make_table(sys.argv[2])
    elif sys.argv[1] == "update":
        update(sys.argv[2], sys.argv[3] if len(sys.argv) > 3 else os.path.join(HERE, "keywords.txt"))
    else:
        sys.exit(__doc__)
