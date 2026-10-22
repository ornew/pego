# Reference results from CPython 3.14: the program that the tests of parsers/python run (as
# python3.14 -I -c "$(cat refgen.py)" ARGS, with the interpreter named by PEGO_PYTHON) and that
# generates the recorded reference data in testdata (see the README of the module).
#
# It reads paths of files (or, with -s, JSON strings of source text) from standard input, one per
# line, or with -x ROOT takes the snippets of Python code in the files under ROOT: every string
# constant (they include the code that tests compile, valid or not) and every doctest example. It
# writes a JSON object for each: whether ast.parse accepts it, the SHA-256 of ast.dump of the tree
# (or the full dump with -d; with -a ast.dump includes the positions; with -b the first 16 digits
# of the hashes of both, as "hash" and "phash"), the error, and the source text (as CPython
# decodes it, for files, which may be in other encodings).
#
# With -b (the data that testdata keeps) the output has no source text, since that is CPython's: a snippet is given
# by where it is in the files under ROOT, and a file by its path under ROOT, with the first 8 hexadecimal digits of
# the SHA-256 of the source ("h"), which the tests check after they read the text from the files (-r ROOT names the
# directory that the paths are relative to). A snippet is
# "f", the file under ROOT, "o" and "n", the offset and size in bytes of a range of it, and when the snippet is not
# that range's text "p", the pieces that make the snippet from the range: [offset, size] copies a part of the range,
# a string is text of its own (an escape that the range spells out, for example, or the prefix of a doctest line).
import ast, difflib, doctest, hashlib, io, json, os, sys, tokenize, warnings
warnings.simplefilter("ignore")
full = "-d" in sys.argv and "-b" not in sys.argv

def pieces(base, target):
    """Returns the pieces ([offset, size] of a copy from base, or a string) that make target from base, both bytes."""
    a = base.decode("utf-8", "surrogateescape")
    b = target.decode("utf-8")
    offs = [0]
    for ch in a:
        offs.append(offs[-1] + (1 if "\udc80" <= ch <= "\udcff" else len(ch.encode("utf-8"))))
    out = []
    j = 0
    for i, k, size in difflib.SequenceMatcher(None, a, b, autojunk=False).get_matching_blocks():
        if k > j:
            out.append(b[j:k])
        if size:
            out.append([offs[i], offs[i + size] - offs[i]])
        j = k + size
    return out

def snippets(root):
    """Returns the snippets, sorted, each with where it is: (text, file, offset, size, pieces or None)."""
    found = {}  # text -> (rank, file, offset, size, pieces); a snippet that is a part of a file is better
    parser = doctest.DocTestParser()
    for dp, dn, fn in os.walk(root):
        dn.sort()
        for f in sorted(fn):
            if not f.endswith(".py"):
                continue
            path = os.path.join(dp, f)
            data = open(path, "rb").read()
            try:
                tree = ast.parse(data)
            except Exception:
                continue
            rel = os.path.relpath(path, root).replace(os.sep, "/")
            lines = [0]
            for i, c in enumerate(data):
                if c == 10:
                    lines.append(i + 1)
            def note(text, seg, start):
                if text in found and found[text][0] == 0:
                    return
                i = seg.find(text.encode("utf-8", "surrogatepass"))
                if i >= 0:
                    found[text] = (0, rel, start + i, len(text.encode("utf-8", "surrogatepass")), None)
                elif text not in found:
                    try:
                        found[text] = (1, rel, start, len(seg), pieces(seg, text.encode("utf-8")))
                    except UnicodeEncodeError:
                        found[text] = (1, rel, start, len(seg), None)  # not text; left out below
            for n in ast.walk(tree):
                if not isinstance(n, ast.Constant) or not isinstance(n.value, (str, bytes)):
                    continue
                v = n.value
                if isinstance(v, bytes):
                    try:
                        v = v.decode("utf-8")
                    except UnicodeDecodeError:
                        continue
                if not 0 < len(v) <= 5000:
                    continue
                start = lines[n.lineno - 1] + n.col_offset
                seg = data[start:lines[n.end_lineno - 1] + n.end_col_offset]
                note(v, seg, start)
                if ">>>" in v:
                    try:
                        for ex in parser.get_examples(v):
                            note(ex.source, seg, start)
                    except Exception:
                        pass
    out = []
    for s in sorted(found):
        try:
            s.encode("utf-8")
        except UnicodeEncodeError:
            continue
        out.append((s,) + found[s][1:])
    return out

places = {}
pack = "-b" in sys.argv
if "-x" in sys.argv:
    root = sys.argv[sys.argv.index("-x") + 1]
    found_snippets = snippets(root)
    inputs = [(s[0], s[0]) for s in found_snippets]
    places = {i: s[1:] for i, s in enumerate(found_snippets)}
elif "-s" in sys.argv:
    inputs = [(json.loads(line), None) for line in sys.stdin]
    inputs = [(s, s) for s, _ in inputs]
else:
    paths = [line.rstrip("\n") for line in sys.stdin]
    filesroot = sys.argv[sys.argv.index("-r") + 1] if "-r" in sys.argv else ""
    inputs = [(open(p, "rb").read(), None) for p in paths]
for i, (data, src) in enumerate(inputs):
    r = {}
    if pack and "-x" in sys.argv:
        f, o, n, p = places[i]
        r["f"], r["o"], r["n"] = f, o, n
        if p is not None:
            r["p"] = p
    if pack and "-x" not in sys.argv and "-s" not in sys.argv:
        r["path"] = os.path.relpath(paths[i], filesroot).replace(os.sep, "/")
    try:
        tree = ast.parse(data)
        d = ast.dump(tree, include_attributes="-a" in sys.argv)
        r["ok"] = True
        if full:
            r["dump"] = d
        else:
            r["hash"] = hashlib.sha256(d.encode("utf-8", "surrogatepass")).hexdigest()
        if "-b" in sys.argv:
            # for the recorded data: short hashes of the dumps without and with positions, and the
            # error of a rejected input only by its class
            r["hash"] = r["hash"][:16]
            pd = ast.dump(tree, include_attributes=True)
            r["phash"] = hashlib.sha256(pd.encode("utf-8", "surrogatepass")).hexdigest()[:16]
    except (SyntaxError, ValueError, MemoryError, RecursionError) as e:
        r["ok"] = False
        r["error"] = type(e).__name__ + ": " + str(e)
        if "-b" in sys.argv:
            r["error"] = type(e).__name__
    if src is None:
        try:
            enc, _ = tokenize.detect_encoding(io.BytesIO(data).readline)
            src = data.decode(enc)
            if src.startswith(chr(0xfeff)):
                src = src[1:]
        except Exception as e:
            r["decode_error"] = str(e)
    if pack and "-s" not in sys.argv:
        r["h"] = hashlib.sha256(src.encode("utf-8", "surrogatepass")).hexdigest()[:8]
    else:
        r["source"] = src
    print(json.dumps(r, ensure_ascii=True))
    sys.stdout.flush()
