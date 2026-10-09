"""usage: types.py Name... -- prints the Go declarations of the types from parsers/postgresql/parser.go"""
import re, sys
R = '/Users/s27814/src/github.com/ornew/pego/.claude/worktrees/agent-a84132ee088937fd9/parsers/postgresql/'
p = open(R + 'parser.go').read()
for n in sys.argv[1:]:
    m = re.search(r'^type %s (struct \{.*?^\}|[^\n]*)' % re.escape(n), p, re.M | re.S)
    print('type', n, m.group(1) if m else '(none)')
