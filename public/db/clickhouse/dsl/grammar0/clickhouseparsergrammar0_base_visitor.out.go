// Code generated from ClickHouseParserGrammar0.g4 by ANTLR 4.13.2. DO NOT EDIT.

package grammar0 // ClickHouseParserGrammar0
import "github.com/antlr4-go/antlr/v4"

type BaseClickHouseParserGrammar0Visitor struct {
	*antlr.BaseParseTreeVisitor
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitQueryStmt(ctx *QueryStmtContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitQuery(ctx *QueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitMultiQuery(ctx *MultiQueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitCtes(ctx *CtesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitNamedQuery(ctx *NamedQueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnAliases(ctx *ColumnAliasesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitSelectUnionStmt(ctx *SelectUnionStmtContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitSelectUnionStmtItem(ctx *SelectUnionStmtItemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitSelectStmtWithParens(ctx *SelectStmtWithParensContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitSelectStmt(ctx *SelectStmtContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitProjectionClause(ctx *ProjectionClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitProjectionExceptClause(ctx *ProjectionExceptClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitStaticColumnList(ctx *StaticColumnListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitDynamicColumnList(ctx *DynamicColumnListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitDynamicColumnSelection(ctx *DynamicColumnSelectionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitWithClause(ctx *WithClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitTopClause(ctx *TopClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitFromClause(ctx *FromClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitArrayJoinClause(ctx *ArrayJoinClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitWindowClause(ctx *WindowClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitQualifyClause(ctx *QualifyClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitPrewhereClause(ctx *PrewhereClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitWhereClause(ctx *WhereClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitGroupByClause(ctx *GroupByClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitHavingClause(ctx *HavingClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitInterpolateExprs(ctx *InterpolateExprsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitOrderByClause(ctx *OrderByClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitLimitByClause(ctx *LimitByClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitLimitClause(ctx *LimitClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitSettingsClause(ctx *SettingsClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitJoinExprOp(ctx *JoinExprOpContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitJoinExprTable(ctx *JoinExprTableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitJoinExprParens(ctx *JoinExprParensContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitJoinExprCrossOp(ctx *JoinExprCrossOpContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitJoinOpInner(ctx *JoinOpInnerContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitJoinOpLeftRight(ctx *JoinOpLeftRightContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitJoinOpFull(ctx *JoinOpFullContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitJoinOpCross(ctx *JoinOpCrossContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitJoinConstraintClause(ctx *JoinConstraintClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitSampleClause(ctx *SampleClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitLimitExpr(ctx *LimitExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitOrderExprList(ctx *OrderExprListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitOrderExpr(ctx *OrderExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitRatioExpr(ctx *RatioExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitSettingExprList(ctx *SettingExprListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitSettingExpr(ctx *SettingExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitSettingLiteral(ctx *SettingLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitSettingEmptyArray(ctx *SettingEmptyArrayContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitSettingArray(ctx *SettingArrayContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitSettingTuple(ctx *SettingTupleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitSettingFunctionEmpty(ctx *SettingFunctionEmptyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitSettingFunction(ctx *SettingFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitWindowExpr(ctx *WindowExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitWinPartitionByClause(ctx *WinPartitionByClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitWinOrderByClause(ctx *WinOrderByClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitWinFrameClause(ctx *WinFrameClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitFrameStart(ctx *FrameStartContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitFrameBetween(ctx *FrameBetweenContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitWinFrameBound(ctx *WinFrameBoundContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitSetStmt(ctx *SetStmtContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnTypeExprSimple(ctx *ColumnTypeExprSimpleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnTypeExprNested(ctx *ColumnTypeExprNestedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnTypeExprEnum(ctx *ColumnTypeExprEnumContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnTypeExprComplex(ctx *ColumnTypeExprComplexContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnTypeExprParam(ctx *ColumnTypeExprParamContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprList(ctx *ColumnExprListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnsExprAsterisk(ctx *ColumnsExprAsteriskContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnsExprSubquery(ctx *ColumnsExprSubqueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnsExprColumn(ctx *ColumnsExprColumnContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprTernaryOp(ctx *ColumnExprTernaryOpContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprAlias(ctx *ColumnExprAliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprExtract(ctx *ColumnExprExtractContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprNegate(ctx *ColumnExprNegateContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprSubquery(ctx *ColumnExprSubqueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprLiteral(ctx *ColumnExprLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprParamSlot(ctx *ColumnExprParamSlotContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprArray(ctx *ColumnExprArrayContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprSubstring(ctx *ColumnExprSubstringContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprCast(ctx *ColumnExprCastContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprOr(ctx *ColumnExprOrContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprDynamic(ctx *ColumnExprDynamicContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprPrecedence1(ctx *ColumnExprPrecedence1Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprPrecedence2(ctx *ColumnExprPrecedence2Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprPrecedence3(ctx *ColumnExprPrecedence3Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprInterval(ctx *ColumnExprIntervalContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprIsNull(ctx *ColumnExprIsNullContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprWinFunctionTarget(ctx *ColumnExprWinFunctionTargetContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprTrim(ctx *ColumnExprTrimContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprTuple(ctx *ColumnExprTupleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprArrayAccess(ctx *ColumnExprArrayAccessContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprBetween(ctx *ColumnExprBetweenContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprParens(ctx *ColumnExprParensContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprTimestamp(ctx *ColumnExprTimestampContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprAnd(ctx *ColumnExprAndContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprTupleAccess(ctx *ColumnExprTupleAccessContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprCase(ctx *ColumnExprCaseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprDate(ctx *ColumnExprDateContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprNot(ctx *ColumnExprNotContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprWinFunction(ctx *ColumnExprWinFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprIdentifier(ctx *ColumnExprIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprFunction(ctx *ColumnExprFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnExprAsterisk(ctx *ColumnExprAsteriskContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnArgList(ctx *ColumnArgListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnArgExpr(ctx *ColumnArgExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnLambdaExpr(ctx *ColumnLambdaExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitColumnIdentifier(ctx *ColumnIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitNestedIdentifier(ctx *NestedIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitTableExprIdentifier(ctx *TableExprIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitTableExprSubquery(ctx *TableExprSubqueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitTableExprAlias(ctx *TableExprAliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitTableExprFunction(ctx *TableExprFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitTableFunctionExpr(ctx *TableFunctionExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitTableIdentifier(ctx *TableIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitTableArgList(ctx *TableArgListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitTableArgExpr(ctx *TableArgExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitDatabaseIdentifier(ctx *DatabaseIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitParamSlot(ctx *ParamSlotContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitFloatingLiteral(ctx *FloatingLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitNumberLiteral(ctx *NumberLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitLiteral(ctx *LiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitInterval(ctx *IntervalContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitKeyword(ctx *KeywordContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitKeywordForAlias(ctx *KeywordForAliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitAlias(ctx *AliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitIdentifier(ctx *IdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitIdentifierOrNull(ctx *IdentifierOrNullContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar0Visitor) VisitEnumValue(ctx *EnumValueContext) interface{} {
	return v.VisitChildren(ctx)
}
