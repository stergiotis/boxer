// Code generated from ClickHouseParserGrammar1.g4 by ANTLR 4.13.2. DO NOT EDIT.

package grammar1 // ClickHouseParserGrammar1
import "github.com/antlr4-go/antlr/v4"

type BaseClickHouseParserGrammar1Visitor struct {
	*antlr.BaseParseTreeVisitor
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitQueryStmt(ctx *QueryStmtContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitQuery(ctx *QueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitMultiQuery(ctx *MultiQueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitCtes(ctx *CtesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitNamedQuery(ctx *NamedQueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnAliases(ctx *ColumnAliasesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitSelectUnionStmt(ctx *SelectUnionStmtContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitSelectUnionStmtItem(ctx *SelectUnionStmtItemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitSelectStmtWithParens(ctx *SelectStmtWithParensContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitSelectStmt(ctx *SelectStmtContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitProjectionClause(ctx *ProjectionClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitProjectionExceptClause(ctx *ProjectionExceptClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitStaticColumnList(ctx *StaticColumnListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitDynamicColumnList(ctx *DynamicColumnListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitDynamicColumnSelection(ctx *DynamicColumnSelectionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitWithClause(ctx *WithClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitTopClause(ctx *TopClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitFromClause(ctx *FromClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitArrayJoinClause(ctx *ArrayJoinClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitWindowClause(ctx *WindowClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitQualifyClause(ctx *QualifyClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitPrewhereClause(ctx *PrewhereClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitWhereClause(ctx *WhereClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitGroupByClause(ctx *GroupByClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitHavingClause(ctx *HavingClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitInterpolateExprs(ctx *InterpolateExprsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitOrderByClause(ctx *OrderByClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitLimitByClause(ctx *LimitByClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitLimitClause(ctx *LimitClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitSettingsClause(ctx *SettingsClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitJoinExprOp(ctx *JoinExprOpContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitJoinExprTable(ctx *JoinExprTableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitJoinExprParens(ctx *JoinExprParensContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitJoinExprCrossOp(ctx *JoinExprCrossOpContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitJoinOpInner(ctx *JoinOpInnerContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitJoinOpLeftRight(ctx *JoinOpLeftRightContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitJoinOpFull(ctx *JoinOpFullContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitJoinOpCross(ctx *JoinOpCrossContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitJoinConstraintClause(ctx *JoinConstraintClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitSampleClause(ctx *SampleClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitLimitExpr(ctx *LimitExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitOrderExprList(ctx *OrderExprListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitOrderExpr(ctx *OrderExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitRatioExpr(ctx *RatioExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitSettingExprList(ctx *SettingExprListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitSettingExpr(ctx *SettingExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitSettingLiteral(ctx *SettingLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitSettingEmptyArray(ctx *SettingEmptyArrayContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitSettingArray(ctx *SettingArrayContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitSettingTuple(ctx *SettingTupleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitSettingFunctionEmpty(ctx *SettingFunctionEmptyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitSettingFunction(ctx *SettingFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitWindowExpr(ctx *WindowExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitWinPartitionByClause(ctx *WinPartitionByClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitWinOrderByClause(ctx *WinOrderByClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitWinFrameClause(ctx *WinFrameClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitFrameStart(ctx *FrameStartContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitFrameBetween(ctx *FrameBetweenContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitWinFrameBound(ctx *WinFrameBoundContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitSetStmt(ctx *SetStmtContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnTypeExprSimple(ctx *ColumnTypeExprSimpleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnTypeExprNested(ctx *ColumnTypeExprNestedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnTypeExprEnum(ctx *ColumnTypeExprEnumContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnTypeExprComplex(ctx *ColumnTypeExprComplexContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnTypeExprParam(ctx *ColumnTypeExprParamContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprList(ctx *ColumnExprListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnsExprAsterisk(ctx *ColumnsExprAsteriskContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnsExprSubquery(ctx *ColumnsExprSubqueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnsExprColumn(ctx *ColumnsExprColumnContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprTernaryOp(ctx *ColumnExprTernaryOpContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprAlias(ctx *ColumnExprAliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprExtract(ctx *ColumnExprExtractContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprNegate(ctx *ColumnExprNegateContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprSubquery(ctx *ColumnExprSubqueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprLiteral(ctx *ColumnExprLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprParamSlot(ctx *ColumnExprParamSlotContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprArray(ctx *ColumnExprArrayContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprSubstring(ctx *ColumnExprSubstringContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprCast(ctx *ColumnExprCastContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprOr(ctx *ColumnExprOrContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprDynamic(ctx *ColumnExprDynamicContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprPrecedence1(ctx *ColumnExprPrecedence1Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprPrecedence2(ctx *ColumnExprPrecedence2Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprPrecedence3(ctx *ColumnExprPrecedence3Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprInterval(ctx *ColumnExprIntervalContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprIsNull(ctx *ColumnExprIsNullContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprWinFunctionTarget(ctx *ColumnExprWinFunctionTargetContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprTrim(ctx *ColumnExprTrimContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprTuple(ctx *ColumnExprTupleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprArrayAccess(ctx *ColumnExprArrayAccessContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprBetween(ctx *ColumnExprBetweenContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprParens(ctx *ColumnExprParensContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprTimestamp(ctx *ColumnExprTimestampContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprAnd(ctx *ColumnExprAndContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprTupleAccess(ctx *ColumnExprTupleAccessContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprCase(ctx *ColumnExprCaseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprDate(ctx *ColumnExprDateContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprNot(ctx *ColumnExprNotContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprWinFunction(ctx *ColumnExprWinFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprIdentifier(ctx *ColumnExprIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprFunction(ctx *ColumnExprFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnExprAsterisk(ctx *ColumnExprAsteriskContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnArgList(ctx *ColumnArgListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnArgExpr(ctx *ColumnArgExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnLambdaExpr(ctx *ColumnLambdaExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitColumnIdentifier(ctx *ColumnIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitNestedIdentifier(ctx *NestedIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitTableExprIdentifier(ctx *TableExprIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitTableExprSubquery(ctx *TableExprSubqueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitTableExprAlias(ctx *TableExprAliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitTableExprFunction(ctx *TableExprFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitTableFunctionExpr(ctx *TableFunctionExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitTableIdentifier(ctx *TableIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitTableArgList(ctx *TableArgListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitTableArgExpr(ctx *TableArgExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitDatabaseIdentifier(ctx *DatabaseIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitParamSlot(ctx *ParamSlotContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitFloatingLiteral(ctx *FloatingLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitNumberLiteral(ctx *NumberLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitLiteral(ctx *LiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitInterval(ctx *IntervalContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitKeyword(ctx *KeywordContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitAlias(ctx *AliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitIdentifier(ctx *IdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitIdentifierOrNull(ctx *IdentifierOrNullContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseClickHouseParserGrammar1Visitor) VisitEnumValue(ctx *EnumValueContext) interface{} {
	return v.VisitChildren(ctx)
}
