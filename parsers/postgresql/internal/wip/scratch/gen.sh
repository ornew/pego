#!/bin/sh
# usage: gen.sh [main]   regenerates parsers/postgresql/parser.go with the engine of the worktree, or with
# the engine of main (a build in pego-main) for measurements.
R=/Users/s27814/src/github.com/ornew/pego/.claude/worktrees/agent-a84132ee088937fd9
W=/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work
cd $R || exit 1
if [ "$1" = "main" ]; then
  $W/pego-main gen -g parsers/postgresql/postgresql.pego -pkg postgresql -types -recognize -nodoc -o parsers/postgresql/parser.go
else
  go run ./cmd/pego gen -g parsers/postgresql/postgresql.pego -pkg postgresql -types -recognize -nodoc -o parsers/postgresql/parser.go
fi
