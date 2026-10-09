import re, sys, collections, glob

W = '/private/tmp/claude-240327714/-Users-s27814-src-github-com-ornew-pego/bde1b762-0570-4a12-9e68-6c76f7e9531e/scratchpad/postgresql-work/'
text = open(W + 'gram.condensed').read()
# split productions
prods = {}
order = []
cur = None
for line in text.split('\n'):
    m = re.match(r'^([A-Za-z_][A-Za-z_0-9]*):(.*)$', line)
    if m:
        cur = m.group(1)
        prods[cur] = []
        order.append(cur)
        rest = m.group(2)
        prods[cur].append(rest)
    elif cur is not None:
        prods[cur].append(line)
rhs = {}
for k, v in prods.items():
    body = ' '.join(v)
    syms = set(re.findall(r"[A-Za-z_][A-Za-z_0-9]*", body))
    rhs[k] = syms
nts = set(prods)

core = set()
for f in glob.glob(W + 'main/parts/*.pego'):
    for m in re.finditer(r'^def (\w+)', open(f).read(), flags=re.M):
        core.add(m.group(1))
# gram.y names that the core defines under another name
core |= {'SelectStmt', 'select_no_parens', 'select_with_parens', 'InsertStmt', 'UpdateStmt', 'DeleteStmt', 'MergeStmt',
         'a_expr', 'b_expr', 'c_expr', 'func_expr_windowless', 'func_expr', 'func_application', 'func_expr_common_subexpr',
         'Typename', 'SimpleTypename', 'GenericType', 'ConstTypename', 'Numeric', 'Bit', 'Character', 'ConstDatetime',
         'ConstInterval', 'opt_interval', 'AexprConst', 'columnref', 'indirection', 'opt_indirection', 'indirection_el',
         'qualified_name', 'any_name', 'name', 'attr_name', 'attrs', 'func_name', 'name_list', 'qualified_name_list',
         'ColId', 'ColLabel', 'NonReservedWord', 'type_function_name', 'BareColLabel', 'Iconst', 'Sconst', 'IDENT',
         'relation_expr', 'relation_expr_list', 'relation_expr_opt_alias', 'table_ref', 'alias_clause', 'opt_alias_clause',
         'expr_list', 'target_list', 'from_list', 'where_clause', 'sort_clause', 'opt_sort_clause', 'index_params',
         'index_elem', 'opt_collate', 'opt_collate_clause', 'definition', 'def_list', 'def_elem', 'def_arg', 'reloptions',
         'opt_definition', 'opt_reloptions', 'NumericOnly', 'SignedIconst', 'RoleSpec', 'role_list', 'func_arg',
         'func_args', 'func_type', 'param_name', 'function_with_argtypes', 'function_with_argtypes_list',
         'aggregate_with_argtypes', 'aggr_args', 'operator_with_argtypes', 'oper_argtypes', 'any_operator',
         'qual_all_Op', 'qual_Op', 'all_Op', 'PreparableStmt', 'with_clause', 'opt_with_clause', 'opt_column_list',
         'columnList', 'columnElem', 'any_name_list', 'type_list', 'TableFuncElementList', 'TableFuncElement',
         'OptTableFuncElementList', 'cursor_name', 'opt_name_list', 'values_clause', 'json_value_expr', 'json_returning_clause_opt',
         'opt_boolean_or_string', 'func_arg_list', 'func_arg_expr', 'window_specification', 'opt_ordinality',
         'opt_asc_desc', 'opt_nulls_order', 'ConstraintAttributeSpec'}

groups = {
    'A': ['CreateStmt', 'CreateAsStmt', 'CreateMatViewStmt', 'RefreshMatViewStmt', 'CreateSeqStmt', 'AlterSeqStmt',
          'CreateStatsStmt', 'AlterStatsStmt', 'IndexStmt', 'ViewStmt', 'AlterTableStmt', 'AlterCompositeTypeStmt',
          'CreateDomainStmt', 'AlterDomainStmt', 'CreateForeignTableStmt'],
    'B1': ['CreateFunctionStmt', 'AlterFunctionStmt', 'CreateCastStmt', 'DropCastStmt', 'CreateTransformStmt',
           'DropTransformStmt', 'DefineStmt', 'AlterEnumStmt', 'CreateOpClassStmt', 'CreateOpFamilyStmt',
           'AlterOpFamilyStmt', 'DropOpClassStmt', 'DropOpFamilyStmt', 'AlterOperatorStmt', 'AlterTypeStmt',
           'AlterTSDictionaryStmt', 'AlterTSConfigurationStmt', 'CreateConversionStmt', 'CreatePLangStmt',
           'CreateExtensionStmt', 'AlterExtensionStmt', 'AlterExtensionContentsStmt', 'AlterCollationStmt',
           'CreateAmStmt'],
    'B2': ['CreateFdwStmt', 'AlterFdwStmt', 'CreateForeignServerStmt', 'AlterForeignServerStmt', 'ImportForeignSchemaStmt',
           'CreateUserMappingStmt', 'AlterUserMappingStmt', 'DropUserMappingStmt', 'CreatePublicationStmt',
           'AlterPublicationStmt', 'CreateSubscriptionStmt', 'AlterSubscriptionStmt', 'DropSubscriptionStmt',
           'CreateEventTrigStmt', 'AlterEventTrigStmt', 'CreateTrigStmt', 'RuleStmt', 'CreatePolicyStmt',
           'AlterPolicyStmt', 'CreateTableSpaceStmt', 'DropTableSpaceStmt', 'AlterTblSpcStmt', 'CreateAssertionStmt',
           'CreatedbStmt', 'AlterDatabaseStmt', 'AlterDatabaseSetStmt', 'DropdbStmt', 'CreateRoleStmt',
           'AlterRoleStmt', 'AlterRoleSetStmt', 'DropRoleStmt', 'CreateGroupStmt', 'AlterGroupStmt', 'CreateUserStmt',
           'DropOwnedStmt', 'ReassignOwnedStmt'],
    'C1': ['VariableSetStmt', 'VariableResetStmt', 'VariableShowStmt', 'ConstraintsSetStmt', 'CheckPointStmt',
           'DiscardStmt', 'TransactionStmt', 'TransactionStmtLegacy', 'CopyStmt', 'ExplainStmt', 'VacuumStmt',
           'AnalyzeStmt', 'ClusterStmt', 'ReindexStmt', 'LockStmt', 'PrepareStmt', 'ExecuteStmt', 'DeallocateStmt',
           'DeclareCursorStmt', 'FetchStmt', 'ClosePortalStmt', 'ListenStmt', 'UnlistenStmt', 'NotifyStmt',
           'LoadStmt', 'DoStmt', 'CallStmt', 'TruncateStmt', 'AlterSystemStmt'],
    'C2': ['DropStmt', 'RemoveFuncStmt', 'RemoveAggrStmt', 'RemoveOperStmt', 'RenameStmt', 'AlterObjectSchemaStmt',
           'AlterOwnerStmt', 'AlterObjectDependsStmt', 'CommentStmt', 'SecLabelStmt', 'GrantStmt', 'RevokeStmt',
           'GrantRoleStmt', 'RevokeRoleStmt', 'AlterDefaultPrivilegesStmt'],
}


def closure(starts):
    seen = set()
    todo = list(starts)
    while todo:
        x = todo.pop()
        if x in seen or x not in nts:
            continue
        seen.add(x)
        if x in core and x not in starts:
            continue
        todo.extend(rhs[x])
    return seen


reach = {g: closure(s) for g, s in groups.items()}
owner = collections.defaultdict(set)
for g, s in reach.items():
    for x in s:
        if x not in core or x in groups[g]:
            owner[x].add(g)
shared = {x: sorted(g) for x, g in owner.items() if len(g) > 1}
print('shared among groups (not in core):')
for x, g in sorted(shared.items(), key=lambda kv: (kv[1], kv[0])):
    print(' ', x, g)
allstart = set(sum(groups.values(), []))
print('\nstatements not assigned:')
stmts = re.findall(r'^\t\t\t\t\|? ?(\w+)$', '\n'.join(prods['stmt']), flags=re.M)
for s in sorted(set(re.findall(r'\b(\w+Stmt\w*)\b', ' '.join(prods['stmt'])))):
    if s not in allstart and s not in core:
        print(' ', s)
for g, s in reach.items():
    print(g, len([x for x in s if x not in core]), 'nonterminals')
