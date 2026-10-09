import re
P = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/parts/'


def sub(f, a, b):
    t = open(P + f).read()
    assert a in t, (f, a[:60])
    open(P + f, 'w').write(t.replace(a, b))


# names: no variables
sub('20-names.pego', '''// ==== Names ====
//
// The raw parser builds a RangeVar from a qualified name and then sets fields on it (relpersistence for
// temporary tables, inh for ONLY). The rules here read these two variables when they build it; the rules
// that need other values define them before they call qualified_name. main defines the defaults.
''', '''// ==== Names ====
''')
t = open(P + '20-names.pego').read()
t = t.replace('Inh: inh, Relpersistence: persistence', 'Inh: true, Relpersistence: "p"')
open(P + '20-names.pego', 'w').write(t)

# 41: relations
t = open(P + '41-clauses.pego').read()
a = t.index('def table_from:')
b = t.index('def into_clause:')
t = t[:a] + '''def table_from: []TableRef = r:relation_expr -> list($r)

''' + t[b:]
a = t.index('def into_clause:')
b = t.index('def from_clause:')
t = t[:a] + '''def into_clause: IntoClause = INTO r:opt_temp_table_name -> new IntoClause{Rel: $r, OnCommit: "ONCOMMIT_NOOP"}
// OptTempTableName: a table name with its persistence
def opt_temp_table_name: RangeVar = temp_table_name / unlogged_table_name / table_table_name / qualified_name
def temp_table_name: RangeVar =
    (TEMPORARY / TEMP / LOCAL TEMPORARY / LOCAL TEMP / GLOBAL TEMPORARY / GLOBAL TEMP) TABLE? n:qualified_name
    -> new RangeVar{Catalogname: $n.Catalogname, Schemaname: $n.Schemaname, Relname: $n.Relname, Inh: true, Relpersistence: "t"}
def unlogged_table_name: RangeVar = UNLOGGED TABLE? n:qualified_name
    -> new RangeVar{Catalogname: $n.Catalogname, Schemaname: $n.Schemaname, Relname: $n.Relname, Inh: true, Relpersistence: "u"}
def table_table_name: RangeVar = TABLE n:qualified_name -> $n

''' + t[b:]
t = t.replace('''def locked_rel_list: []RangeVar = first:plain_name rest:(-COMMA x:plain_name)* -> concat(list($first), map($rest, (r) => $r.x))''',
              '''def locked_rel_list: []RangeVar = qualified_name_list''')
a = t.index('def table_primary:')
b = t.index('def subselect_table:')
t = t[:a] + '''def table_primary: TableRef = relation_table / sample_table / func_table_ref / subselect_table / paren_join_alias / paren_join

// relation_expr: a table name, with * (the same as without it) or ONLY
def relation_expr: RangeVar = rel_only_paren / rel_only / rel_plain
def rel_plain: RangeVar = r:qualified_name -STAR? -> $r
def rel_only: RangeVar = ONLY n:qualified_name
    -> new RangeVar{Catalogname: $n.Catalogname, Schemaname: $n.Schemaname, Relname: $n.Relname, Inh: false, Relpersistence: "p"}
def rel_only_paren: RangeVar = ONLY -LP n:qualified_name -RP
    -> new RangeVar{Catalogname: $n.Catalogname, Schemaname: $n.Schemaname, Relname: $n.Relname, Inh: false, Relpersistence: "p"}
def relation_expr_list: []RangeVar = first:relation_expr rest:(-COMMA x:relation_expr)* -> concat(list($first), map($rest, (r) => $r.x))
// relation_expr opt_alias_clause
def relation_alias: RangeVar = r:relation_expr a:alias_clause?
    -> new RangeVar{Catalogname: $r.Catalogname, Schemaname: $r.Schemaname, Relname: $r.Relname, Inh: $r.Inh, Relpersistence: $r.Relpersistence, Alias: $a}
def relation_table: RangeVar = r:relation_alias !TABLESAMPLE -> $r
def sample_table: RangeTableSample = r:relation_alias TABLESAMPLE m:func_name -LP a:expr_list -RP rp:repeatable_clause?
    -> new RangeTableSample{Relation: $r, Method: $m, Args: $a, Repeatable: $rp}
def repeatable_clause: Expr = REPEATABLE -LP e:a_expr -RP -> $e

''' + t[b:]
# drop the alias-variable rules
a = t.index('// An alias after a relation, read by qualified_name')
b = t.index('// Function calls in FROM')
t = t[:a] + t[b:]
open(P + '41-clauses.pego', 'w').write(t)

# 90 main
sub('90-main.pego', '''def main: Script = [persistence = "p"] [inh = true] [star_ok = false] [alias_mode = 0] [only_paren = false]
    [windowless = false] [tails_ok = true] [setof = false] [consttype = false] [dolq = ""]''', '''def main: Script = [windowless = false] [tails_ok = true] [setof = false] [consttype = false] [dolq = ""]''')
