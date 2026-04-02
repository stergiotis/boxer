// Grammar2 — ClickHouse SELECT parser for canonical/normalized SQL only.
//
// This grammar is the input grammar for the CST→AST conversion pass. It accepts
// ONLY the canonical form of each construct. Non-canonical forms cause parse
// errors, serving as a structural guarantee that the normalization pipeline
// has done its job.
//
// === Identifier normalization ===
//
// All identifiers are double-quoted IDENTIFIER tokens:
//   col → "col", `col` → "col", keyword-as-name → "keyword"
// The identifier rule is a single terminal: IDENTIFIER.
//
// === Removed columnExpr alternatives ===
//
//   ColumnExprCast        — CAST(e AS T), e::T  → CAST(e, 'T')
//   ColumnExprCase        — CASE ... END        → caseWithExpression() / caseWithoutExpression()
//   ColumnExprDate        — DATE 'str'           → toDate('str')
//   ColumnExprTimestamp    — TIMESTAMP 'str'      → toDateTime('str')
//   ColumnExprExtract     — EXTRACT(X FROM e)    → extract(e, 'X')
//   ColumnExprSubstring   — SUBSTRING(e FROM ..) → substring(e, n, m)
//   ColumnExprTrim        — TRIM(BOTH s FROM e)  → trimBoth(e, s)
//   ColumnExprArrayAccess — e[i]                 → arrayElement(e, i)
//   ColumnExprTupleAccess — e.N                  → tupleElement(e, N)
//   ColumnExprArray       — [1,2,3]              → array(1,2,3)
//   ColumnExprTuple       — (1,2,3)              → tuple(1,2,3)
//   ColumnExprTernaryOp   — a ? b : c            → if(a, b, c)
//
// === Removed from top level ===
//
//   INTO OUTFILE, WITH FILL / INTERPOLATE, multiQuery
//
// === JOIN canonicalization ===
//
//   Keyword order: strictness before direction (ALL LEFT, not LEFT ALL)
//   Comma join removed (use CROSS JOIN)
//   USING requires parentheses
//   OUTER keyword removed (LEFT = LEFT OUTER)
//
// === Operator canonicalization ===
//
//   == removed (use = only)
//
// Original: Copyright (c) 2016-2025 ClickHouse Inc see LICENSE
// Modifications: Copyright (c) 2025 Panos Stergiotis, Apache 2.0

parser grammar ClickHouseParserGrammar2;

options {
    tokenVocab = ClickHouseLexer;
}

// Top-level statements
queryStmt: query (FORMAT IDENTIFIER)? (SEMICOLON)? EOF;
query: setStmt* ctes? selectUnionStmt;

// CTE statement
ctes
    : WITH namedQuery (COMMA namedQuery)*
    ;

namedQuery
    : name=IDENTIFIER (columnAliases)? AS LPAREN query RPAREN
    ;

columnAliases
    : LPAREN IDENTIFIER (COMMA IDENTIFIER)* RPAREN
    ;

// SELECT statement

selectUnionStmt: selectStmtWithParens selectUnionStmtItem*;
selectUnionStmtItem: (( UNION | EXCEPT | INTERSECT ) ( ALL | DISTINCT )? selectStmtWithParens);
selectStmtWithParens: selectStmt | (LPAREN selectUnionStmt RPAREN);
selectStmt:
    withClause?
    projectionClause
    fromClause?
    arrayJoinClause?
    windowClause?
    qualifyClause?
    prewhereClause?
    whereClause?
    groupByClause?
    havingClause?
    orderByClause?
    limitByClause?
    limitClause?
    settingsClause?
    ;

projectionClause : SELECT DISTINCT? topClause? columnExprList projectionExceptClause?;
projectionExceptClause : EXCEPT staticOrDynamicColumnSelection;
staticOrDynamicColumnSelection
    : IDENTIFIER (COMMA IDENTIFIER)*       # StaticColumnList
    | dynamicColumnSelection               # DynamicColumnList;
dynamicColumnSelection
    : COLUMNS LPAREN STRING_LITERAL RPAREN;
withClause: WITH columnExprList;
topClause: TOP DECIMAL_LITERAL (WITH TIES)?;
fromClause: FROM joinExpr;
arrayJoinClause: (LEFT | INNER)? ARRAY JOIN columnExprList;
windowClause: WINDOW IDENTIFIER AS LPAREN windowExpr RPAREN;
qualifyClause: QUALIFY columnExpr;
prewhereClause: PREWHERE columnExpr;
whereClause: WHERE columnExpr;
groupByClause: GROUP BY ((CUBE | ROLLUP) LPAREN columnExprList RPAREN | columnExprList) (WITH (CUBE | ROLLUP))? (WITH TOTALS)?;
havingClause: HAVING columnExpr;
orderByClause: ORDER BY orderExprList;
limitByClause: LIMIT limitExpr BY columnExprList;
limitClause: LIMIT limitExpr (WITH TIES)?;
settingsClause: SETTINGS settingExprList;

// ==========================================================================
// JOIN — canonical form only
//
// joinOp: strictness always precedes direction. No OUTER keyword.
//   Canonical: (ALL|ANY|SEMI|ANTI|ASOF)? (INNER|LEFT|RIGHT|FULL)
//   Rejected:  LEFT ALL, LEFT OUTER, FULL OUTER ANY
//
// joinOpCross: CROSS JOIN only, no comma join.
//
// joinConstraintClause: USING requires parentheses.
// ==========================================================================
joinExpr
    : joinExpr (GLOBAL | LOCAL)? joinOp? JOIN joinExpr joinConstraintClause  # JoinExprOp
    | joinExpr joinOpCross joinExpr                                          # JoinExprCrossOp
    | tableExpr FINAL? sampleClause?                                         # JoinExprTable
    | LPAREN joinExpr RPAREN                                                 # JoinExprParens
    ;
joinOp
    : (ALL | ANY | ASOF)? INNER                                              # JoinOpInner
    | (SEMI | ALL | ANTI | ANY | ASOF)? (LEFT | RIGHT)                       # JoinOpLeftRight
    | (ALL | ANY)? FULL                                                      # JoinOpFull
    ;
joinOpCross
    : (GLOBAL|LOCAL)? CROSS JOIN
    ;
joinConstraintClause
    : ON columnExprList
    | USING LPAREN columnExprList RPAREN
    ;

sampleClause: SAMPLE ratioExpr (OFFSET ratioExpr)?;
limitExpr: columnExpr ((COMMA | OFFSET) columnExpr)?;
orderExprList: orderExpr (COMMA orderExpr)*;
orderExpr: columnExpr (ASCENDING | DESCENDING | DESC)? (NULLS (FIRST | LAST))? (COLLATE STRING_LITERAL)?;
ratioExpr: numberLiteral (SLASH numberLiteral)?;
settingExprList: settingExpr (COMMA settingExpr)*;
settingExpr: IDENTIFIER EQ_SINGLE settingValue;

settingValue
    : literal                                                              # SettingLiteral
    | LBRACKET RBRACKET                                                    # SettingEmptyArray
    | LBRACKET settingValue (COMMA settingValue)* RBRACKET                 # SettingArray
    | LPAREN settingValue COMMA settingValue (COMMA settingValue)* RPAREN  # SettingTuple
    | IDENTIFIER LPAREN RPAREN                                             # SettingFunctionEmpty
    | IDENTIFIER LPAREN settingValue (COMMA settingValue)* RPAREN          # SettingFunction
    ;

windowExpr: winPartitionByClause? winOrderByClause? winFrameClause?;
winPartitionByClause: PARTITION BY columnExprList;
winOrderByClause: ORDER BY orderExprList;
winFrameClause: (ROWS | RANGE) winFrameExtend;
winFrameExtend
    : winFrameBound                             # frameStart
    | BETWEEN winFrameBound AND winFrameBound   # frameBetween
    ;
winFrameBound: (CURRENT ROW | UNBOUNDED PRECEDING | UNBOUNDED FOLLOWING | numberLiteral PRECEDING | numberLiteral FOLLOWING);


// SET statement

setStmt: SET settingExprList SEMICOLON;

// Columns

columnTypeExpr
    : IDENTIFIER                                                                               # ColumnTypeExprSimple   // UInt64
    | IDENTIFIER LPAREN IDENTIFIER columnTypeExpr (COMMA IDENTIFIER columnTypeExpr)* RPAREN    # ColumnTypeExprNested   // Nested
    | IDENTIFIER LPAREN enumValue (COMMA enumValue)* RPAREN                                    # ColumnTypeExprEnum     // Enum
    | IDENTIFIER LPAREN columnTypeExpr (COMMA columnTypeExpr)* RPAREN                          # ColumnTypeExprComplex  // Array, Tuple
    | IDENTIFIER LPAREN columnExprList? RPAREN                                                 # ColumnTypeExprParam    // FixedString(N)
    ;
columnExprList: columnsExpr (COMMA columnsExpr)*;
columnsExpr
    : (tableIdentifier DOT)? ASTERISK  # ColumnsExprAsterisk
    | LPAREN selectUnionStmt RPAREN    # ColumnsExprSubquery
    | columnExpr                       # ColumnsExprColumn
    ;

// ==========================================================================
// columnExpr — CANONICAL FORMS ONLY
//
// All syntactic sugar removed → parsed as ColumnExprFunction.
// CASE removed → caseWithExpression() / caseWithoutExpression().
// Ternary removed → if(cond, then, else).
// == removed from comparison operators.
// ==========================================================================
columnExpr
    : INTERVAL columnExpr interval                                                        # ColumnExprInterval
    | IDENTIFIER (LPAREN columnExprList? RPAREN) OVER LPAREN windowExpr RPAREN            # ColumnExprWinFunction
    | IDENTIFIER (LPAREN columnExprList? RPAREN) OVER IDENTIFIER                          # ColumnExprWinFunctionTarget
    | IDENTIFIER (LPAREN columnExprList? RPAREN)? LPAREN DISTINCT? columnArgList? RPAREN  # ColumnExprFunction
    | literal                                                                             # ColumnExprLiteral
    | paramSlot                                                                           # ColumnExprParamSlot

    | DASH columnExpr                                                                     # ColumnExprNegate
    | columnExpr ( ASTERISK
                 | SLASH
                 | PERCENT
                 ) columnExpr                                                             # ColumnExprPrecedence1
    | columnExpr ( PLUS
                 | DASH
                 | CONCAT
                 ) columnExpr                                                             # ColumnExprPrecedence2
    | columnExpr ( EQ_SINGLE
                 | NOT_EQ
                 | LE
                 | GE
                 | LT
                 | GT
                 | GLOBAL? NOT? IN
                 | NOT? (LIKE | ILIKE)
                 ) columnExpr                                                             # ColumnExprPrecedence3
    | columnExpr IS NOT? NULL_SQL                                                         # ColumnExprIsNull
    | NOT columnExpr                                                                      # ColumnExprNot
    | columnExpr AND columnExpr                                                           # ColumnExprAnd
    | columnExpr OR columnExpr                                                            # ColumnExprOr
    | columnExpr NOT? BETWEEN columnExpr AND columnExpr                                   # ColumnExprBetween
    | columnExpr (alias | AS IDENTIFIER)                                                  # ColumnExprAlias

    | (tableIdentifier DOT)? ASTERISK                                                     # ColumnExprAsterisk
    | LPAREN selectUnionStmt RPAREN                                                       # ColumnExprSubquery
    | LPAREN columnExpr RPAREN                                                            # ColumnExprParens
    | columnIdentifier                                                                    # ColumnExprIdentifier
    | dynamicColumnSelection                                                              # ColumnExprDynamic
    ;
columnArgList: columnArgExpr (COMMA columnArgExpr)*;
columnArgExpr: columnLambdaExpr | columnExpr;
columnLambdaExpr:
    ( LPAREN IDENTIFIER (COMMA IDENTIFIER)* RPAREN
    |        IDENTIFIER (COMMA IDENTIFIER)*
    )
    ARROW columnExpr
    ;
columnIdentifier: (tableIdentifier DOT)? nestedIdentifier;
nestedIdentifier: IDENTIFIER (DOT IDENTIFIER)?;

// Tables

tableExpr
    : tableIdentifier                    # TableExprIdentifier
    | tableFunctionExpr                  # TableExprFunction
    | LPAREN selectUnionStmt RPAREN      # TableExprSubquery
    | tableExpr (alias | AS IDENTIFIER)  # TableExprAlias
    ;
tableFunctionExpr: IDENTIFIER LPAREN tableArgList? RPAREN;
tableIdentifier: (databaseIdentifier DOT)? IDENTIFIER;
tableArgList: tableArgExpr (COMMA tableArgExpr)*;
tableArgExpr
    : nestedIdentifier
    | tableFunctionExpr
    | literal
    ;

// Databases

databaseIdentifier: IDENTIFIER;

// Basics
paramSlot: (LBRACE IDENTIFIER COLON columnTypeExpr RBRACE);
floatingLiteral
    : FLOATING_LITERAL
    | DOT (DECIMAL_LITERAL | OCTAL_LITERAL)
    | DECIMAL_LITERAL DOT (DECIMAL_LITERAL | OCTAL_LITERAL)?
    ;
numberLiteral: (PLUS | DASH)? (floatingLiteral | OCTAL_LITERAL | DECIMAL_LITERAL | HEXADECIMAL_LITERAL | INF | NAN_SQL);
literal
    : numberLiteral
    | STRING_LITERAL
    | NULL_SQL
    | JSON_TRUE
    | JSON_FALSE
    ;
interval: SECOND | MINUTE | HOUR | DAY | WEEK | MONTH | QUARTER | YEAR;

alias: IDENTIFIER;
enumValue: STRING_LITERAL EQ_SINGLE numberLiteral;