#!/bin/sh
# prints the reference tree of the statements given as arguments
R=/Users/s27814/src/github.com/ornew/pego/.claude/worktrees/agent-a84132ee088937fd9
W=/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work
exec $R/.refsql/bin/python $W/p.py "$@"
