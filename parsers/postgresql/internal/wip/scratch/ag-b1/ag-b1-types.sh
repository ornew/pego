#!/bin/sh
# usage: sh ag-b1-types.sh [cmp.py args]   (run pgrun.sh first: it writes out.jsonl)
# Compares the statements of the corpus that the reference accepts and whose raw node is one of the statements of
# region B1 (DROP only for CAST, TRANSFORM, OPERATOR CLASS and OPERATOR FAMILY).
W=/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work
R=/Users/s27814/src/github.com/ornew/pego/.claude/worktrees/agent-a84132ee088937fd9
D=$W/ag-b1
TYPES=CreateFunctionStmt,AlterFunctionStmt,CreateCastStmt,CreateTransformStmt,DropStmt,DefineStmt,CompositeTypeStmt,CreateEnumStmt,CreateRangeStmt,AlterEnumStmt,AlterTypeStmt,CreateOpClassStmt,CreateOpFamilyStmt,AlterOpFamilyStmt,AlterOperatorStmt,AlterTSDictionaryStmt,AlterTSConfigurationStmt,CreateConversionStmt,AlterCollationStmt,CreatePLangStmt,CreateExtensionStmt,AlterExtensionStmt,AlterExtensionContentsStmt,CreateAmStmt
export TYPES
PGDIR=$D $R/.refsql/bin/python $D/ag-b1-cmp.py $W/full.jsonl.gz $D/out.jsonl "$@"
