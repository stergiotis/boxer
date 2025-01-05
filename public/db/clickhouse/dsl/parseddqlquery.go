package dsl

import (
	"fmt"
	chparser "github.com/AfterShip/clickhouse-sql-parser/parser"
	"github.com/fxamacker/cbor/v2"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"slices"
	"strings"
)

type ParsedDqlQuery struct {
	inputSql string
	ast      *chparser.SelectQuery

	paramSet    *ParamSlotSet
	paramSetErr error
	noParams    bool
}

func (inst *ParsedDqlQuery) String() string {
	return inst.ast.String()
}
func (inst *ParsedDqlQuery) GetAst() *chparser.SelectQuery {
	return inst.ast
}
func (inst *ParsedDqlQuery) GetParamSlotSet() (paramSet *ParamSlotSet, err error) {
	if inst.noParams {
		return
	}
	if inst.paramSet == nil && inst.paramSetErr == nil {
		ps := NewParamSlotsSet()
		d := newParamSlotsDiscoverer()
		err = d.discover(inst.ast, ps)
		if err != nil {
			err = eh.Errorf("error while discovering paramset: %w", err)
			inst.paramSetErr = err
			return
		}
		if ps.IsEmpty() {
			inst.noParams = true
			ps = nil
		}
		inst.paramSet = ps
	}
	paramSet = inst.paramSet
	err = inst.paramSetErr
	return
}

func NewParsedDqlQuery(sql string) (inst *ParsedDqlQuery, err error) {
	inst = &ParsedDqlQuery{
		inputSql: sql,
		ast:      nil,
	}
	err = inst.parse()
	if err != nil {
		err = eh.Errorf("unable to parse sql: %w", err)
		return
	}
	return
}
func removeParamSettingsFromExprs(exprs []chparser.Expr) (exprsOut []chparser.Expr) {
	for _, expr := range exprs {
		switch exprt := expr.(type) {
		case *chparser.SetStmt:
			items := slices.DeleteFunc(exprt.Settings.Items, func(list *chparser.SettingExprList) bool {
				name := list.Name.Name
				if strings.HasPrefix(list.Name.Name, "param_") {
					log.Info().Str("name", name).Msg("removing set param value expression")
					return true
				}
				return false
			})
			exprt.Settings.Items = items
			break
		}
	}
	exprsOut = slices.DeleteFunc(exprs, func(expr chparser.Expr) bool {
		switch exprt := expr.(type) {
		case *chparser.SetStmt:
			return len(exprt.Settings.Items) == 0
		}
		return false
	})
	return
}
func (inst *ParsedDqlQuery) parse() (err error) {
	p := chparser.NewParser(inst.inputSql)
	var exprs []chparser.Expr
	exprs, err = p.ParseStmts()
	if err != nil {
		err = eh.Errorf("unable to parse sql: %w", err)
		return
	}

	exprs = removeParamSettingsFromExprs(exprs)
	if len(exprs) != 1 {
		err = eb.Build().Int("nExprs", len(exprs)).Errorf("sql must contain exactly on expression")
		return
	}
	q, ok := exprs[0].(*chparser.SelectQuery)
	if !ok {
		err = eb.Build().Type("expr", exprs[0]).Errorf("supplied query is not a data query language expression")
		return
	}
	inst.ast = q
	return
}
func (inst *ParsedDqlQuery) DeepCopy() (other *ParsedDqlQuery, err error) {
	var b []byte
	b, err = cbor.Marshal(inst.ast)
	if err != nil {
		err = eh.Errorf("unable to marshall ast: %w", err)
		return
	}
	var astAny *chparser.SelectQuery
	err = cbor.Unmarshal(b, &astAny)
	if err != nil {
		err = eh.Errorf("unable to unmarshall ast: %w", err)
		return
	}
	return
}

var _ fmt.Stringer = (*ParsedDqlQuery)(nil)
