package dsl

import chparser "github.com/AfterShip/clickhouse-sql-parser/parser"

type TransfomerI interface {
	Apply(ast chparser.Expr) (err error)
}
type TableIdTransformerPluginI interface {
	Name() string
	Transform(db string, table string) (replacement *PreparedSql, isStaticReplacement bool, applicable bool, err error)
}
