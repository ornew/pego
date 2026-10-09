"""usage: cmpag.py AGENT_DIR  -- shows which parts of the agent differ from main/parts and from parts"""
import sys, os, difflib
W = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/'
A = W + sys.argv[1] + '/parts/'
show = sys.argv[2:]
for f in sorted(os.listdir(A)):
    a = open(A + f).read()
    m = W + 'main/parts/' + f
    p = W + 'parts/' + f
    mt = open(m).read() if os.path.exists(m) else None
    pt = open(p).read() if os.path.exists(p) else None
    print('%-24s %s | %s' % (f, 'same as main' if a == mt else ('NEW' if mt is None else 'differs from main'),
                             'same as parts' if a == pt else ('not in parts' if pt is None else 'differs from parts')))
    if f in show and mt is not None:
        for l in difflib.unified_diff(mt.split('\n'), a.split('\n'), lineterm='', n=0):
            print('   ', l[:200])
