import sys, subprocess, os
W = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work'
words = [l.split()[0] for l in open('/Users/s27814/src/github.com/ornew/pego/.claude/worktrees/agent-a84132ee088937fd9/parsers/postgresql/internal/refgen/keywords.txt')][:200]
variant = sys.argv[1]

def ci(w):
    return ''.join('(?%s%s)' % (c.lower(), c.upper()) if c.isalpha() else '"_"' for c in w)

out = ['package kw', 'def main = s kws $$', 'def s = (? \\t\\n)*', 'def ic = (?A-Za-z0-9_)']
names = []
for w in words:
    n = w.upper()
    names.append(n)
    if variant == 'seq':
        out.append('def %s = %s !ic s' % (n, ci(w)))
    elif variant == 'atomic':
        out.append('def %s = @(%s) !ic s' % (n, ci(w)))
    elif variant == 'lit':
        out.append('def %s = "%s" !ic s' % (n, w))
    elif variant == 'word':
        out.append('def %s = w:word [text($w) == "%s"] s' % (n, w))
out.append('def word = (?A-Za-z_) ic*')
out.append('def kws = ' + ' / '.join(names))
open(W + '/kw_' + variant + '.pego', 'w').write('\n'.join(out) + '\n')
r = subprocess.run([W + '/pego', 'gen', '-g', W + '/kw_' + variant + '.pego', '-pkg', 'kw', '-recognize', '-o', W + '/kw_' + variant + '.go'], capture_output=True, text=True)
print(variant, r.stderr[:300])
print(sum(1 for _ in open(W + '/kw_' + variant + '.go')))
