// Code generated from ClickHouseParserGrammar0.g4 by ANTLR 4.13.2. DO NOT EDIT.

package grammar0 // ClickHouseParserGrammar0
import "github.com/antlr4-go/antlr/v4"

// A complete Visitor for a parse tree produced by ClickHouseParserGrammar0.
type ClickHouseParserGrammar0Visitor interface {
	antlr.ParseTreeVisitor

	// Visit a parse tree produced by ClickHouseParserGrammar0#queryStmt.
	VisitQueryStmt(ctx *QueryStmtContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#query.
	VisitQuery(ctx *QueryContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#multiQuery.
	VisitMultiQuery(ctx *MultiQueryContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ctes.
	VisitCtes(ctx *CtesContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#namedQuery.
	VisitNamedQuery(ctx *NamedQueryContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#columnAliases.
	VisitColumnAliases(ctx *ColumnAliasesContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#selectUnionStmt.
	VisitSelectUnionStmt(ctx *SelectUnionStmtContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#selectUnionStmtItem.
	VisitSelectUnionStmtItem(ctx *SelectUnionStmtItemContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#selectStmtWithParens.
	VisitSelectStmtWithParens(ctx *SelectStmtWithParensContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#selectStmt.
	VisitSelectStmt(ctx *SelectStmtContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#projectionClause.
	VisitProjectionClause(ctx *ProjectionClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#projectionExceptClause.
	VisitProjectionExceptClause(ctx *ProjectionExceptClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#StaticColumnList.
	VisitStaticColumnList(ctx *StaticColumnListContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#DynamicColumnList.
	VisitDynamicColumnList(ctx *DynamicColumnListContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#dynamicColumnSelection.
	VisitDynamicColumnSelection(ctx *DynamicColumnSelectionContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#withClause.
	VisitWithClause(ctx *WithClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#topClause.
	VisitTopClause(ctx *TopClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#fromClause.
	VisitFromClause(ctx *FromClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#arrayJoinClause.
	VisitArrayJoinClause(ctx *ArrayJoinClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#windowClause.
	VisitWindowClause(ctx *WindowClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#qualifyClause.
	VisitQualifyClause(ctx *QualifyClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#prewhereClause.
	VisitPrewhereClause(ctx *PrewhereClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#whereClause.
	VisitWhereClause(ctx *WhereClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#groupByClause.
	VisitGroupByClause(ctx *GroupByClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#havingClause.
	VisitHavingClause(ctx *HavingClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#interpolateExprs.
	VisitInterpolateExprs(ctx *InterpolateExprsContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#orderByClause.
	VisitOrderByClause(ctx *OrderByClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#limitByClause.
	VisitLimitByClause(ctx *LimitByClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#limitClause.
	VisitLimitClause(ctx *LimitClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#settingsClause.
	VisitSettingsClause(ctx *SettingsClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#JoinExprOp.
	VisitJoinExprOp(ctx *JoinExprOpContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#JoinExprTable.
	VisitJoinExprTable(ctx *JoinExprTableContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#JoinExprParens.
	VisitJoinExprParens(ctx *JoinExprParensContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#JoinExprCrossOp.
	VisitJoinExprCrossOp(ctx *JoinExprCrossOpContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#JoinOpInner.
	VisitJoinOpInner(ctx *JoinOpInnerContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#JoinOpLeftRight.
	VisitJoinOpLeftRight(ctx *JoinOpLeftRightContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#JoinOpFull.
	VisitJoinOpFull(ctx *JoinOpFullContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#joinOpCross.
	VisitJoinOpCross(ctx *JoinOpCrossContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#joinConstraintClause.
	VisitJoinConstraintClause(ctx *JoinConstraintClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#sampleClause.
	VisitSampleClause(ctx *SampleClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#limitExpr.
	VisitLimitExpr(ctx *LimitExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#orderExprList.
	VisitOrderExprList(ctx *OrderExprListContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#orderExpr.
	VisitOrderExpr(ctx *OrderExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ratioExpr.
	VisitRatioExpr(ctx *RatioExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#settingExprList.
	VisitSettingExprList(ctx *SettingExprListContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#settingExpr.
	VisitSettingExpr(ctx *SettingExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#SettingLiteral.
	VisitSettingLiteral(ctx *SettingLiteralContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#SettingEmptyArray.
	VisitSettingEmptyArray(ctx *SettingEmptyArrayContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#SettingArray.
	VisitSettingArray(ctx *SettingArrayContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#SettingTuple.
	VisitSettingTuple(ctx *SettingTupleContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#SettingFunctionEmpty.
	VisitSettingFunctionEmpty(ctx *SettingFunctionEmptyContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#SettingFunction.
	VisitSettingFunction(ctx *SettingFunctionContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#windowExpr.
	VisitWindowExpr(ctx *WindowExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#winPartitionByClause.
	VisitWinPartitionByClause(ctx *WinPartitionByClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#winOrderByClause.
	VisitWinOrderByClause(ctx *WinOrderByClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#winFrameClause.
	VisitWinFrameClause(ctx *WinFrameClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#frameStart.
	VisitFrameStart(ctx *FrameStartContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#frameBetween.
	VisitFrameBetween(ctx *FrameBetweenContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#winFrameBound.
	VisitWinFrameBound(ctx *WinFrameBoundContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#setStmt.
	VisitSetStmt(ctx *SetStmtContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnTypeExprSimple.
	VisitColumnTypeExprSimple(ctx *ColumnTypeExprSimpleContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnTypeExprNested.
	VisitColumnTypeExprNested(ctx *ColumnTypeExprNestedContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnTypeExprEnum.
	VisitColumnTypeExprEnum(ctx *ColumnTypeExprEnumContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnTypeExprComplex.
	VisitColumnTypeExprComplex(ctx *ColumnTypeExprComplexContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnTypeExprParam.
	VisitColumnTypeExprParam(ctx *ColumnTypeExprParamContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#columnExprList.
	VisitColumnExprList(ctx *ColumnExprListContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnsExprAsterisk.
	VisitColumnsExprAsterisk(ctx *ColumnsExprAsteriskContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnsExprSubquery.
	VisitColumnsExprSubquery(ctx *ColumnsExprSubqueryContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnsExprColumn.
	VisitColumnsExprColumn(ctx *ColumnsExprColumnContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprTernaryOp.
	VisitColumnExprTernaryOp(ctx *ColumnExprTernaryOpContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprAlias.
	VisitColumnExprAlias(ctx *ColumnExprAliasContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprExtract.
	VisitColumnExprExtract(ctx *ColumnExprExtractContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprNegate.
	VisitColumnExprNegate(ctx *ColumnExprNegateContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprSubquery.
	VisitColumnExprSubquery(ctx *ColumnExprSubqueryContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprLiteral.
	VisitColumnExprLiteral(ctx *ColumnExprLiteralContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprParamSlot.
	VisitColumnExprParamSlot(ctx *ColumnExprParamSlotContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprArray.
	VisitColumnExprArray(ctx *ColumnExprArrayContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprSubstring.
	VisitColumnExprSubstring(ctx *ColumnExprSubstringContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprCast.
	VisitColumnExprCast(ctx *ColumnExprCastContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprOr.
	VisitColumnExprOr(ctx *ColumnExprOrContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprDynamic.
	VisitColumnExprDynamic(ctx *ColumnExprDynamicContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprPrecedence1.
	VisitColumnExprPrecedence1(ctx *ColumnExprPrecedence1Context) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprPrecedence2.
	VisitColumnExprPrecedence2(ctx *ColumnExprPrecedence2Context) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprPrecedence3.
	VisitColumnExprPrecedence3(ctx *ColumnExprPrecedence3Context) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprInterval.
	VisitColumnExprInterval(ctx *ColumnExprIntervalContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprIsNull.
	VisitColumnExprIsNull(ctx *ColumnExprIsNullContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprWinFunctionTarget.
	VisitColumnExprWinFunctionTarget(ctx *ColumnExprWinFunctionTargetContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprTrim.
	VisitColumnExprTrim(ctx *ColumnExprTrimContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprTuple.
	VisitColumnExprTuple(ctx *ColumnExprTupleContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprArrayAccess.
	VisitColumnExprArrayAccess(ctx *ColumnExprArrayAccessContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprBetween.
	VisitColumnExprBetween(ctx *ColumnExprBetweenContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprParens.
	VisitColumnExprParens(ctx *ColumnExprParensContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprTimestamp.
	VisitColumnExprTimestamp(ctx *ColumnExprTimestampContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprAnd.
	VisitColumnExprAnd(ctx *ColumnExprAndContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprTupleAccess.
	VisitColumnExprTupleAccess(ctx *ColumnExprTupleAccessContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprCase.
	VisitColumnExprCase(ctx *ColumnExprCaseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprDate.
	VisitColumnExprDate(ctx *ColumnExprDateContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprNot.
	VisitColumnExprNot(ctx *ColumnExprNotContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprWinFunction.
	VisitColumnExprWinFunction(ctx *ColumnExprWinFunctionContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprIdentifier.
	VisitColumnExprIdentifier(ctx *ColumnExprIdentifierContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprFunction.
	VisitColumnExprFunction(ctx *ColumnExprFunctionContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#ColumnExprAsterisk.
	VisitColumnExprAsterisk(ctx *ColumnExprAsteriskContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#columnArgList.
	VisitColumnArgList(ctx *ColumnArgListContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#columnArgExpr.
	VisitColumnArgExpr(ctx *ColumnArgExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#columnLambdaExpr.
	VisitColumnLambdaExpr(ctx *ColumnLambdaExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#columnIdentifier.
	VisitColumnIdentifier(ctx *ColumnIdentifierContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#nestedIdentifier.
	VisitNestedIdentifier(ctx *NestedIdentifierContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#TableExprIdentifier.
	VisitTableExprIdentifier(ctx *TableExprIdentifierContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#TableExprSubquery.
	VisitTableExprSubquery(ctx *TableExprSubqueryContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#TableExprAlias.
	VisitTableExprAlias(ctx *TableExprAliasContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#TableExprFunction.
	VisitTableExprFunction(ctx *TableExprFunctionContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#tableFunctionExpr.
	VisitTableFunctionExpr(ctx *TableFunctionExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#tableIdentifier.
	VisitTableIdentifier(ctx *TableIdentifierContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#tableArgList.
	VisitTableArgList(ctx *TableArgListContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#tableArgExpr.
	VisitTableArgExpr(ctx *TableArgExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#databaseIdentifier.
	VisitDatabaseIdentifier(ctx *DatabaseIdentifierContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#paramSlot.
	VisitParamSlot(ctx *ParamSlotContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#floatingLiteral.
	VisitFloatingLiteral(ctx *FloatingLiteralContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#numberLiteral.
	VisitNumberLiteral(ctx *NumberLiteralContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#literal.
	VisitLiteral(ctx *LiteralContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#interval.
	VisitInterval(ctx *IntervalContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#keyword.
	VisitKeyword(ctx *KeywordContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#keywordForAlias.
	VisitKeywordForAlias(ctx *KeywordForAliasContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#alias.
	VisitAlias(ctx *AliasContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#identifier.
	VisitIdentifier(ctx *IdentifierContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#identifierOrNull.
	VisitIdentifierOrNull(ctx *IdentifierOrNullContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar0#enumValue.
	VisitEnumValue(ctx *EnumValueContext) interface{}
}
