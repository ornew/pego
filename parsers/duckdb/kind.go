package duckdb

// Kind returns the type DuckDB gives the statement (the name of its StatementType: SELECT, INSERT, CREATE, ...).
//
// DuckDB rewrites some statements while it parses them, so a statement can have another type in DuckDB than its
// syntax says: PRAGMA is a SELECT when it calls a function or a SET when it assigns a value, SHOW, DESCRIBE and
// SUMMARIZE are SELECT statements, CHECKPOINT is a CALL, USE and RESET are SET statements, ANALYZE a VACUUM, and
// COMMENT ON an ALTER. Kind of a PragmaStmt is "PRAGMA".
func Kind(s Statement) string {
	switch s := s.(type) {
	case *Select, *ShowStmt:
		return "SELECT"
	case *InsertStmt:
		return "INSERT"
	case *UpdateStmt:
		return "UPDATE"
	case *DeleteStmt:
		return "DELETE"
	case *MergeStmt:
		return "MERGE_INTO"
	case *CreateTableStmt, *CreateTableAsStmt, *CreateViewStmt, *CreateIndexStmt, *CreateSchemaStmt,
		*CreateSequenceStmt, *CreateTypeStmt, *CreateMacroStmt, *CreateSecretStmt:
		return "CREATE"
	case *AlterTableStmt, *AlterSequenceStmt, *AlterDatabaseStmt, *AlterObjectSchemaStmt, *RenameStmt, *CommentStmt:
		return "ALTER"
	case *DropStmt, *DropSecretStmt:
		return "DROP"
	case *CopyStmt:
		return "COPY"
	case *CopyDatabaseStmt:
		return "COPY_DATABASE"
	case *ExportStmt:
		return "EXPORT"
	case *ImportStmt:
		return "PRAGMA"
	case *AttachStmt:
		return "ATTACH"
	case *DetachStmt:
		return "DETACH"
	case *UseStmt, *SetStmt, *ResetStmt:
		return "SET"
	case *LoadStmt:
		return "LOAD"
	case *PragmaStmt:
		return "PRAGMA"
	case *ExplainStmt:
		return "EXPLAIN"
	case *PrepareStmt:
		return "PREPARE"
	case *ExecuteStmt:
		return "EXECUTE"
	case *DeallocateStmt:
		return "DROP"
	case *TransactionStmt:
		return "TRANSACTION"
	case *VacuumStmt, *AnalyzeStmt:
		return "VACUUM"
	case *CheckpointStmt, *CallStmt:
		return "CALL"
	case *UpdateExtensionsStmt:
		return "UPDATE_EXTENSIONS"
	default:
		_ = s
		return "INVALID"
	}
}
