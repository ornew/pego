#!/bin/sh
# usage: t.sh RULE 'input' [more inputs...]   parses each input with the rule and prints the tree (cut)
R=/Users/s27814/src/github.com/ornew/pego/.claude/worktrees/agent-a84132ee088937fd9
RULE=$1
shift
for IN in "$@"; do
  echo "== $IN"
  $R/.w/pego parse -g $R/parsers/postgresql/postgresql.pego -s "$RULE" -i "$IN" -f sexpr 2>&1 | cut -c1-${W:-260}
done
