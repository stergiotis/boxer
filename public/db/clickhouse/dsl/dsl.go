package dsl

import (
	"fmt"
	chparser "github.com/AfterShip/clickhouse-sql-parser/parser"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"strings"
)

type Dsl struct {
	tableIdTransformer TransfomerI
	Exprs              []chparser.Expr
}

func NewDsl(tableIdTransformer TransfomerI) (inst *Dsl, err error) {
	inst = &Dsl{
		tableIdTransformer: tableIdTransformer,
		Exprs:              nil,
	}
	return
}
func (inst *Dsl) Parse(sql string) (err error) {
	p := chparser.NewParser(sql)
	inst.Exprs, err = p.ParseStmts()
	if err != nil {
		err = eh.Errorf("unable to parse sql: %w", err)
		return
	}
	return
}
func (inst *Dsl) LoadDql(dql *ParsedDqlQuery) (err error) {
	inst.Exprs = []chparser.Expr{dql.GetAst()}
	return
}
func (inst *Dsl) Transform() (err error) {
	err = inst.checkParsed()
	if err != nil {
		return
	}
	if inst.tableIdTransformer != nil {
		tr := inst.tableIdTransformer
		for i, expr := range inst.Exprs {
			err = tr.Apply(expr)
			if err != nil {
				err = eb.Build().Int("exprIndex", i).Errorf("unable to apply ast visitor: %w", err)
				return
			}
		}
	}

	return
}

var ErrNoParsedAsAvailable = eh.Errorf("no parsed AST available")

func (inst *Dsl) checkParsed() (err error) {
	if len(inst.Exprs) == 0 {
		return ErrNoParsedAsAvailable
	}
	return
}

/*
	func (inst *Dsl) FromExprs() (err error) {
		err = inst.checkParsed()
		if err != nil {
			return
		}
		return
	}
*/
func (inst *Dsl) Apply(visitor chparser.ASTVisitor) (err error) {
	err = inst.checkParsed()
	if err != nil {
		return
	}
	for i, expr := range inst.Exprs {
		err = expr.Accept(visitor)
		if err != nil {
			err = eb.Build().Int("exprIndex", i).Errorf("unable to apply ast visitor: %w", err)
			return
		}
	}
	return
}
func (inst *Dsl) String() string {
	if len(inst.Exprs) == 0 {
		return ""
	}
	b := strings.Builder{}
	for _, s := range inst.Exprs {
		b.WriteString(s.String())
		b.WriteString(";\n")
	}
	return b.String()
}

var _ fmt.Stringer = (*Dsl)(nil)
