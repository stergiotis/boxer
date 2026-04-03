// Code generated from ClickHouseParserGrammar1.g4 by ANTLR 4.13.2. DO NOT EDIT.

package grammar1 // ClickHouseParserGrammar1
import "github.com/antlr4-go/antlr/v4"

// A complete Visitor for a parse tree produced by ClickHouseParserGrammar1.
type ClickHouseParserGrammar1Visitor interface {
	antlr.ParseTreeVisitor

	// Visit a parse tree produced by ClickHouseParserGrammar1#queryStmt.
	VisitQueryStmt(ctx *QueryStmtContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#query.
	VisitQuery(ctx *QueryContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#multiQuery.
	VisitMultiQuery(ctx *MultiQueryContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ctes.
	VisitCtes(ctx *CtesContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#namedQuery.
	VisitNamedQuery(ctx *NamedQueryContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#columnAliases.
	VisitColumnAliases(ctx *ColumnAliasesContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#selectUnionStmt.
	VisitSelectUnionStmt(ctx *SelectUnionStmtContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#selectUnionStmtItem.
	VisitSelectUnionStmtItem(ctx *SelectUnionStmtItemContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#selectStmtWithParens.
	VisitSelectStmtWithParens(ctx *SelectStmtWithParensContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#selectStmt.
	VisitSelectStmt(ctx *SelectStmtContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#projectionClause.
	VisitProjectionClause(ctx *ProjectionClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#projectionExceptClause.
	VisitProjectionExceptClause(ctx *ProjectionExceptClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#StaticColumnList.
	VisitStaticColumnList(ctx *StaticColumnListContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#DynamicColumnList.
	VisitDynamicColumnList(ctx *DynamicColumnListContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#dynamicColumnSelection.
	VisitDynamicColumnSelection(ctx *DynamicColumnSelectionContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#withClause.
	VisitWithClause(ctx *WithClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#topClause.
	VisitTopClause(ctx *TopClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#fromClause.
	VisitFromClause(ctx *FromClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#arrayJoinClause.
	VisitArrayJoinClause(ctx *ArrayJoinClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#windowClause.
	VisitWindowClause(ctx *WindowClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#qualifyClause.
	VisitQualifyClause(ctx *QualifyClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#prewhereClause.
	VisitPrewhereClause(ctx *PrewhereClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#whereClause.
	VisitWhereClause(ctx *WhereClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#groupByClause.
	VisitGroupByClause(ctx *GroupByClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#havingClause.
	VisitHavingClause(ctx *HavingClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#interpolateExprs.
	VisitInterpolateExprs(ctx *InterpolateExprsContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#orderByClause.
	VisitOrderByClause(ctx *OrderByClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#limitByClause.
	VisitLimitByClause(ctx *LimitByClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#limitClause.
	VisitLimitClause(ctx *LimitClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#settingsClause.
	VisitSettingsClause(ctx *SettingsClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#JoinExprOp.
	VisitJoinExprOp(ctx *JoinExprOpContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#JoinExprTable.
	VisitJoinExprTable(ctx *JoinExprTableContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#JoinExprParens.
	VisitJoinExprParens(ctx *JoinExprParensContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#JoinExprCrossOp.
	VisitJoinExprCrossOp(ctx *JoinExprCrossOpContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#JoinOpInner.
	VisitJoinOpInner(ctx *JoinOpInnerContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#JoinOpLeftRight.
	VisitJoinOpLeftRight(ctx *JoinOpLeftRightContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#JoinOpFull.
	VisitJoinOpFull(ctx *JoinOpFullContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#joinOpCross.
	VisitJoinOpCross(ctx *JoinOpCrossContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#joinConstraintClause.
	VisitJoinConstraintClause(ctx *JoinConstraintClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#sampleClause.
	VisitSampleClause(ctx *SampleClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#limitExpr.
	VisitLimitExpr(ctx *LimitExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#orderExprList.
	VisitOrderExprList(ctx *OrderExprListContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#orderExpr.
	VisitOrderExpr(ctx *OrderExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ratioExpr.
	VisitRatioExpr(ctx *RatioExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#settingExprList.
	VisitSettingExprList(ctx *SettingExprListContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#settingExpr.
	VisitSettingExpr(ctx *SettingExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#SettingLiteral.
	VisitSettingLiteral(ctx *SettingLiteralContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#SettingEmptyArray.
	VisitSettingEmptyArray(ctx *SettingEmptyArrayContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#SettingArray.
	VisitSettingArray(ctx *SettingArrayContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#SettingTuple.
	VisitSettingTuple(ctx *SettingTupleContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#SettingFunctionEmpty.
	VisitSettingFunctionEmpty(ctx *SettingFunctionEmptyContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#SettingFunction.
	VisitSettingFunction(ctx *SettingFunctionContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#windowExpr.
	VisitWindowExpr(ctx *WindowExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#winPartitionByClause.
	VisitWinPartitionByClause(ctx *WinPartitionByClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#winOrderByClause.
	VisitWinOrderByClause(ctx *WinOrderByClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#winFrameClause.
	VisitWinFrameClause(ctx *WinFrameClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#frameStart.
	VisitFrameStart(ctx *FrameStartContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#frameBetween.
	VisitFrameBetween(ctx *FrameBetweenContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#winFrameBound.
	VisitWinFrameBound(ctx *WinFrameBoundContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#setStmt.
	VisitSetStmt(ctx *SetStmtContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnTypeExprSimple.
	VisitColumnTypeExprSimple(ctx *ColumnTypeExprSimpleContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnTypeExprNested.
	VisitColumnTypeExprNested(ctx *ColumnTypeExprNestedContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnTypeExprEnum.
	VisitColumnTypeExprEnum(ctx *ColumnTypeExprEnumContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnTypeExprComplex.
	VisitColumnTypeExprComplex(ctx *ColumnTypeExprComplexContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnTypeExprParam.
	VisitColumnTypeExprParam(ctx *ColumnTypeExprParamContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#columnExprList.
	VisitColumnExprList(ctx *ColumnExprListContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnsExprAsterisk.
	VisitColumnsExprAsterisk(ctx *ColumnsExprAsteriskContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnsExprSubquery.
	VisitColumnsExprSubquery(ctx *ColumnsExprSubqueryContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnsExprColumn.
	VisitColumnsExprColumn(ctx *ColumnsExprColumnContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprTernaryOp.
	VisitColumnExprTernaryOp(ctx *ColumnExprTernaryOpContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprAlias.
	VisitColumnExprAlias(ctx *ColumnExprAliasContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprExtract.
	VisitColumnExprExtract(ctx *ColumnExprExtractContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprNegate.
	VisitColumnExprNegate(ctx *ColumnExprNegateContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprSubquery.
	VisitColumnExprSubquery(ctx *ColumnExprSubqueryContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprLiteral.
	VisitColumnExprLiteral(ctx *ColumnExprLiteralContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprParamSlot.
	VisitColumnExprParamSlot(ctx *ColumnExprParamSlotContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprArray.
	VisitColumnExprArray(ctx *ColumnExprArrayContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprSubstring.
	VisitColumnExprSubstring(ctx *ColumnExprSubstringContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprCast.
	VisitColumnExprCast(ctx *ColumnExprCastContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprOr.
	VisitColumnExprOr(ctx *ColumnExprOrContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprDynamic.
	VisitColumnExprDynamic(ctx *ColumnExprDynamicContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprPrecedence1.
	VisitColumnExprPrecedence1(ctx *ColumnExprPrecedence1Context) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprPrecedence2.
	VisitColumnExprPrecedence2(ctx *ColumnExprPrecedence2Context) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprPrecedence3.
	VisitColumnExprPrecedence3(ctx *ColumnExprPrecedence3Context) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprInterval.
	VisitColumnExprInterval(ctx *ColumnExprIntervalContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprIsNull.
	VisitColumnExprIsNull(ctx *ColumnExprIsNullContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprWinFunctionTarget.
	VisitColumnExprWinFunctionTarget(ctx *ColumnExprWinFunctionTargetContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprTrim.
	VisitColumnExprTrim(ctx *ColumnExprTrimContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprTuple.
	VisitColumnExprTuple(ctx *ColumnExprTupleContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprArrayAccess.
	VisitColumnExprArrayAccess(ctx *ColumnExprArrayAccessContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprBetween.
	VisitColumnExprBetween(ctx *ColumnExprBetweenContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprParens.
	VisitColumnExprParens(ctx *ColumnExprParensContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprTimestamp.
	VisitColumnExprTimestamp(ctx *ColumnExprTimestampContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprAnd.
	VisitColumnExprAnd(ctx *ColumnExprAndContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprTupleAccess.
	VisitColumnExprTupleAccess(ctx *ColumnExprTupleAccessContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprCase.
	VisitColumnExprCase(ctx *ColumnExprCaseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprDate.
	VisitColumnExprDate(ctx *ColumnExprDateContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprNot.
	VisitColumnExprNot(ctx *ColumnExprNotContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprWinFunction.
	VisitColumnExprWinFunction(ctx *ColumnExprWinFunctionContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprIdentifier.
	VisitColumnExprIdentifier(ctx *ColumnExprIdentifierContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprFunction.
	VisitColumnExprFunction(ctx *ColumnExprFunctionContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#ColumnExprAsterisk.
	VisitColumnExprAsterisk(ctx *ColumnExprAsteriskContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#columnArgList.
	VisitColumnArgList(ctx *ColumnArgListContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#columnArgExpr.
	VisitColumnArgExpr(ctx *ColumnArgExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#columnLambdaExpr.
	VisitColumnLambdaExpr(ctx *ColumnLambdaExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#columnIdentifier.
	VisitColumnIdentifier(ctx *ColumnIdentifierContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#nestedIdentifier.
	VisitNestedIdentifier(ctx *NestedIdentifierContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#TableExprIdentifier.
	VisitTableExprIdentifier(ctx *TableExprIdentifierContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#TableExprSubquery.
	VisitTableExprSubquery(ctx *TableExprSubqueryContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#TableExprAlias.
	VisitTableExprAlias(ctx *TableExprAliasContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#TableExprFunction.
	VisitTableExprFunction(ctx *TableExprFunctionContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#tableFunctionExpr.
	VisitTableFunctionExpr(ctx *TableFunctionExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#tableIdentifier.
	VisitTableIdentifier(ctx *TableIdentifierContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#tableArgList.
	VisitTableArgList(ctx *TableArgListContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#tableArgExpr.
	VisitTableArgExpr(ctx *TableArgExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#databaseIdentifier.
	VisitDatabaseIdentifier(ctx *DatabaseIdentifierContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#paramSlot.
	VisitParamSlot(ctx *ParamSlotContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#floatingLiteral.
	VisitFloatingLiteral(ctx *FloatingLiteralContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#numberLiteral.
	VisitNumberLiteral(ctx *NumberLiteralContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#literal.
	VisitLiteral(ctx *LiteralContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#interval.
	VisitInterval(ctx *IntervalContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#keyword.
	VisitKeyword(ctx *KeywordContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#alias.
	VisitAlias(ctx *AliasContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#identifier.
	VisitIdentifier(ctx *IdentifierContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#identifierOrNull.
	VisitIdentifierOrNull(ctx *IdentifierOrNullContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar1#enumValue.
	VisitEnumValue(ctx *EnumValueContext) interface{}
}
