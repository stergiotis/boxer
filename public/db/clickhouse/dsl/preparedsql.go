package dsl

import (
	"fmt"
	chparser "github.com/AfterShip/clickhouse-sql-parser/parser"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

type PreparedSql struct {
	inputSql string
	ast      chparser.Expr
}

func (inst *PreparedSql) String() string {
	return inst.ast.String()
}

func NewPreparedSql(sql string) (inst *PreparedSql, err error) {
	inst = &PreparedSql{
		inputSql: sql,
		ast:      nil,
	}
	err = inst.prepare()
	if err != nil {
		err = eh.Errorf("unable to prepare sql: %w", err)
		return
	}
	return
}
func (inst *PreparedSql) prepare() (err error) {
	p := chparser.NewParser(inst.inputSql)
	var exprs []chparser.Expr
	exprs, err = p.ParseStmts()
	if err != nil {
		err = eh.Errorf("unable to parse sql: %w", err)
		return
	}
	if len(exprs) != 1 {
		err = eb.Build().Int("nExprs", len(exprs)).Errorf("sql must contain exactly on expression")
		return
	}
	inst.ast = exprs[0]
	return
}

var _ fmt.Stringer = (*PreparedSql)(nil)
