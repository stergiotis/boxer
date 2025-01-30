//go:build disabled

package dsl

type TransfomerI interface {
	Apply(ast chparser.Expr) (err error)
}
type TableIdTransformerPluginI interface {
	Name() string
	Transform(db string, table string) (replacement *ParsedDqlQuery, isStaticReplacement bool, applicable bool, err error)
}
