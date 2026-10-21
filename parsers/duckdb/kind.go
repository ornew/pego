package duckdb

import "github.com/ornew/pego/parsers/duckdb/types"

// Kind returns the type DuckDB gives the statement (the name of its StatementType: SELECT, INSERT, CREATE, ...).
//
// DuckDB rewrites some statements while it parses them, so a statement can have another type in DuckDB than its
// syntax says: PRAGMA is a SELECT when it calls a function or a SET when it assigns a value, SHOW, DESCRIBE and
// SUMMARIZE are SELECT statements, CHECKPOINT is a CALL, USE and RESET are SET statements, ANALYZE a VACUUM, and
// COMMENT ON an ALTER. Kind of a PragmaStmt is "PRAGMA".
func Kind(s types.Statement) string {
	switch s := s.(type) {
	case *types.Select, *types.ShowStmt:
		return "SELECT"
	case *types.InsertStmt:
		return "INSERT"
	case *types.UpdateStmt:
		return "UPDATE"
	case *types.DeleteStmt:
		return "DELETE"
	case *types.MergeStmt:
		return "MERGE_INTO"
	case *types.CreateTableStmt, *types.CreateTableAsStmt, *types.CreateViewStmt, *types.CreateIndexStmt, *types.CreateSchemaStmt,
		*types.CreateSequenceStmt, *types.CreateTypeStmt, *types.CreateMacroStmt, *types.CreateSecretStmt:
		return "CREATE"
	case *types.AlterTableStmt, *types.AlterSequenceStmt, *types.AlterDatabaseStmt, *types.AlterObjectSchemaStmt, *types.RenameStmt, *types.CommentStmt:
		return "ALTER"
	case *types.DropStmt, *types.DropSecretStmt:
		return "DROP"
	case *types.CopyStmt:
		return "COPY"
	case *types.CopyDatabaseStmt:
		return "COPY_DATABASE"
	case *types.ExportStmt:
		return "EXPORT"
	case *types.ImportStmt:
		return "PRAGMA"
	case *types.AttachStmt:
		return "ATTACH"
	case *types.DetachStmt:
		return "DETACH"
	case *types.UseStmt, *types.SetStmt, *types.ResetStmt:
		return "SET"
	case *types.LoadStmt:
		return "LOAD"
	case *types.PragmaStmt:
		return "PRAGMA"
	case *types.ExplainStmt:
		return "EXPLAIN"
	case *types.PrepareStmt:
		return "PREPARE"
	case *types.ExecuteStmt:
		return "EXECUTE"
	case *types.DeallocateStmt:
		return "DROP"
	case *types.TransactionStmt:
		return "TRANSACTION"
	case *types.VacuumStmt, *types.AnalyzeStmt:
		return "VACUUM"
	case *types.CheckpointStmt, *types.CallStmt:
		return "CALL"
	case *types.UpdateExtensionsStmt:
		return "UPDATE_EXTENSIONS"
	default:
		_ = s
		return "INVALID"
	}
}
