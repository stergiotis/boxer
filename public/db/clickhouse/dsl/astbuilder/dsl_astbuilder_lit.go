package astbuilder

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/ast"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/observability/eh"
)

var marshallingOptions = marshalling.MarshalOptions{
	PreserveCasts:            false,
	MapCanonicalToClickHouse: marshalling.MapCanonicalToClickHouseTypeStr,
}
var marshallingOptionsCast = marshalling.MarshalOptions{
	PreserveCasts:            true,
	MapCanonicalToClickHouse: marshalling.MapCanonicalToClickHouseTypeStr,
}

func preprocessLiteralValue(v any) any {
	switch vt := v.(type) {
	case int:
		v = int64(vt)
	case uint:
		v = uint64(vt)
	}
	return v
}

// Lit creates a literal expression from a Go value. Uses marshalling.MarshalGoValueToSQL.
// If the value type is unsupported, the error is deferred to Build().
func Lit(v interface{}) E {
	v = preprocessLiteralValue(v)
	sql, err := marshalling.MarshalGoValueToSQLWithOptions(v, marshallingOptions)
	if err != nil {
		return errE(eh.Errorf("Lit: %w", err))
	}
	return E{Expr: ast.Expr{Kind: ast.KindLiteral, Literal: &ast.LiteralData{SQL: sql}}}
}
func LitCast(v interface{}) E {
	v = preprocessLiteralValue(v)
	sql, typeName, err := marshalling.MarshalGoValueToSQLWithOptionsCast(v, marshallingOptionsCast)
	if err != nil {
		return errE(eh.Errorf("LitCast: %w", err))
	}
	return E{Expr: ast.Expr{Kind: ast.KindFunctionCall, Func: &ast.FuncCallData{
		Name: "CAST",
		Args: []ast.Expr{
			{Kind: ast.KindLiteral, Literal: &ast.LiteralData{SQL: sql}},
			{Kind: ast.KindLiteral, Literal: &ast.LiteralData{SQL: "'" + typeName + "'"}},
		},
	}}}
}
