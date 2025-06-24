// Code generated from ClickHouseParser.g4 by ANTLR 4.13.1. DO NOT EDIT.

package grammar // ClickHouseParser
import "github.com/antlr4-go/antlr/v4"

type BaseClickHouseParserVisitor struct {
	*antlr.BaseParseTreeVisitor
}

func (v *BaseClickHouseParserVisitor) VisitQueryStmt(ctx *QueryStmtContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitQuery(ctx *QueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitCtes(ctx *CtesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitNamedQuery(ctx *NamedQueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnAliases(ctx *ColumnAliasesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitSelectUnionStmt(ctx *SelectUnionStmtContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitSelectUnionStmtItem(ctx *SelectUnionStmtItemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitSelectStmtWithParens(ctx *SelectStmtWithParensContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitSelectStmt(ctx *SelectStmtContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitProjectionClause(ctx *ProjectionClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitProjectionExceptClause(ctx *ProjectionExceptClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitStaticColumnList(ctx *StaticColumnListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitDynamicColumnList(ctx *DynamicColumnListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitDynamicColumnSelection(ctx *DynamicColumnSelectionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitWithClause(ctx *WithClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitTopClause(ctx *TopClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitFromClause(ctx *FromClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitArrayJoinClause(ctx *ArrayJoinClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitWindowClause(ctx *WindowClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitQualifyClause(ctx *QualifyClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitPrewhereClause(ctx *PrewhereClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitWhereClause(ctx *WhereClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitGroupByClause(ctx *GroupByClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitHavingClause(ctx *HavingClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitInterpolateExprs(ctx *InterpolateExprsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitOrderByClause(ctx *OrderByClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitLimitByClause(ctx *LimitByClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitLimitClause(ctx *LimitClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitSettingsClause(ctx *SettingsClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitJoinExprOp(ctx *JoinExprOpContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitJoinExprTable(ctx *JoinExprTableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitJoinExprParens(ctx *JoinExprParensContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitJoinExprCrossOp(ctx *JoinExprCrossOpContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitJoinOpInner(ctx *JoinOpInnerContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitJoinOpLeftRight(ctx *JoinOpLeftRightContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitJoinOpFull(ctx *JoinOpFullContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitJoinOpCross(ctx *JoinOpCrossContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitJoinConstraintClause(ctx *JoinConstraintClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitSampleClause(ctx *SampleClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitLimitExpr(ctx *LimitExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitOrderExprList(ctx *OrderExprListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitOrderExpr(ctx *OrderExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitRatioExpr(ctx *RatioExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitSettingExprList(ctx *SettingExprListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitSettingExpr(ctx *SettingExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitWindowExpr(ctx *WindowExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitWinPartitionByClause(ctx *WinPartitionByClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitWinOrderByClause(ctx *WinOrderByClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitWinFrameClause(ctx *WinFrameClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitFrameStart(ctx *FrameStartContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitFrameBetween(ctx *FrameBetweenContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitWinFrameBound(ctx *WinFrameBoundContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitSetStmt(ctx *SetStmtContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnTypeExprSimple(ctx *ColumnTypeExprSimpleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnTypeExprNested(ctx *ColumnTypeExprNestedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnTypeExprEnum(ctx *ColumnTypeExprEnumContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnTypeExprComplex(ctx *ColumnTypeExprComplexContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnTypeExprParam(ctx *ColumnTypeExprParamContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprList(ctx *ColumnExprListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnsExprAsterisk(ctx *ColumnsExprAsteriskContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnsExprSubquery(ctx *ColumnsExprSubqueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnsExprColumn(ctx *ColumnsExprColumnContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprTernaryOp(ctx *ColumnExprTernaryOpContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprAlias(ctx *ColumnExprAliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprExtract(ctx *ColumnExprExtractContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprNegate(ctx *ColumnExprNegateContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprSubquery(ctx *ColumnExprSubqueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprLiteral(ctx *ColumnExprLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprParamSlot(ctx *ColumnExprParamSlotContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprArray(ctx *ColumnExprArrayContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprSubstring(ctx *ColumnExprSubstringContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprCast(ctx *ColumnExprCastContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprOr(ctx *ColumnExprOrContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprDynamic(ctx *ColumnExprDynamicContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprPrecedence1(ctx *ColumnExprPrecedence1Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprPrecedence2(ctx *ColumnExprPrecedence2Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprPrecedence3(ctx *ColumnExprPrecedence3Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprInterval(ctx *ColumnExprIntervalContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprIsNull(ctx *ColumnExprIsNullContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprWinFunctionTarget(ctx *ColumnExprWinFunctionTargetContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprTrim(ctx *ColumnExprTrimContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprTuple(ctx *ColumnExprTupleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprArrayAccess(ctx *ColumnExprArrayAccessContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprBetween(ctx *ColumnExprBetweenContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprParens(ctx *ColumnExprParensContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprTimestamp(ctx *ColumnExprTimestampContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprAnd(ctx *ColumnExprAndContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprTupleAccess(ctx *ColumnExprTupleAccessContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprCase(ctx *ColumnExprCaseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprDate(ctx *ColumnExprDateContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprNot(ctx *ColumnExprNotContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprWinFunction(ctx *ColumnExprWinFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprIdentifier(ctx *ColumnExprIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprFunction(ctx *ColumnExprFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnExprAsterisk(ctx *ColumnExprAsteriskContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnArgList(ctx *ColumnArgListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnArgExpr(ctx *ColumnArgExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnLambdaExpr(ctx *ColumnLambdaExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitColumnIdentifier(ctx *ColumnIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitNestedIdentifier(ctx *NestedIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitTableExprIdentifier(ctx *TableExprIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitTableExprSubquery(ctx *TableExprSubqueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitTableExprAlias(ctx *TableExprAliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitTableExprFunction(ctx *TableExprFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitTableFunctionExpr(ctx *TableFunctionExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitTableIdentifier(ctx *TableIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitTableArgList(ctx *TableArgListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitTableArgExpr(ctx *TableArgExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitDatabaseIdentifier(ctx *DatabaseIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitParamSlot(ctx *ParamSlotContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitFloatingLiteral(ctx *FloatingLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitNumberLiteral(ctx *NumberLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitLiteral(ctx *LiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitInterval(ctx *IntervalContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitKeyword(ctx *KeywordContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitKeywordForAlias(ctx *KeywordForAliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitAlias(ctx *AliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitIdentifier(ctx *IdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitIdentifierOrNull(ctx *IdentifierOrNullContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserVisitor) VisitEnumValue(ctx *EnumValueContext) interface{} {
	return v.VisitChildren(ctx)
}
