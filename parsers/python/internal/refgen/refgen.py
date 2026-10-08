# Reference results from CPython 3.14: the program that the tests of parsers/python run (as
# python3.14 -I -c "$(cat refgen.py)" ARGS, with the interpreter named by PEGO_PYTHON) and that
# generates the vendored reference data in testdata (see the README of the module).
#
# It reads paths of files (or, with -s, JSON strings of source text) from standard input, one per
# line, or with -x ROOT takes the snippets of Python code in the files under ROOT: every string
# constant (they include the code that tests compile, valid or not) and every doctest example. It
# writes a JSON object for each: whether ast.parse accepts it, the SHA-256 of ast.dump of the tree
# (or the full dump with -d; with -a ast.dump includes the positions; with -b the first 16 digits
# of the hashes of both, as "hash" and "phash"), the error, and the source text (as CPython
# decodes it, for files, which may be in other encodings).
import ast, doctest, hashlib, io, json, os, sys, tokenize, warnings
warnings.simplefilter("ignore")
full = "-d" in sys.argv and "-b" not in sys.argv

def snippets(root):
    found = set()
    parser = doctest.DocTestParser()
    for dp, dn, fn in os.walk(root):
        for f in sorted(fn):
            if not f.endswith(".py"):
                continue
            try:
                tree = ast.parse(open(os.path.join(dp, f), "rb").read())
            except Exception:
                continue
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
                found.add(v)
                if ">>>" in v:
                    try:
                        found.update(ex.source for ex in parser.get_examples(v))
                    except Exception:
                        pass
    out = []
    for s in sorted(found):
        try:
            s.encode("utf-8")
        except UnicodeEncodeError:
            continue
        out.append(s)
    return out

if "-x" in sys.argv:
    inputs = [(s, s) for s in snippets(sys.argv[sys.argv.index("-x") + 1])]
elif "-s" in sys.argv:
    inputs = [(json.loads(line), None) for line in sys.stdin]
    inputs = [(s, s) for s, _ in inputs]
else:
    paths = [line.rstrip("\n") for line in sys.stdin]
    inputs = [(open(p, "rb").read(), None) for p in paths]
for i, (data, src) in enumerate(inputs):
    r = {}
    if "-b" in sys.argv and "-x" not in sys.argv and "-s" not in sys.argv:
        r["path"] = os.path.basename(paths[i])
    try:
        tree = ast.parse(data)
        d = ast.dump(tree, include_attributes="-a" in sys.argv)
        r["ok"] = True
        if full:
            r["dump"] = d
        else:
            r["hash"] = hashlib.sha256(d.encode("utf-8", "surrogatepass")).hexdigest()
        if "-b" in sys.argv:
            # for the vendored data: short hashes of the dumps without and with positions, and the
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
    r["source"] = src
    print(json.dumps(r, ensure_ascii=True))
    sys.stdout.flush()
