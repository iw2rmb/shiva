package schema

import _ "embed"

//go:embed 000001_initial.sql
var initialSchemaSQL string

func InitialSchemaSQL() string {
	return initialSchemaSQL
}
