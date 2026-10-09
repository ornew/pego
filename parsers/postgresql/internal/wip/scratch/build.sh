#!/bin/sh
# Assembles parsers/postgresql/postgresql.pego from the parts and regenerates the keyword rules.
W=/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work
R=/Users/s27814/src/github.com/ornew/pego/.claude/worktrees/agent-a84132ee088937fd9
cat $W/parts/*.pego > $R/parsers/postgresql/postgresql.pego || exit 1
python3 $R/parsers/postgresql/internal/refgen/keywords.py update $R/parsers/postgresql/postgresql.pego || exit 1
$W/pego fmt -l $R/parsers/postgresql/postgresql.pego
echo built
