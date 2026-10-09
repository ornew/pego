P = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/'


def sub(f, a, b):
    t = open(P + f).read()
    assert a in t, (f, a[:60])
    open(P + f, 'w').write(t.replace(a, b, 1))


sub('cmp.py', '''def conv_node(n, in_list, tname=None, fname=None):
    t = n['type']''', '''# Extension points, filled by the files of the ext directory next to this script (see AGENTS.md):
#   TERMINAL_HOOKS[type] = fn(text, in_list) -> value        (a terminal of a grammar type)
#   NODE_HOOKS[type] = fn(node, fields, in_list) -> value     (a struct node: fields is node['fields'])
#   POST_HOOKS.append(fn(dict) -> dict or None)               (applied to every converted dict, children first)
TERMINAL_HOOKS = {}
NODE_HOOKS = {}
POST_HOOKS = []


def conv_node(n, in_list, tname=None, fname=None):
    t = n['type']
    if t in TERMINAL_HOOKS:
        return TERMINAL_HOOKS[t](n.get('text'), in_list)
    if t in NODE_HOOKS:
        return NODE_HOOKS[t](n, n.get('fields', {}), in_list)''')
sub('cmp.py', '''        return r
    return x


def fix_const(c):''', '''        for h in POST_HOOKS:
            r2 = h(r)
            if r2 is not None:
                r = r2
        return r
    return x


def fix_const(c):''')
sub('cmp.py', '''def main():
    corpus = sys.argv[1]''', '''def load_ext():
    import glob, os
    for f in sorted(glob.glob(os.path.join(os.path.dirname(os.path.abspath(__file__)), 'ext', '*.py'))):
        exec(open(f).read(), globals())


def main():
    load_ext()
    corpus = sys.argv[1]''')
