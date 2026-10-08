package duckdb_test

import (
	"strings"

	"github.com/ornew/pego/parsers/duckdb"
)

// typ maps a type to the JSON of a LogicalType in the AST of DuckDB.
func (m *mapper) typ(t *duckdb.Type) shape {
	if len(t.Dims) > 0 {
		inner := *t
		inner.Dims = nil
		child := m.typ(&inner)
		// the first dimension is the innermost: int[3][] is a list of arrays of 3
		for _, d := range t.Dims {
			if d.Size != nil {
				n, _ := d.Size.Int64()
				child = shape{"id": "ARRAY", "type_info": shape{"type": "ARRAY_TYPE_INFO", "child_type": child, "size": n}}
			} else {
				child = shape{"id": "LIST", "type_info": shape{"type": "LIST_TYPE_INFO", "child_type": child}}
			}
		}
		return child
	}
	fields := strings.Fields(lower(t.Name))
	name := strings.Join(fields, " ")
	zone := lower(strings.Join(strings.Fields(t.Zone), " "))
	if fields[0] == "interval" && len(t.Mods) == 0 {
		return shape{"id": "INTERVAL"}
	}
	switch name {
	case "struct", "row", "union":
		var cts []any
		if name == "union" {
			cts = append(cts, shape{"first": "", "second": shape{"id": "UTINYINT"}})
		}
		for i := range t.Fields {
			cts = append(cts, shape{"first": t.Fields[i].Name.Name(), "second": m.typ(t.Fields[i].Type)})
		}
		id := "STRUCT"
		if name == "union" {
			id = "UNION"
		}
		return shape{"id": id, "type_info": shape{"type": "STRUCT_TYPE_INFO", "child_types": cts}}
	case "map":
		if len(t.Elems) != 2 {
			unsup("map type")
		}
		kv := shape{"id": "STRUCT", "type_info": shape{"type": "STRUCT_TYPE_INFO", "child_types": []any{
			shape{"first": "key", "second": m.typ(t.Elems[0])}, shape{"first": "value", "second": m.typ(t.Elems[1])}}}}
		return shape{"id": "MAP", "type_info": shape{"type": "LIST_TYPE_INFO", "child_type": kv}}
	case "timestamp", "datetime", "timestamptz", "time", "timetz":
		base := fields[0]
		tz := strings.HasPrefix(zone, "with ") || base == "timestamptz" || base == "timetz"
		if len(t.Mods) > 0 {
			if tz || base == "time" || base == "timetz" {
				unsup("time type modifier")
			}
			switch m.intMod(t.Mods[0]) {
			case 0:
				return shape{"id": "TIMESTAMP_S"}
			case 3:
				return shape{"id": "TIMESTAMP_MS"}
			case 6:
				return shape{"id": "TIMESTAMP"}
			case 9:
				return shape{"id": "TIMESTAMP_NS"}
			}
			unsup("timestamp precision")
		}
		switch {
		case base == "time" || base == "timetz":
			if tz {
				return shape{"id": "TIME WITH TIME ZONE"}
			}
			return shape{"id": "TIME"}
		case tz:
			return shape{"id": "TIMESTAMP WITH TIME ZONE"}
		}
		return shape{"id": "TIMESTAMP"}
	case "interval":
		return shape{"id": "INTERVAL"}
	case "bit varying":
		if len(t.Mods) > 0 {
			unsup("bit varying modifier")
		}
		return unbound("", "", "varbit")
	case "float":
		if len(t.Mods) == 1 {
			n := m.intMod(t.Mods[0])
			if n >= 25 && n <= 53 {
				return shape{"id": "DOUBLE"}
			}
			if n < 1 || n > 53 {
				unsup("float precision")
			}
		}
		return shape{"id": "FLOAT"}
	case "decimal", "numeric", "dec":
		w, s := 18, 3
		if len(t.Mods) >= 1 {
			w = m.intMod(t.Mods[0])
			s = 0
		}
		if len(t.Mods) >= 2 {
			s = m.intMod(t.Mods[1])
		}
		if len(t.Mods) > 2 || w < 1 || w > 38 || s > w {
			unsup("decimal type")
		}
		return shape{"id": "DECIMAL", "type_info": shape{"type": "DECIMAL_TYPE_INFO", "width": w, "scale": s}}
	}
	var id string
	switch name {
	case "int", "integer", "int4", "signed", "int32":
		id = "INTEGER"
	case "bigint", "int8", "long", "oid", "int64":
		id = "BIGINT"
	case "smallint", "int2", "short", "int16":
		id = "SMALLINT"
	case "tinyint", "int1":
		id = "TINYINT"
	case "hugeint", "int128":
		id = "HUGEINT"
	case "utinyint", "uint8":
		id = "UTINYINT"
	case "usmallint", "uint16":
		id = "USMALLINT"
	case "uinteger", "uint32":
		id = "UINTEGER"
	case "ubigint", "uint64":
		id = "UBIGINT"
	case "uhugeint", "uint128":
		id = "UHUGEINT"
	case "real", "float4":
		id = "FLOAT"
	case "double precision", "double", "float8":
		id = "DOUBLE"
	case "boolean", "bool", "logical":
		id = "BOOLEAN"
	case "varchar", "text", "string", "char", "character", "bpchar", "nchar", "character varying", "char varying", "nchar varying",
		"national character", "national char", "national character varying", "national char varying":
		id = "VARCHAR"
	case "date":
		id = "DATE"
	case "timestamp_us":
		id = "TIMESTAMP"
	case "time_ns":
		id = "TIME_NS"
	case "timestamp_s":
		id = "TIMESTAMP_S"
	case "timestamp_ms":
		id = "TIMESTAMP_MS"
	case "timestamp_ns":
		id = "TIMESTAMP_NS"
	case "blob", "bytea", "binary", "varbinary":
		id = "BLOB"
	case "uuid":
		id = "UUID"
	case "bit", "bitstring":
		id = "BIT"
	case "bignum", "varint":
		id = "BIGNUM"
	}
	if id != "" {
		if len(t.Mods) > 0 && id != "VARCHAR" && id != "BIT" {
			unsup("type modifier of %s", id)
		}
		return shape{"id": id}
	}
	if len(t.Mods) > 0 || len(t.Fields) > 0 || len(t.Elems) > 0 {
		unsup("type %s with modifiers", t.Name)
	}
	switch name {
	case "geometry", "variant", "list", "enum", "null":
		unsup("type %s", t.Name)
	}
	// a type that DuckDB resolves later: its name as written, with the schema and the catalog
	var parts []string
	for _, p := range strings.Split(t.Name, ".") {
		p = strings.TrimSpace(p)
		if strings.ContainsAny(p, `"'`) {
			if len(p) < 2 || p[0] != '"' || p[len(p)-1] != '"' || strings.Contains(p[1:len(p)-1], `"`) {
				unsup("quoted type name")
			}
			p = p[1 : len(p)-1]
		}
		parts = append(parts, p)
	}
	if len(parts) == 1 && strings.EqualFold(parts[0], "null") {
		unsup("type NULL")
	}
	switch len(parts) {
	case 1:
		return unbound("", "", parts[0])
	case 2:
		return unbound("", parts[0], parts[1])
	case 3:
		return unbound(parts[0], parts[1], parts[2])
	}
	unsup("type name")
	return nil
}

func unbound(catalog, schema, name string) shape {
	return shape{"id": "UNBOUND", "type_info": shape{"type": "UNBOUND_TYPE_INFO", "catalog": catalog, "schema": schema, "name": name}}
}

func (m *mapper) intMod(e duckdb.Expr) int {
	if n, ok := e.(*duckdb.Number); ok {
		v, ok := n.Int64()
		if ok {
			return int(v)
		}
	}
	unsup("type modifier")
	return 0
}
