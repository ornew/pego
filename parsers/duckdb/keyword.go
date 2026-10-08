package duckdb

import "strings"

// A KeywordCategory is the category of a keyword, which decides where DuckDB accepts the word as a name.
type KeywordCategory int

// The categories of keywords, as duckdb_keywords() reports them.
const (
	// Unreserved keywords are names everywhere.
	Unreserved KeywordCategory = iota + 1
	// ColumnName keywords can be the names of columns and tables, but not of functions and types.
	ColumnName
	// TypeFunction keywords can be the names of functions or types, not of columns.
	TypeFunction
	// Reserved keywords can only be written as names between double quotes (or after AS, or after a dot).
	Reserved
)

func (c KeywordCategory) String() string {
	switch c {
	case Unreserved:
		return "unreserved"
	case ColumnName:
		return "column_name"
	case TypeFunction:
		return "type_function"
	case Reserved:
		return "reserved"
	}
	return "none"
}

// Keyword returns the category of word, if it is a keyword of DuckDB (without regard to its case).
func Keyword(word string) (KeywordCategory, bool) {
	c, ok := keywords[foldASCII(word)]
	return c, ok
}

// Keywords returns the keywords of DuckDB in lower case, with their categories.
func Keywords() map[string]KeywordCategory {
	m := make(map[string]KeywordCategory, len(keywords))
	for k, v := range keywords {
		m[k] = v
	}
	return m
}

// QuoteIdent returns name as it must be written to be read back as one identifier: unchanged if it is a plain
// word that is not a keyword, or an unreserved keyword; otherwise between double quotes, with the quotes in it
// doubled.
func QuoteIdent(name string) string {
	if needsQuotes(name) {
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	}
	return name
}

func needsQuotes(name string) bool {
	if name == "" {
		return true
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_':
		case c >= '0' && c <= '9' || c == '$':
			if i == 0 {
				return true
			}
		default:
			return true
		}
	}
	c, ok := keywords[foldASCII(name)]
	return ok && c != Unreserved
}
