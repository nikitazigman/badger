package sqlite

import (
	"fmt"
	"strings"

	gosqlx "github.com/ajitpratap0/GoSQLX/pkg/gosqlx"
	sqlast "github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
)

type SchemaDefinition struct {
	Fields []SchemaField
}

type SchemaField struct {
	Name         string
	DeclaredType string
	Expression   string
}

const sqliteSchemaTableSQL = "CREATE TABLE sqlite_schema(\n" +
	"  type text,\n" +
	"  name text,\n" +
	"  tbl_name text,\n" +
	"  rootpage integer,\n" +
	"  \"sql\" text\n" +
	");"

func ParseSchemaDefinitionSQL(sql string) (*SchemaDefinition, error) {
	tree, err := gosqlx.Parse(sql)
	if err != nil {
		return nil, err
	}
	if tree == nil || len(tree.Statements) != 1 {
		return nil, fmt.Errorf("expected exactly 1 SQL statement, got %d", statementCount(tree))
	}

	switch stmt := tree.Statements[0].(type) {
	case *sqlast.CreateTableStatement:
		fields := make([]SchemaField, 0, len(stmt.Columns))
		for _, column := range stmt.Columns {
			fields = append(fields, SchemaField{
				Name:         column.Name,
				DeclaredType: normalizeDeclaredType(column.Type),
			})
		}
		return &SchemaDefinition{
			Fields: fields,
		}, nil
	case *sqlast.CreateIndexStatement:
		fields := make([]SchemaField, 0, len(stmt.Columns))
		for _, column := range stmt.Columns {
			fields = append(fields, SchemaField{
				Name:       column.Column,
				Expression: column.Column,
			})
		}
		return &SchemaDefinition{
			Fields: fields,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported schema SQL statement type %T", stmt)
	}
}

func normalizeDeclaredType(raw string) string {
	return strings.TrimSpace(strings.ToLower(raw))
}

func statementCount(tree *sqlast.AST) int {
	if tree == nil {
		return 0
	}
	return len(tree.Statements)
}
