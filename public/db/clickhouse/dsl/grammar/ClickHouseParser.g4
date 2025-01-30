// Original: Copyright (c) 2016-2025 ClickHouse Inc see LICENSE
// Modifications: Copyright (c) 2025 Panos Stergiotis, Apache 2.0

/*
    Important: Consumers visiting the resulting parse tree will rely on rules having a specific form.

    Homogenous lists with terminal(s) between may be expressed as usual:
    ```antlr4
    columnAliases : LPAREN identifier (COMMA identifier)* RPAREN;
    ```
    Inhomogenous lists need to be re-written by putting the kleen star part in a separate rule:
    ```antlr4
    selectUnionStmt: selectStmtWithParens (UNION ( ALL | DISTINCT )? selectStmtWithParens)*;
    ```
    becomes
    ```antlr4
    selectUnionStmt: selectStmtWithParens selectUnionStmtItem*;
    selectUnionStmtItem: (UNION ( ALL | DISTINCT )? selectStmtWithParens);
    ```
    The same as for kleen star applies for optional (?) and 1 to many (+) repetitions.
*/

parser grammar ClickHouseParser;

options {
    tokenVocab = ClickHouseLexer;
}

// Top-level statements
queryStmt: query (INTO OUTFILE STRING_LITERAL)? (FORMAT identifierOrNull)? (SEMICOLON)? EOF;
query: setStmt* ctes? selectUnionStmt;

// CTE statement
ctes
    : WITH namedQuery (COMMA namedQuery)*
    ;

namedQuery
    : name=identifier (columnAliases)? AS LPAREN query RPAREN
    ;

columnAliases
    : LPAREN identifier (COMMA identifier)* RPAREN
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

projectionClause : SELECT DISTINCT? topClause? columnExprList;
withClause: WITH columnExprList;
topClause: TOP DECIMAL_LITERAL (WITH TIES)?;
fromClause: FROM joinExpr;
arrayJoinClause: (LEFT | INNER)? ARRAY JOIN columnExprList;
windowClause: WINDOW identifier AS LPAREN windowExpr RPAREN;
qualifyClause: QUALIFY columnExpr;
prewhereClause: PREWHERE columnExpr;
whereClause: WHERE columnExpr;
groupByClause: GROUP BY ((CUBE | ROLLUP) LPAREN columnExprList RPAREN | columnExprList) (WITH (CUBE | ROLLUP))? (WITH TOTALS)?;
havingClause: HAVING columnExpr;
interpolateExprs : (columnExpr (AS columnExpr)?) (COMMA columnExpr (AS columnExpr)?)*; // FIXME itemize kleen
orderByClause: ORDER BY orderExprList (WITH FILL (FROM columnExpr)? (TO columnExpr)? (STEP columnExpr)? (STALENESS columnExpr)?
                                      (INTERPOLATE interpolateExprs)?)?;
limitByClause: LIMIT limitExpr BY columnExprList;
limitClause: LIMIT limitExpr (WITH TIES)?;
settingsClause: SETTINGS settingExprList;

joinExpr
    : joinExpr (GLOBAL | LOCAL)? joinOp? JOIN joinExpr joinConstraintClause  # JoinExprOp
    | joinExpr joinOpCross joinExpr                                          # JoinExprCrossOp
    | tableExpr FINAL? sampleClause?                                         # JoinExprTable
    | LPAREN joinExpr RPAREN                                                 # JoinExprParens
    ;
joinOp
    : ((ALL | ANY | ASOF)? INNER | INNER (ALL | ANY | ASOF)? | (ALL | ANY | ASOF))  # JoinOpInner
    | ( (SEMI | ALL | ANTI | ANY | ASOF)? (LEFT | RIGHT) OUTER?
      | (LEFT | RIGHT) OUTER? (SEMI | ALL | ANTI | ANY | ASOF)?
      )                                                                             # JoinOpLeftRight
    | ((ALL | ANY)? FULL OUTER? | FULL OUTER? (ALL | ANY)?)                         # JoinOpFull
    ;
joinOpCross
    : (GLOBAL|LOCAL)? CROSS JOIN
    | COMMA
    ;
joinConstraintClause
    : ON columnExprList
    | USING LPAREN columnExprList RPAREN
    | USING columnExprList
    ;

sampleClause: SAMPLE ratioExpr (OFFSET ratioExpr)?;
limitExpr: columnExpr ((COMMA | OFFSET) columnExpr)?;
orderExprList: orderExpr (COMMA orderExpr)*;
orderExpr: columnExpr (ASCENDING | DESCENDING | DESC)? (NULLS (FIRST | LAST))? (COLLATE STRING_LITERAL)?;
ratioExpr: numberLiteral (SLASH numberLiteral)?;
settingExprList: settingExpr (COMMA settingExpr)*;
settingExpr: identifier EQ_SINGLE literal;

windowExpr: winPartitionByClause? winOrderByClause? winFrameClause?;
winPartitionByClause: PARTITION BY columnExprList;
winOrderByClause: ORDER BY orderExprList;
winFrameClause: (ROWS | RANGE) winFrameExtend;
winFrameExtend
    : winFrameBound                             # frameStart
    | BETWEEN winFrameBound AND winFrameBound   # frameBetween
    ;
winFrameBound: (CURRENT ROW | UNBOUNDED PRECEDING | UNBOUNDED FOLLOWING | numberLiteral PRECEDING | numberLiteral FOLLOWING);
//rangeClause: RANGE LPAREN (MIN identifier MAX identifier | MAX identifier MIN identifier) RPAREN;


// SET statement

setStmt: SET settingExprList SEMICOLON;

// Columns

columnTypeExpr
    : identifier                                                                             # ColumnTypeExprSimple   // UInt64
    | identifier LPAREN identifier columnTypeExpr (COMMA identifier columnTypeExpr)* RPAREN  # ColumnTypeExprNested   // Nested    // FIXME itemize kleen
    | identifier LPAREN enumValue (COMMA enumValue)* RPAREN                                  # ColumnTypeExprEnum     // Enum
    | identifier LPAREN columnTypeExpr (COMMA columnTypeExpr)* RPAREN                        # ColumnTypeExprComplex  // Array, Tuple
    | identifier LPAREN columnExprList? RPAREN                                               # ColumnTypeExprParam    // FixedString(N)
    ;
columnExprList: columnsExpr (COMMA columnsExpr)*;
columnsExpr
    : (tableIdentifier DOT)? ASTERISK  # ColumnsExprAsterisk
    | LPAREN selectUnionStmt RPAREN    # ColumnsExprSubquery
    // NOTE: asterisk and subquery goes before |columnExpr| so that we can mark them as multi-column expressions.
    | columnExpr                       # ColumnsExprColumn
    ;
columnExpr
    : CASE columnExpr? (WHEN columnExpr THEN columnExpr)+ (ELSE columnExpr)? END          # ColumnExprCase
    | CAST LPAREN columnExpr AS columnTypeExpr RPAREN                                     # ColumnExprCast
    | columnExpr DOUBLE_COLON columnTypeExpr                                              # ColumnExprCast
    | DATE STRING_LITERAL                                                                 # ColumnExprDate
    | EXTRACT LPAREN interval FROM columnExpr RPAREN                                      # ColumnExprExtract
    | INTERVAL columnExpr interval                                                        # ColumnExprInterval
    | SUBSTRING LPAREN columnExpr FROM columnExpr (FOR columnExpr)? RPAREN                # ColumnExprSubstring
    | TIMESTAMP STRING_LITERAL                                                            # ColumnExprTimestamp
    | TRIM LPAREN (BOTH | LEADING | TRAILING) STRING_LITERAL FROM columnExpr RPAREN       # ColumnExprTrim
    | identifier (LPAREN columnExprList? RPAREN) OVER LPAREN windowExpr RPAREN            # ColumnExprWinFunction
    | identifier (LPAREN columnExprList? RPAREN) OVER identifier                          # ColumnExprWinFunctionTarget
    | identifier (LPAREN columnExprList? RPAREN)? LPAREN DISTINCT? columnArgList? RPAREN  # ColumnExprFunction
    | literal                                                                             # ColumnExprLiteral
    | paramSlot                                                                           # ColumnExprParamSlot

    // FIXME(ilezhankin): this part looks very ugly, maybe there is another way to express it
    | columnExpr LBRACKET columnExpr RBRACKET                                             # ColumnExprArrayAccess
    | columnExpr DOT DECIMAL_LITERAL                                                      # ColumnExprTupleAccess
    | DASH columnExpr                                                                     # ColumnExprNegate
    | columnExpr ( ASTERISK                                                               // multiply
                 | SLASH                                                                  // divide
                 | PERCENT                                                                // modulo
                 ) columnExpr                                                             # ColumnExprPrecedence1
    | columnExpr ( PLUS                                                                   // plus
                 | DASH                                                                   // minus
                 | CONCAT                                                                 // concat
                 ) columnExpr                                                             # ColumnExprPrecedence2
    | columnExpr ( EQ_DOUBLE                                                              // equals
                 | EQ_SINGLE                                                              // equals
                 | NOT_EQ                                                                 // notEquals
                 | LE                                                                     // lessOrEquals
                 | GE                                                                     // greaterOrEquals
                 | LT                                                                     // less
                 | GT                                                                     // greater
                 | GLOBAL? NOT? IN                                                        // in, notIn, globalIn, globalNotIn
                 | NOT? (LIKE | ILIKE)                                                    // like, notLike, ilike, notILike
                 ) columnExpr                                                             # ColumnExprPrecedence3
    | columnExpr IS NOT? NULL_SQL                                                         # ColumnExprIsNull
    | NOT columnExpr                                                                      # ColumnExprNot
    | columnExpr AND columnExpr                                                           # ColumnExprAnd
    | columnExpr OR columnExpr                                                            # ColumnExprOr
    // TODO(ilezhankin): `BETWEEN a AND b AND c` is parsed in a wrong way: `BETWEEN (a AND b) AND c`
    | columnExpr NOT? BETWEEN columnExpr AND columnExpr                                   # ColumnExprBetween
    | <assoc=right> columnExpr QUERY columnExpr COLON columnExpr                          # ColumnExprTernaryOp
    | columnExpr (alias | AS identifier)                                                  # ColumnExprAlias

    | (tableIdentifier DOT)? ASTERISK                                                     # ColumnExprAsterisk  // single-column only
    | LPAREN selectUnionStmt RPAREN                                                       # ColumnExprSubquery  // single-column only
    | LPAREN columnExpr RPAREN                                                            # ColumnExprParens    // single-column only
    | LPAREN columnExprList RPAREN                                                        # ColumnExprTuple
    | LBRACKET columnExprList? RBRACKET                                                   # ColumnExprArray
    | columnIdentifier                                                                    # ColumnExprIdentifier
    ;
columnArgList: columnArgExpr (COMMA columnArgExpr)*;
columnArgExpr: columnLambdaExpr | columnExpr;
columnLambdaExpr:
    ( LPAREN identifier (COMMA identifier)* RPAREN
    |        identifier (COMMA identifier)*
    )
    ARROW columnExpr
    ;
columnIdentifier: (tableIdentifier DOT)? nestedIdentifier;
nestedIdentifier: identifier (DOT identifier)?;

// Tables

tableExpr
    : tableIdentifier                    # TableExprIdentifier
    | tableFunctionExpr                  # TableExprFunction
    | LPAREN selectUnionStmt RPAREN      # TableExprSubquery
    | tableExpr (alias | AS identifier)  # TableExprAlias
    ;
tableFunctionExpr: identifier LPAREN tableArgList? RPAREN;
tableIdentifier: (databaseIdentifier DOT)? identifier;
tableArgList: tableArgExpr (COMMA tableArgExpr)*;
tableArgExpr
    : nestedIdentifier
    | tableFunctionExpr
    | literal
    ;

// Databases

databaseIdentifier: identifier;

// Basics
paramSlot: (LBRACE identifier COLON columnTypeExpr RBRACE);
floatingLiteral
    : FLOATING_LITERAL
    | DOT (DECIMAL_LITERAL | OCTAL_LITERAL)
    | DECIMAL_LITERAL DOT (DECIMAL_LITERAL | OCTAL_LITERAL)?  // can't move this to the lexer or it will break nested tuple access: t.1.2
    ;
numberLiteral: (PLUS | DASH)? (floatingLiteral | OCTAL_LITERAL | DECIMAL_LITERAL | HEXADECIMAL_LITERAL | INF | NAN_SQL);
literal
    : numberLiteral
    | STRING_LITERAL
    | NULL_SQL
    ;
interval: SECOND | MINUTE | HOUR | DAY | WEEK | MONTH | QUARTER | YEAR;
keyword
    // except NULL_SQL, INF, NAN_SQL
    : AFTER | ALIAS | ALL | ALTER | AND | ANTI | ANY | ARRAY | AS | ASCENDING | ASOF | AST | ASYNC | ATTACH | BETWEEN | BOTH | BY | CASE
    | CAST | CHECK | CLEAR | CLUSTER | CODEC | COLLATE | COLUMN | COMMENT | CONSTRAINT | CREATE | CROSS | CUBE | CURRENT | DATABASE
    | DATABASES | DATE | DEDUPLICATE | DEFAULT | DELAY | DELETE | DESCRIBE | DESC | DESCENDING | DETACH | DICTIONARIES | DICTIONARY | DISK
    | DISTINCT | DISTRIBUTED | DROP | ELSE | END | ENGINE | EVENTS | EXISTS | EXPLAIN | EXPRESSION | EXTRACT | FETCHES | FINAL | FIRST
    | FLUSH | FOR | FOLLOWING | FORMAT | FREEZE | FROM | FULL | FUNCTION | GLOBAL | GRANULARITY | GROUP | HAVING | HIERARCHICAL | ID
    | IF | ILIKE | IN | INDEX | INJECTIVE | INNER | INSERT | INTERVAL | INTO | IS | IS_OBJECT_ID | JOIN | JSON_FALSE | JSON_TRUE | KEY
    | KILL | LAST | LAYOUT | LEADING | LEFT | LIFETIME | LIKE | LIMIT | LIVE | LOCAL | LOGS | MATERIALIZE | MATERIALIZED | MAX | MERGES
    | MIN | MODIFY | MOVE | MUTATION | NO | NOT | NULLS | OFFSET | ON | OPTIMIZE | OR | ORDER | OUTER | OUTFILE | OVER | PARTITION
    | POPULATE | PRECEDING | PREWHERE | PRIMARY | RANGE | RELOAD | REMOVE | RENAME | REPLACE | REPLICA | REPLICATED | RIGHT | ROLLUP | ROW
    | ROWS | SAMPLE | SELECT | SEMI | SENDS | SET | SETTINGS | SHOW | SOURCE | START | STOP | SUBSTRING | SYNC | SYNTAX | SYSTEM | TABLE
    | TABLES | TEMPORARY | TEST | THEN | TIES | TIMEOUT | TIMESTAMP | TOTALS | TRAILING | TRIM | TRUNCATE | TO | TOP | TTL | TYPE
    | UNBOUNDED | UNION | UPDATE | USE | USING | UUID | VALUES | VIEW | VOLUME | WATCH | WHEN | WHERE | WINDOW | WITH | FILL | STEP
    | STALENESS | INTERPOLATE | INTERSECT | EXCEPT
    ;
keywordForAlias
    : AFTER | ALIAS | ALTER | ASCENDING | AST | ASYNC | ATTACH | BOTH | BY | CASE | CAST | CHECK | CLEAR | CLUSTER | CODEC | COLLATE
    | COLUMN | COMMENT | CONSTRAINT | CREATE | CUBE | CURRENT | DATABASE | DATABASES | DATE | DEDUPLICATE | DEFAULT | DELAY | DELETE
    | DESCRIBE | DESC | DESCENDING | DETACH | DICTIONARIES | DICTIONARY | DISK | DISTRIBUTED | DROP | ELSE | END | ENGINE | EVENTS
    | EXISTS | EXPLAIN | EXPRESSION | EXTRACT | FETCHES | FIRST | FLUSH | FOR | FOLLOWING | FREEZE | FUNCTION | GRANULARITY | HIERARCHICAL
    | ID | IF | INDEX | INJECTIVE | INSERT | INTERVAL | IS_OBJECT_ID | KEY | KILL | LAST | LAYOUT | LEADING | LIFETIME | LIVE | LOCAL
    | LOGS | MATERIALIZE | MATERIALIZED | MAX | MERGES | MIN | MODIFY | MOVE | MUTATION | NO | NULLS | OPTIMIZE | OUTER | OUTFILE | OVER
    | PARTITION | POPULATE | PRECEDING | PRIMARY | RANGE | RELOAD | REMOVE | RENAME | REPLACE | REPLICA | REPLICATED | ROLLUP | ROW
    | ROWS | SELECT | SENDS | SET | SHOW | SOURCE | START | STOP | SUBSTRING | SYNC | SYNTAX | SYSTEM | TABLE | TABLES | TEMPORARY
    | TEST | THEN | TIES | TIMEOUT | TIMESTAMP | TOTALS | TRAILING | TRIM | TRUNCATE | TO | TTL | TYPE | UNBOUNDED | UPDATE
    | USE | UUID | VALUES | VIEW | VOLUME | WATCH | WHEN
    ;
alias: IDENTIFIER | keywordForAlias;  // |interval| can't be an alias, otherwise 'INTERVAL 1 SOMETHING' becomes ambiguous.
identifier: IDENTIFIER | interval | keyword;
identifierOrNull: identifier | NULL_SQL;  // NULL_SQL can be only 'Null' here.
enumValue: STRING_LITERAL EQ_SINGLE numberLiteral;