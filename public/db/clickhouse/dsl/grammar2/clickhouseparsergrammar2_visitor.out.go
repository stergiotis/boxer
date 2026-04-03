// Code generated from ClickHouseParserGrammar2.g4 by ANTLR 4.13.2. DO NOT EDIT.

package grammar2 // ClickHouseParserGrammar2
import "github.com/antlr4-go/antlr/v4"

// A complete Visitor for a parse tree produced by ClickHouseParserGrammar2.
type ClickHouseParserGrammar2Visitor interface {
	antlr.ParseTreeVisitor

	// Visit a parse tree produced by ClickHouseParserGrammar2#queryStmt.
	VisitQueryStmt(ctx *QueryStmtContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#query.
	VisitQuery(ctx *QueryContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ctes.
	VisitCtes(ctx *CtesContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#namedQuery.
	VisitNamedQuery(ctx *NamedQueryContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#columnAliases.
	VisitColumnAliases(ctx *ColumnAliasesContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#selectUnionStmt.
	VisitSelectUnionStmt(ctx *SelectUnionStmtContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#selectUnionStmtItem.
	VisitSelectUnionStmtItem(ctx *SelectUnionStmtItemContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#selectStmtWithParens.
	VisitSelectStmtWithParens(ctx *SelectStmtWithParensContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#selectStmt.
	VisitSelectStmt(ctx *SelectStmtContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#projectionClause.
	VisitProjectionClause(ctx *ProjectionClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#projectionExceptClause.
	VisitProjectionExceptClause(ctx *ProjectionExceptClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#StaticColumnList.
	VisitStaticColumnList(ctx *StaticColumnListContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#DynamicColumnList.
	VisitDynamicColumnList(ctx *DynamicColumnListContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#dynamicColumnSelection.
	VisitDynamicColumnSelection(ctx *DynamicColumnSelectionContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#withClause.
	VisitWithClause(ctx *WithClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#topClause.
	VisitTopClause(ctx *TopClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#fromClause.
	VisitFromClause(ctx *FromClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#arrayJoinClause.
	VisitArrayJoinClause(ctx *ArrayJoinClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#windowClause.
	VisitWindowClause(ctx *WindowClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#qualifyClause.
	VisitQualifyClause(ctx *QualifyClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#prewhereClause.
	VisitPrewhereClause(ctx *PrewhereClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#whereClause.
	VisitWhereClause(ctx *WhereClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#groupByClause.
	VisitGroupByClause(ctx *GroupByClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#havingClause.
	VisitHavingClause(ctx *HavingClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#orderByClause.
	VisitOrderByClause(ctx *OrderByClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#limitByClause.
	VisitLimitByClause(ctx *LimitByClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#limitClause.
	VisitLimitClause(ctx *LimitClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#settingsClause.
	VisitSettingsClause(ctx *SettingsClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#JoinExprOp.
	VisitJoinExprOp(ctx *JoinExprOpContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#JoinExprTable.
	VisitJoinExprTable(ctx *JoinExprTableContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#JoinExprParens.
	VisitJoinExprParens(ctx *JoinExprParensContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#JoinExprCrossOp.
	VisitJoinExprCrossOp(ctx *JoinExprCrossOpContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#JoinOpInner.
	VisitJoinOpInner(ctx *JoinOpInnerContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#JoinOpLeftRight.
	VisitJoinOpLeftRight(ctx *JoinOpLeftRightContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#JoinOpFull.
	VisitJoinOpFull(ctx *JoinOpFullContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#joinOpCross.
	VisitJoinOpCross(ctx *JoinOpCrossContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#joinConstraintClause.
	VisitJoinConstraintClause(ctx *JoinConstraintClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#sampleClause.
	VisitSampleClause(ctx *SampleClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#limitExpr.
	VisitLimitExpr(ctx *LimitExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#orderExprList.
	VisitOrderExprList(ctx *OrderExprListContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#orderExpr.
	VisitOrderExpr(ctx *OrderExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ratioExpr.
	VisitRatioExpr(ctx *RatioExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#settingExprList.
	VisitSettingExprList(ctx *SettingExprListContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#settingExpr.
	VisitSettingExpr(ctx *SettingExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#SettingLiteral.
	VisitSettingLiteral(ctx *SettingLiteralContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#SettingEmptyArray.
	VisitSettingEmptyArray(ctx *SettingEmptyArrayContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#SettingArray.
	VisitSettingArray(ctx *SettingArrayContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#SettingTuple.
	VisitSettingTuple(ctx *SettingTupleContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#SettingFunctionEmpty.
	VisitSettingFunctionEmpty(ctx *SettingFunctionEmptyContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#SettingFunction.
	VisitSettingFunction(ctx *SettingFunctionContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#windowExpr.
	VisitWindowExpr(ctx *WindowExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#winPartitionByClause.
	VisitWinPartitionByClause(ctx *WinPartitionByClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#winOrderByClause.
	VisitWinOrderByClause(ctx *WinOrderByClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#winFrameClause.
	VisitWinFrameClause(ctx *WinFrameClauseContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#frameStart.
	VisitFrameStart(ctx *FrameStartContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#frameBetween.
	VisitFrameBetween(ctx *FrameBetweenContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#winFrameBound.
	VisitWinFrameBound(ctx *WinFrameBoundContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#setStmt.
	VisitSetStmt(ctx *SetStmtContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnTypeExprSimple.
	VisitColumnTypeExprSimple(ctx *ColumnTypeExprSimpleContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnTypeExprNested.
	VisitColumnTypeExprNested(ctx *ColumnTypeExprNestedContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnTypeExprEnum.
	VisitColumnTypeExprEnum(ctx *ColumnTypeExprEnumContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnTypeExprComplex.
	VisitColumnTypeExprComplex(ctx *ColumnTypeExprComplexContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnTypeExprParam.
	VisitColumnTypeExprParam(ctx *ColumnTypeExprParamContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#columnExprList.
	VisitColumnExprList(ctx *ColumnExprListContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnsExprAsterisk.
	VisitColumnsExprAsterisk(ctx *ColumnsExprAsteriskContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnsExprSubquery.
	VisitColumnsExprSubquery(ctx *ColumnsExprSubqueryContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnsExprColumn.
	VisitColumnsExprColumn(ctx *ColumnsExprColumnContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnExprAlias.
	VisitColumnExprAlias(ctx *ColumnExprAliasContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnExprNegate.
	VisitColumnExprNegate(ctx *ColumnExprNegateContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnExprSubquery.
	VisitColumnExprSubquery(ctx *ColumnExprSubqueryContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnExprLiteral.
	VisitColumnExprLiteral(ctx *ColumnExprLiteralContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnExprParamSlot.
	VisitColumnExprParamSlot(ctx *ColumnExprParamSlotContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnExprBetween.
	VisitColumnExprBetween(ctx *ColumnExprBetweenContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnExprOr.
	VisitColumnExprOr(ctx *ColumnExprOrContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnExprParens.
	VisitColumnExprParens(ctx *ColumnExprParensContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnExprDynamic.
	VisitColumnExprDynamic(ctx *ColumnExprDynamicContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnExprPrecedence1.
	VisitColumnExprPrecedence1(ctx *ColumnExprPrecedence1Context) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnExprPrecedence2.
	VisitColumnExprPrecedence2(ctx *ColumnExprPrecedence2Context) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnExprPrecedence3.
	VisitColumnExprPrecedence3(ctx *ColumnExprPrecedence3Context) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnExprAnd.
	VisitColumnExprAnd(ctx *ColumnExprAndContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnExprInterval.
	VisitColumnExprInterval(ctx *ColumnExprIntervalContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnExprNot.
	VisitColumnExprNot(ctx *ColumnExprNotContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnExprIsNull.
	VisitColumnExprIsNull(ctx *ColumnExprIsNullContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnExprWinFunction.
	VisitColumnExprWinFunction(ctx *ColumnExprWinFunctionContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnExprIdentifier.
	VisitColumnExprIdentifier(ctx *ColumnExprIdentifierContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnExprWinFunctionTarget.
	VisitColumnExprWinFunctionTarget(ctx *ColumnExprWinFunctionTargetContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnExprFunction.
	VisitColumnExprFunction(ctx *ColumnExprFunctionContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#ColumnExprAsterisk.
	VisitColumnExprAsterisk(ctx *ColumnExprAsteriskContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#columnArgList.
	VisitColumnArgList(ctx *ColumnArgListContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#columnArgExpr.
	VisitColumnArgExpr(ctx *ColumnArgExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#columnLambdaExpr.
	VisitColumnLambdaExpr(ctx *ColumnLambdaExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#columnIdentifier.
	VisitColumnIdentifier(ctx *ColumnIdentifierContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#nestedIdentifier.
	VisitNestedIdentifier(ctx *NestedIdentifierContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#TableExprIdentifier.
	VisitTableExprIdentifier(ctx *TableExprIdentifierContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#TableExprSubquery.
	VisitTableExprSubquery(ctx *TableExprSubqueryContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#TableExprAlias.
	VisitTableExprAlias(ctx *TableExprAliasContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#TableExprFunction.
	VisitTableExprFunction(ctx *TableExprFunctionContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#tableFunctionExpr.
	VisitTableFunctionExpr(ctx *TableFunctionExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#tableIdentifier.
	VisitTableIdentifier(ctx *TableIdentifierContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#tableArgList.
	VisitTableArgList(ctx *TableArgListContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#tableArgExpr.
	VisitTableArgExpr(ctx *TableArgExprContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#databaseIdentifier.
	VisitDatabaseIdentifier(ctx *DatabaseIdentifierContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#paramSlot.
	VisitParamSlot(ctx *ParamSlotContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#floatingLiteral.
	VisitFloatingLiteral(ctx *FloatingLiteralContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#numberLiteral.
	VisitNumberLiteral(ctx *NumberLiteralContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#literal.
	VisitLiteral(ctx *LiteralContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#interval.
	VisitInterval(ctx *IntervalContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#alias.
	VisitAlias(ctx *AliasContext) interface{}

	// Visit a parse tree produced by ClickHouseParserGrammar2#enumValue.
	VisitEnumValue(ctx *EnumValueContext) interface{}
}
