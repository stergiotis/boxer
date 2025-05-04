package dsl

import (
	"io"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/ast"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/ea"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

func parseSql(sql string, errL antlr.ErrorListener, errStrategy antlr.ErrorStrategy) (parser *grammar.ClickHouseParser, parseTree *grammar.QueryStmtContext, err error) {
	inputStream := antlr.NewInputStream(sql)
	if errL == nil {
		errL = NewStoringErrListener(32, 16, 16, 16)
	}
	lexer := grammar.NewClickHouseLexer(inputStream)
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(errL)
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser = grammar.NewClickHouseParser(stream)
	parser.RemoveErrorListeners()
	parser.AddErrorListener(errL)
	if errStrategy != nil {
		parser.SetErrorHandler(errStrategy)
	}

	parseTreeI := parser.QueryStmt()

	var ok bool
	parseTree, ok = parseTreeI.(*grammar.QueryStmtContext)
	if !ok {
		err = eb.Build().Type("parseTreeI", parseTreeI).Errorf("unable to cast to QueryStmtContext")
		return
	}
	parseError := parser.GetError()
	if parseError != nil {
		var s SyntaxErrorSynthesizerI
		s, ok = errL.(SyntaxErrorSynthesizerI)
		if ok {
			err = s.GetSynthSyntaxError(128, true)
			return
		} else {
			token := parseError.GetOffendingToken()
			err = eb.Build().Str("message", parseError.GetMessage()).
				Int("start", token.GetStart()).
				Int("stop", token.GetStop()).
				Int("line", token.GetLine()).
				Int("column", token.GetColumn()).
				Type("tokenType", token).
				Str("message", parseError.GetMessage()).
				Errorf("syntax errors detected")
		}
		return
	}
	return
}

type ParsedDqlQuery struct {
	paramSlotSetErr    error
	parseTree          *grammar.QueryStmtContext
	parser             *grammar.ClickHouseParser
	errL               *StoringErrListener
	errS               antlr.ErrorStrategy
	recoverParseErrors bool

	paramBindEnv *ParamBindEnv
	paramSlotSet *ParamSlotSet
	paramExprs   []*grammar.SettingExprContext

	inputSql string
	noParams bool
}

func (inst *ParsedDqlQuery) GetErrorListener() *StoringErrListener {
	return inst.errL
}
func (inst *ParsedDqlQuery) SetRecoverFromParseErrors(recover bool) {
	inst.recoverParseErrors = recover
	//	FIXME: BailErrorStrategy is not properly implemented (FIXME panic)
	//if recover {
	//	inst.errS = antlr.NewDefaultErrorStrategy()
	//} else {
	//	inst.errS = antlr.NewBailErrorStrategy()
	//}
}

func (inst *ParsedDqlQuery) GetParser() *grammar.ClickHouseParser {
	return inst.parser
}
func (inst *ParsedDqlQuery) GetInputSql() (sql string) {
	sql = inst.inputSql
	return
}
func (inst *ParsedDqlQuery) GetInputParseTree() *grammar.QueryStmtContext {
	return inst.parseTree
}
func (inst *ParsedDqlQuery) GetParamBindEnv() (paramBindEnv *ParamBindEnv) {
	if inst.paramBindEnv.IsEmpty() {
		return nil
	}
	return inst.paramBindEnv
}
func (inst *ParsedDqlQuery) InputSqlSelect() (sql string) {
	log.Panic().Msg("not implemented")
	return
}
func (inst *ParsedDqlQuery) InputSqlBindEnv() (sql string) {
	log.Panic().Msg("not implemented")
	return
}
func (inst *ParsedDqlQuery) GetParamSlotSet() (paramSet *ParamSlotSet, err error) {
	if inst.noParams {
		return
	}
	if inst.paramSlotSet == nil && inst.paramSlotSetErr == nil {
		ps := NewParamSlotsSet()
		err = ps.AddSlotsFromParseTree(inst.parseTree)
		if err != nil {
			err = eh.Errorf("error while discovering paramset: %w", err)
			inst.paramSlotSetErr = err
			return
		}
		if ps.IsEmpty() {
			inst.noParams = true
			ps = nil
		}
		inst.paramSlotSet = ps
	}
	paramSet = inst.paramSlotSet
	err = inst.paramSlotSetErr
	return
}

func NewParsedDqlQuery() (inst *ParsedDqlQuery) {
	inst = &ParsedDqlQuery{
		paramSlotSetErr: nil,
		parseTree:       nil,
		errL:            NewStoringErrListener(32, 16, 16, 16),
		errS:            antlr.NewDefaultErrorStrategy(),
		paramBindEnv:    NewParamBindEnv(),
		paramSlotSet:    NewParamSlotsSet(),
		paramExprs:      make([]*grammar.SettingExprContext, 0, 64),
		inputSql:        "",
		noParams:        false,
	}
	return
}
func (inst *ParsedDqlQuery) identifyParamBindEnvs() (err error) {
	const paramPrefixName = "param_" // Note: param names are case-sensitive
	var nonParam bool
	clear(inst.paramExprs)
	paramExprs := inst.paramExprs[:0]
	for node := range IterateAllByType[*grammar.SettingExprContext](inst.parseTree) {
		id := ast.Identifier{}
		id.LoadContext(node.Identifier().(*grammar.IdentifierContext))
		if strings.HasPrefix(id.Name, paramPrefixName) {
			{ // TODO lift this limitations
				if nonParam {
					err = eh.Errorf("param settings must be first settings and not mixed with regular settings")
					return
				}
				ok := false
				switch pt := node.GetParent().(type) {
				case *grammar.SettingExprListContext:
					switch pt.GetParent().(type) {
					case *grammar.SetStmtContext:
						ok = true
						break
					}
					break
				}
				if !ok {
					err = eb.Build().Type("parent", node.GetParent()).Errorf("param settings must be defined using SET ... statement, not SETTINGS ... clause")
					return
				}
			}
			paramExprs = append(paramExprs, node)
		} else {
			nonParam = true
		}
	}
	inst.paramExprs = paramExprs
	return
}
func (inst *ParsedDqlQuery) populateBindEnv() (err error) {
	bindEnv := inst.paramBindEnv
	bindEnv.Clear()
	bindEnv.inputSql = inst.inputSql
	err = inst.identifyParamBindEnvs()
	if err != nil {
		return
	}
	for _, ex := range inst.paramExprs {
		err = bindEnv.AddDistinct(ex)
		if err != nil {
			err = eh.Errorf("unable to add param expression to binding environment: %w", err)
			return
		}
	}

	return
}
func (inst *ParsedDqlQuery) ParseFromReader(sql io.Reader) (err error) {
	var s string
	s, err = ea.ReadAllString(sql)
	if err != nil {
		inst.Reset()
		err = eh.Errorf("unable to read sql from reader: %w", err)
		return
	}
	return inst.ParseFromString(s)
}
func (inst *ParsedDqlQuery) ParseFromString(sql string) (err error) {
	inst.Reset()
	var parseTree *grammar.QueryStmtContext

	errL := inst.errL
	errL.Reset()
	var parser *grammar.ClickHouseParser
	parser, parseTree, err = parseSql(sql, errL, inst.errS)
	if err == nil && !inst.recoverParseErrors {
		err = errL.GetSynthSyntaxError(32, false)
	}
	inst.inputSql = sql
	inst.parseTree = parseTree
	inst.parser = parser
	if err != nil {
		inst.paramBindEnv.Clear()
		err = eh.Errorf("unable to parse sql as dql query: %w", err)
		return
	}
	return inst.populateBindEnv()
}
func (inst *ParsedDqlQuery) Reset() {
	inst.noParams = false
	inst.parseTree = nil
	inst.paramSlotSet.Clear()
	inst.paramSlotSetErr = nil
	inst.paramBindEnv.Clear()
	inst.inputSql = ""
	clear(inst.paramExprs)
	inst.paramExprs = inst.paramExprs[:0]
}
