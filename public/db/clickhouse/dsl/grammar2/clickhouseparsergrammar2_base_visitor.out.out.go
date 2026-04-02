// Code generated from ClickHouseParserGrammar2.g4 by ANTLR 4.13.2. DO NOT EDIT.

package grammar2 // ClickHouseParserGrammar2
import "github.com/antlr4-go/antlr/v4"

type BaseClickHouseParserGrammar2Visitor struct {
	*antlr.BaseParseTreeVisitor
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitQueryStmt(ctx *QueryStmtContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitQuery(ctx *QueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitCtes(ctx *CtesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitNamedQuery(ctx *NamedQueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnAliases(ctx *ColumnAliasesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitSelectUnionStmt(ctx *SelectUnionStmtContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitSelectUnionStmtItem(ctx *SelectUnionStmtItemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitSelectStmtWithParens(ctx *SelectStmtWithParensContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitSelectStmt(ctx *SelectStmtContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitProjectionClause(ctx *ProjectionClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitProjectionExceptClause(ctx *ProjectionExceptClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitStaticColumnList(ctx *StaticColumnListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitDynamicColumnList(ctx *DynamicColumnListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitDynamicColumnSelection(ctx *DynamicColumnSelectionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitWithClause(ctx *WithClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitTopClause(ctx *TopClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitFromClause(ctx *FromClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitArrayJoinClause(ctx *ArrayJoinClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitWindowClause(ctx *WindowClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitQualifyClause(ctx *QualifyClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitPrewhereClause(ctx *PrewhereClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitWhereClause(ctx *WhereClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitGroupByClause(ctx *GroupByClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitHavingClause(ctx *HavingClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitOrderByClause(ctx *OrderByClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitLimitByClause(ctx *LimitByClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitLimitClause(ctx *LimitClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitSettingsClause(ctx *SettingsClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitJoinExprOp(ctx *JoinExprOpContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitJoinExprTable(ctx *JoinExprTableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitJoinExprParens(ctx *JoinExprParensContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitJoinExprCrossOp(ctx *JoinExprCrossOpContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitJoinOpInner(ctx *JoinOpInnerContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitJoinOpLeftRight(ctx *JoinOpLeftRightContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitJoinOpFull(ctx *JoinOpFullContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitJoinOpCross(ctx *JoinOpCrossContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitJoinConstraintClause(ctx *JoinConstraintClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitSampleClause(ctx *SampleClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitLimitExpr(ctx *LimitExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitOrderExprList(ctx *OrderExprListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitOrderExpr(ctx *OrderExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitRatioExpr(ctx *RatioExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitSettingExprList(ctx *SettingExprListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitSettingExpr(ctx *SettingExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitSettingLiteral(ctx *SettingLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitSettingEmptyArray(ctx *SettingEmptyArrayContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitSettingArray(ctx *SettingArrayContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitSettingTuple(ctx *SettingTupleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitSettingFunctionEmpty(ctx *SettingFunctionEmptyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitSettingFunction(ctx *SettingFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitWindowExpr(ctx *WindowExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitWinPartitionByClause(ctx *WinPartitionByClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitWinOrderByClause(ctx *WinOrderByClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitWinFrameClause(ctx *WinFrameClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitFrameStart(ctx *FrameStartContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitFrameBetween(ctx *FrameBetweenContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitWinFrameBound(ctx *WinFrameBoundContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitSetStmt(ctx *SetStmtContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnTypeExprSimple(ctx *ColumnTypeExprSimpleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnTypeExprNested(ctx *ColumnTypeExprNestedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnTypeExprEnum(ctx *ColumnTypeExprEnumContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnTypeExprComplex(ctx *ColumnTypeExprComplexContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnTypeExprParam(ctx *ColumnTypeExprParamContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnExprList(ctx *ColumnExprListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnsExprAsterisk(ctx *ColumnsExprAsteriskContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnsExprSubquery(ctx *ColumnsExprSubqueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnsExprColumn(ctx *ColumnsExprColumnContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnExprAlias(ctx *ColumnExprAliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnExprNegate(ctx *ColumnExprNegateContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnExprSubquery(ctx *ColumnExprSubqueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnExprLiteral(ctx *ColumnExprLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnExprParamSlot(ctx *ColumnExprParamSlotContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnExprBetween(ctx *ColumnExprBetweenContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnExprOr(ctx *ColumnExprOrContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnExprParens(ctx *ColumnExprParensContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnExprDynamic(ctx *ColumnExprDynamicContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnExprPrecedence1(ctx *ColumnExprPrecedence1Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnExprPrecedence2(ctx *ColumnExprPrecedence2Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnExprPrecedence3(ctx *ColumnExprPrecedence3Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnExprAnd(ctx *ColumnExprAndContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnExprInterval(ctx *ColumnExprIntervalContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnExprNot(ctx *ColumnExprNotContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnExprIsNull(ctx *ColumnExprIsNullContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnExprWinFunction(ctx *ColumnExprWinFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnExprIdentifier(ctx *ColumnExprIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnExprWinFunctionTarget(ctx *ColumnExprWinFunctionTargetContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnExprFunction(ctx *ColumnExprFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnExprAsterisk(ctx *ColumnExprAsteriskContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnArgList(ctx *ColumnArgListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnArgExpr(ctx *ColumnArgExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnLambdaExpr(ctx *ColumnLambdaExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitColumnIdentifier(ctx *ColumnIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitNestedIdentifier(ctx *NestedIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitTableExprIdentifier(ctx *TableExprIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitTableExprSubquery(ctx *TableExprSubqueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitTableExprAlias(ctx *TableExprAliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitTableExprFunction(ctx *TableExprFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitTableFunctionExpr(ctx *TableFunctionExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitTableIdentifier(ctx *TableIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitTableArgList(ctx *TableArgListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitTableArgExpr(ctx *TableArgExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitDatabaseIdentifier(ctx *DatabaseIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitParamSlot(ctx *ParamSlotContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitFloatingLiteral(ctx *FloatingLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitNumberLiteral(ctx *NumberLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitLiteral(ctx *LiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitInterval(ctx *IntervalContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitAlias(ctx *AliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar2Visitor) VisitEnumValue(ctx *EnumValueContext) interface{} {
	return v.VisitChildren(ctx)
}
