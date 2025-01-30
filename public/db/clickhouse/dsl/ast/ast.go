package ast

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"strings"
)

type Identifier struct {
	Name        string
	Backtick    bool
	DoubleQuote bool
	ParseNode   *grammar.IdentifierContext
}

func (inst *Identifier) LoadContext(ctx *grammar.IdentifierContext) {
	inst.Name = ""
	inst.Backtick = false
	inst.DoubleQuote = false
	inst.ParseNode = ctx
	if ctx == nil {
		return
	}
	raw := ctx.IDENTIFIER().GetText()
	if raw == "" {
		return
	}
	backtick := raw[0] == '`'
	doubleQuote := raw[0] == '"'
	name := raw
	if backtick {
		name = strings.ReplaceAll(raw[1:len(raw)-1], "\\`", "'")
	} else if doubleQuote {
		name = strings.ReplaceAll(raw[1:len(raw)-1], "\\\"", "\"")
	}
	inst.Name = name
	inst.Backtick = backtick
	inst.DoubleQuote = doubleQuote
	inst.ParseNode = ctx
}

type ColumnType struct {
	Sql       string
	ParseNode *grammar.ColumnTypeExprContext
}

func (inst *ColumnType) LoadContext(ctx *grammar.ColumnTypeExprContext) {
	inst.Sql = ctx.GetText()
	inst.ParseNode = ctx
}
func (inst *ColumnType) IsCompatible(other *ColumnType) (compatible bool) {
	// FIXME do more work
	return inst.Sql == other.Sql
}
