package dsl

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/ast"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/ea"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"io"
	"strings"
)

type errListener struct {
}

func (inst *errListener) SyntaxError(recognizer antlr.Recognizer, offendingSymbol interface{}, line, column int, msg string, e antlr.RecognitionException) {
	log.Debug().Interface("offendingSymbol", offendingSymbol).Int("line", line).Int("col", column).Str("msg", msg).Msg("syntax error")
	recognizer.SetError(e)
}

func (inst *errListener) ReportAmbiguity(recognizer antlr.Parser, dfa *antlr.DFA, startIndex, stopIndex int, exact bool, ambigAlts *antlr.BitSet, configs *antlr.ATNConfigSet) {
	log.Debug().Int("startIndex", startIndex).Int("stopIndex", stopIndex).Bool("exact", exact).Msg("ambiguity detected")
}

func (inst *errListener) ReportAttemptingFullContext(recognizer antlr.Parser, dfa *antlr.DFA, startIndex, stopIndex int, conflictingAlts *antlr.BitSet, configs *antlr.ATNConfigSet) {
	log.Debug().Int("startIndex", startIndex).Int("stopIndex", stopIndex).Msg("conflicting ambiguity detected")
}

func (inst *errListener) ReportContextSensitivity(recognizer antlr.Parser, dfa *antlr.DFA, startIndex, stopIndex, prediction int, configs *antlr.ATNConfigSet) {
	log.Debug().Int("startIndex", startIndex).Int("stopIndex", stopIndex).Msg("context sensitivity")
}

var _ antlr.ErrorListener = (*errListener)(nil)

func parseSql(sql string, errL antlr.ErrorListener) (parser *grammar.ClickHouseParser, parseTree *grammar.QueryStmtContext, err error) {
	inputStream := antlr.NewInputStream(sql)
	if errL == nil {
		errL = &errListener{}
	}
	lexer := grammar.NewClickHouseLexer(inputStream)
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(errL)
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser = grammar.NewClickHouseParser(stream)
	parser.RemoveErrorListeners()
	parser.AddErrorListener(errL)

	parseTreeI := parser.QueryStmt()
	var ok bool
	parseTree, ok = parseTreeI.(*grammar.QueryStmtContext)
	if !ok {
		err = eb.Build().Type("parseTreeI", parseTreeI).Errorf("unable to cast to QueryStmtContext")
		return
	}

	if parser.HasError() {
		pe := parser.GetError()
		err = eh.Errorf("errors detected: %s (token=%s)", pe.GetMessage(), pe.GetOffendingToken().GetText())
		return
	}
	return
}

type ParsedDqlQuery struct {
	paramSlotSetErr error
	parseTree       *grammar.QueryStmtContext

	paramBindEnv *ParamBindEnv
	paramSlotSet *ParamSlotSet
	paramExprs   []*grammar.SettingExprContext

	inputSql string
	noParams bool
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
	_, parseTree, err = parseSql(sql, nil)
	if err != nil {
		err = eh.Errorf("unable to parse sql as dql query: %w", err)
		return
	}
	inst.inputSql = sql
	inst.parseTree = parseTree
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
