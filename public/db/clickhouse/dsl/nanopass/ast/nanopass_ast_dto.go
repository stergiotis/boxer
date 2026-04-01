package ast

// ============================================================================
// Top-level
// ============================================================================

// Query is the top-level AST node for a ClickHouse query.
// Grammar: queryStmt → query (INTO OUTFILE ...)? (FORMAT ...)? ;? EOF
//
//	query    → setStmt* ctes? selectUnionStmt
type Query struct {
	// Settings are SET key=value pairs preceding the query.
	// Grammar: setStmt → SET settingExprList SEMICOLON
	Settings []SettingPair `cbor:"settings,omitempty"`

	// CTEs are Common Table Expressions from the top-level WITH clause.
	// Grammar: ctes → WITH namedQuery (COMMA namedQuery)*
	CTEs []CTE `cbor:"ctes,omitempty"`

	// Body is the SELECT UNION chain.
	Body SelectUnion `cbor:"body"`

	// OutFile is the INTO OUTFILE path (empty if absent).
	OutFile string `cbor:"out_file,omitempty"`

	// Format is the FORMAT identifier (empty if absent).
	Format string `cbor:"format,omitempty"`
}

// ============================================================================
// SELECT / UNION
// ============================================================================

// SelectUnion represents a chain of SELECT statements joined by set operations.
// Grammar: selectUnionStmt → selectStmtWithParens selectUnionStmtItem*
type SelectUnion struct {
	// Head is the first SELECT.
	Head Select `cbor:"head"`

	// Items are subsequent UNION/EXCEPT/INTERSECT operations.
	Items []SelectUnionItem `cbor:"items,omitempty"`
}

// SelectUnionItem is a single set operation in a UNION chain.
// Grammar: selectUnionStmtItem → (UNION|EXCEPT|INTERSECT) (ALL|DISTINCT)? selectStmtWithParens
type SelectUnionItem struct {
	// Op is the set operation: "UNION", "EXCEPT", "INTERSECT".
	Op string `cbor:"op"`

	// Modifier is "ALL", "DISTINCT", or "" (default).
	Modifier string `cbor:"mod,omitempty"`

	// Select is the right-hand SELECT.
	Select Select `cbor:"select"`
}

// Select represents a single SELECT statement with all clauses.
// Grammar: selectStmt → withClause? projectionClause fromClause? arrayJoinClause?
//
//	windowClause? qualifyClause? prewhereClause? whereClause?
//	groupByClause? havingClause? orderByClause? limitByClause?
//	limitClause? settingsClause?
type Select struct {
	// Distinct indicates SELECT DISTINCT.
	Distinct bool `cbor:"distinct,omitempty"`

	// Top is the TOP N clause (ClickHouse extension).
	// Grammar: topClause → TOP DECIMAL_LITERAL (WITH TIES)?
	Top *TopClause `cbor:"top,omitempty"`

	// Projection is the list of output expressions.
	// Grammar: projectionClause → SELECT DISTINCT? topClause? columnExprList
	Projection []Expr `cbor:"projection"`

	// ExceptColumns lists columns excluded from projection.
	// Grammar: projectionExceptClause → EXCEPT staticOrDynamicColumnSelection
	ExceptColumns *ExceptClause `cbor:"except,omitempty"`

	// With is the inline WITH scalar expression list (ClickHouse extension).
	// Different from Query.CTEs — these are WITH expr AS name inside SELECT.
	// Grammar: withClause → WITH columnExprList
	With []Expr `cbor:"with,omitempty"`

	// From is the FROM / JOIN tree.
	// Grammar: fromClause → FROM joinExpr
	From *JoinExpr `cbor:"from,omitempty"`

	// ArrayJoin is the ARRAY JOIN clause (ClickHouse extension).
	// Grammar: arrayJoinClause → (LEFT|INNER)? ARRAY JOIN columnExprList
	ArrayJoin *ArrayJoinClause `cbor:"array_join,omitempty"`

	// WindowDef defines a named window specification.
	// Grammar: windowClause → WINDOW identifier AS (windowExpr)
	WindowDef *WindowDefClause `cbor:"window_def,omitempty"`

	// Qualify is the QUALIFY clause (window function filter).
	// Grammar: qualifyClause → QUALIFY columnExpr
	Qualify *Expr `cbor:"qualify,omitempty"`

	// Prewhere is the PREWHERE clause (ClickHouse extension).
	// Grammar: prewhereClause → PREWHERE columnExpr
	Prewhere *Expr `cbor:"prewhere,omitempty"`

	// Where is the WHERE clause.
	// Grammar: whereClause → WHERE columnExpr
	Where *Expr `cbor:"where,omitempty"`

	// GroupBy is the GROUP BY clause.
	// Grammar: groupByClause → GROUP BY ... (WITH CUBE|ROLLUP)? (WITH TOTALS)?
	GroupBy *GroupByClause `cbor:"group_by,omitempty"`

	// Having is the HAVING clause.
	// Grammar: havingClause → HAVING columnExpr
	Having *Expr `cbor:"having,omitempty"`

	// OrderBy is the ORDER BY clause.
	// Grammar: orderByClause → ORDER BY orderExprList (WITH FILL ...)?
	OrderBy *OrderByClause `cbor:"order_by,omitempty"`

	// LimitBy is the LIMIT ... BY clause (ClickHouse extension).
	// Grammar: limitByClause → LIMIT limitExpr BY columnExprList
	LimitBy *LimitByClause `cbor:"limit_by,omitempty"`

	// Limit is the LIMIT clause.
	// Grammar: limitClause → LIMIT limitExpr (WITH TIES)?
	Limit *LimitClause `cbor:"limit,omitempty"`

	// Settings are query-level SETTINGS key=value pairs.
	// Grammar: settingsClause → SETTINGS settingExprList
	Settings []SettingPair `cbor:"settings,omitempty"`
}

// ============================================================================
// Clauses
// ============================================================================

// TopClause represents TOP N [WITH TIES].
type TopClause struct {
	N        uint64 `cbor:"n"`
	WithTies bool   `cbor:"with_ties,omitempty"`
}

// ExceptClause represents EXCEPT column selection.
type ExceptClause struct {
	// Static is a list of column identifiers to exclude.
	Static []string `cbor:"static,omitempty"`

	// Dynamic is a COLUMNS('regex') pattern (empty if static).
	Dynamic string `cbor:"dynamic,omitempty"`
}

// ArrayJoinClause represents [LEFT|INNER] ARRAY JOIN expressions.
type ArrayJoinClause struct {
	// Kind is "LEFT", "INNER", or "" (default INNER).
	Kind string `cbor:"kind,omitempty"`

	// Exprs is the list of array expressions to join.
	Exprs []Expr `cbor:"exprs"`
}

// WindowDefClause represents WINDOW name AS (windowExpr).
type WindowDefClause struct {
	Name   string     `cbor:"name"`
	Window WindowSpec `cbor:"window"`
}

// GroupByClause represents GROUP BY with optional modifiers.
type GroupByClause struct {
	// Exprs is the list of GROUP BY expressions.
	Exprs []Expr `cbor:"exprs"`

	// Modifier is "CUBE", "ROLLUP", or "" (plain GROUP BY).
	// This covers both the wrapping form (GROUP BY CUBE(...)) and the
	// trailing form (GROUP BY ... WITH CUBE).
	Modifier string `cbor:"mod,omitempty"`

	// WithTotals indicates GROUP BY ... WITH TOTALS.
	WithTotals bool `cbor:"with_totals,omitempty"`
}

// OrderByClause represents ORDER BY with optional FILL.
type OrderByClause struct {
	// Items is the list of ORDER BY expressions.
	Items []OrderItem `cbor:"items"`

	// Fill is the WITH FILL specification (nil if absent).
	Fill *FillClause `cbor:"fill,omitempty"`
}

// OrderItem represents a single ORDER BY expression.
// Grammar: orderExpr → columnExpr (ASC|DESC)? (NULLS (FIRST|LAST))? (COLLATE STRING_LITERAL)?
type OrderItem struct {
	Expr    Expr   `cbor:"expr"`
	Dir     string `cbor:"dir,omitempty"`     // "ASC", "DESC", or "" (default ASC)
	Nulls   string `cbor:"nulls,omitempty"`   // "FIRST", "LAST", or ""
	Collate string `cbor:"collate,omitempty"` // collation string, or ""
}

// FillClause represents WITH FILL parameters.
type FillClause struct {
	From        *Expr             `cbor:"from,omitempty"`
	To          *Expr             `cbor:"to,omitempty"`
	Step        *Expr             `cbor:"step,omitempty"`
	Staleness   *Expr             `cbor:"staleness,omitempty"`
	Interpolate []InterpolateItem `cbor:"interpolate,omitempty"`
}

// InterpolateItem represents an INTERPOLATE expression pair.
type InterpolateItem struct {
	Expr Expr  `cbor:"expr"`
	As   *Expr `cbor:"as,omitempty"`
}

// LimitByClause represents LIMIT n BY columns.
type LimitByClause struct {
	Limit   LimitSpec `cbor:"limit"`
	Columns []Expr    `cbor:"columns"`
}

// LimitClause represents LIMIT n [WITH TIES].
type LimitClause struct {
	Limit    LimitSpec `cbor:"limit"`
	WithTies bool      `cbor:"with_ties,omitempty"`
}

// LimitSpec represents a limit expression: N or N, OFFSET or OFFSET, N.
// Grammar: limitExpr → columnExpr ((COMMA | OFFSET) columnExpr)?
type LimitSpec struct {
	Limit  Expr  `cbor:"n"`
	Offset *Expr `cbor:"offset,omitempty"`
}

// SettingPair represents key = value in SET or SETTINGS.
// Grammar: settingExpr → identifier EQ_SINGLE settingValue
type SettingPair struct {
	Key      string `cbor:"key"`
	ValueSQL string `cbor:"val"` // raw SQL text of the value
}

// CTE represents a Common Table Expression.
// Grammar: namedQuery → name (columnAliases)? AS LPAREN query RPAREN
type CTE struct {
	Name          string   `cbor:"name"`
	ColumnAliases []string `cbor:"col_aliases,omitempty"`
	Body          Query    `cbor:"body"`
}

// ============================================================================
// FROM / JOIN
// ============================================================================

// JoinExprKind discriminates JoinExpr variants.
type JoinExprKind uint8

const (
	JoinExprTable JoinExprKind = iota // leaf: table reference
	JoinExprOp                        // binary: lhs JOIN rhs
	JoinExprCross                     // binary: lhs CROSS JOIN rhs
)

// JoinExpr is a tagged union for FROM / JOIN tree nodes.
// Grammar: joinExpr (4 alternatives, parenthesized form is unwrapped)
type JoinExpr struct {
	Kind  JoinExprKind   `cbor:"k"`
	Table *JoinTableData `cbor:"table,omitempty"`
	Op    *JoinOpData    `cbor:"op,omitempty"`
	Cross *JoinCrossData `cbor:"cross,omitempty"`
}

// JoinTableData represents a leaf table reference in a JOIN tree.
// Grammar: joinExpr → tableExpr FINAL? sampleClause?
type JoinTableData struct {
	// TableKind discriminates: "ref" (identifier), "func" (table function), "subquery".
	TableKind string `cbor:"tk"`

	// Ref is set when TableKind == "ref".
	// Grammar: tableIdentifier → (databaseIdentifier DOT)? identifier
	Database string `cbor:"db,omitempty"`
	Table    string `cbor:"tbl,omitempty"`

	// Function is set when TableKind == "func".
	// Grammar: tableFunctionExpr → identifier LPAREN tableArgList? RPAREN
	FuncName string `cbor:"fn,omitempty"`
	FuncArgs []Expr `cbor:"fn_args,omitempty"`

	// Subquery is set when TableKind == "subquery".
	Subquery *SelectUnion `cbor:"sq,omitempty"`

	// Alias is the table alias (empty if not aliased).
	Alias string `cbor:"alias,omitempty"`

	// Final indicates FINAL keyword.
	Final bool `cbor:"final,omitempty"`

	// Sample is the SAMPLE clause (nil if absent).
	Sample *SampleClause `cbor:"sample,omitempty"`
}

// JoinOpData represents a binary JOIN operation.
// Grammar: joinExpr → joinExpr (GLOBAL|LOCAL)? joinOp? JOIN joinExpr joinConstraintClause
type JoinOpData struct {
	Left  JoinExpr `cbor:"left"`
	Right JoinExpr `cbor:"right"`

	// Kind encodes the join type: "INNER", "LEFT", "RIGHT", "FULL".
	Kind string `cbor:"kind"`

	// Strictness encodes: "ALL", "ANY", "SEMI", "ANTI", "ASOF", or "".
	Strictness string `cbor:"strict,omitempty"`

	// Global indicates GLOBAL keyword.
	Global bool `cbor:"global,omitempty"`

	// Local indicates LOCAL keyword.
	Local bool `cbor:"local,omitempty"`

	// Constraint is the join condition.
	Constraint JoinConstraint `cbor:"constraint"`
}

// JoinCrossData represents a CROSS JOIN.
// Grammar: joinExpr → joinExpr joinOpCross joinExpr
type JoinCrossData struct {
	Left  JoinExpr `cbor:"left"`
	Right JoinExpr `cbor:"right"`

	// Global indicates GLOBAL keyword.
	Global bool `cbor:"global,omitempty"`

	// Local indicates LOCAL keyword.
	Local bool `cbor:"local,omitempty"`
}

// JoinConstraint represents ON or USING.
// Grammar: joinConstraintClause → ON columnExprList | USING (LPAREN)? columnExprList (RPAREN)?
type JoinConstraint struct {
	// Kind is "ON" or "USING".
	Kind string `cbor:"kind"`

	// Exprs is the expression list (ON conditions or USING columns).
	Exprs []Expr `cbor:"exprs"`
}

// SampleClause represents SAMPLE ratio [OFFSET ratio].
// Grammar: sampleClause → SAMPLE ratioExpr (OFFSET ratioExpr)?
type SampleClause struct {
	Ratio  RatioExpr  `cbor:"ratio"`
	Offset *RatioExpr `cbor:"offset,omitempty"`
}

// RatioExpr represents a ratio: number or number/number.
// Grammar: ratioExpr → numberLiteral (SLASH numberLiteral)?
type RatioExpr struct {
	Numerator   string `cbor:"num"`           // number as SQL text
	Denominator string `cbor:"den,omitempty"` // number as SQL text, empty if no slash
}

// ============================================================================
// Window Specification
// ============================================================================

// WindowSpec describes a window for window functions.
// Grammar: windowExpr → winPartitionByClause? winOrderByClause? winFrameClause?
type WindowSpec struct {
	PartitionBy []Expr       `cbor:"partition,omitempty"`
	OrderBy     []OrderItem  `cbor:"order_by,omitempty"`
	Frame       *WindowFrame `cbor:"frame,omitempty"`
}

// WindowFrame describes the frame for a window specification.
// Grammar: winFrameClause → (ROWS|RANGE) winFrameExtend
type WindowFrame struct {
	// Unit is "ROWS" or "RANGE".
	Unit string `cbor:"unit"`

	// Start is the frame start bound.
	Start FrameBound `cbor:"start"`

	// End is the frame end bound (nil if not BETWEEN form).
	End *FrameBound `cbor:"end,omitempty"`
}

// FrameBound describes a window frame boundary.
// Grammar: winFrameBound → CURRENT ROW | UNBOUNDED PRECEDING | UNBOUNDED FOLLOWING
//
//	| numberLiteral PRECEDING | numberLiteral FOLLOWING
type FrameBound struct {
	// Kind is "CURRENT_ROW", "UNBOUNDED_PRECEDING", "UNBOUNDED_FOLLOWING",
	// "N_PRECEDING", or "N_FOLLOWING".
	Kind string `cbor:"kind"`

	// N is the numeric offset (only for N_PRECEDING / N_FOLLOWING).
	N string `cbor:"n,omitempty"`
}

// ============================================================================
// Expressions
// ============================================================================

// ExprKind discriminates expression variants.
type ExprKind uint8

const (
	KindLiteral      ExprKind = iota // scalar literal value
	KindParamSlot                    // {name: Type} parameter slot
	KindColumnRef                    // [db.][table.]column[.nested]
	KindFunctionCall                 // f(args) — includes all normalized sugar
	KindWindowFunc                   // f(args) OVER (spec) or f(args) OVER name
	KindBinary                       // lhs OP rhs
	KindUnary                        // OP expr (NOT, -)
	KindBetween                      // expr [NOT] BETWEEN expr AND expr
	KindIsNull                       // expr IS [NOT] NULL
	KindTernary                      // cond ? then : else
	KindCase                         // CASE ... WHEN ... THEN ... END
	KindInterval                     // INTERVAL expr unit
	KindLambda                       // (params) -> body
	KindAlias                        // expr AS name
	KindSubquery                     // (SELECT ...)
	KindAsterisk                     // [table.]* (only if schema not resolved)
	KindDynColumn                    // COLUMNS('regex') (only if schema not resolved)
)

// Expr is the tagged union for all expression types.
type Expr struct {
	Kind ExprKind `cbor:"k"`

	Literal  *LiteralData    `cbor:"lit,omitempty"`
	Param    *ParamSlotData  `cbor:"param,omitempty"`
	ColRef   *ColumnRefData  `cbor:"col,omitempty"`
	Func     *FuncCallData   `cbor:"fn,omitempty"`
	WinFunc  *WindowFuncData `cbor:"wfn,omitempty"`
	Binary   *BinaryData     `cbor:"bin,omitempty"`
	Unary    *UnaryData      `cbor:"un,omitempty"`
	Between  *BetweenData    `cbor:"btw,omitempty"`
	IsNull   *IsNullData     `cbor:"isn,omitempty"`
	Ternary  *TernaryData    `cbor:"tern,omitempty"`
	Case     *CaseData       `cbor:"case,omitempty"`
	Interval *IntervalData   `cbor:"intv,omitempty"`
	Lambda   *LambdaData     `cbor:"lam,omitempty"`
	Alias    *AliasData      `cbor:"als,omitempty"`
	Subquery *SubqueryData   `cbor:"sq,omitempty"`
	Asterisk *AsteriskData   `cbor:"star,omitempty"`
	DynCol   *DynColumnData  `cbor:"dyn,omitempty"`
}

// --- Expression data structs ---

// LiteralData holds a scalar literal value.
// After literal extraction, only short/blacklisted literals remain in the AST.
type LiteralData struct {
	// SQL is the literal text in canonical form (e.g. "'hello'", "42", "NULL", "true").
	SQL string `cbor:"s"`
}

// ParamSlotData holds a {name: Type} parameter reference.
// Grammar: paramSlot → LBRACE identifier COLON columnTypeExpr RBRACE
type ParamSlotData struct {
	Name string `cbor:"n"`
	Type string `cbor:"t"` // ClickHouse type as string (e.g. "String", "UInt64")
}

// ColumnRefData holds a column reference.
// Grammar: columnIdentifier → (tableIdentifier DOT)? nestedIdentifier
//
//	nestedIdentifier → identifier (DOT identifier)?
//
// After normalization: all identifiers double-quoted, table optionally qualified.
type ColumnRefData struct {
	Database string `cbor:"db,omitempty"`   // database qualifier (empty if unqualified)
	Table    string `cbor:"tbl,omitempty"`  // table/alias qualifier (empty if unqualified)
	Column   string `cbor:"col"`            // column name
	Nested   string `cbor:"nest,omitempty"` // nested field (empty if not nested)
}

// FuncCallData holds a function call.
// This covers regular functions AND all syntactic sugar normalized to function form:
// CAST, array, tuple, arrayElement, tupleElement, toDate, toDateTime,
// extract, substring, trimBoth, trimLeading, trimTrailing.
//
// Grammar: identifier (LPAREN columnExprList? RPAREN)? LPAREN DISTINCT? columnArgList? RPAREN
// The grammar supports parametric functions: f(params)(args).
type FuncCallData struct {
	// Name is the function name (lowercased after normalization).
	// Exception: "CAST" stays uppercase to distinguish from user functions.
	Name string `cbor:"n"`

	// Args is the argument list.
	Args []Expr `cbor:"a,omitempty"`

	// Params is the parametric function parameter list (e.g. quantile(0.9)(x)).
	// Empty for non-parametric functions.
	Params []Expr `cbor:"p,omitempty"`

	// Distinct indicates DISTINCT keyword in arguments (e.g. count(DISTINCT x)).
	Distinct bool `cbor:"distinct,omitempty"`
}

// WindowFuncData holds a window function call.
// Grammar: identifier (LPAREN columnExprList? RPAREN) OVER (LPAREN windowExpr RPAREN | identifier)
type WindowFuncData struct {
	// Name is the function name.
	Name string `cbor:"n"`

	// Args is the argument list.
	Args []Expr `cbor:"a,omitempty"`

	// Params is the parametric function parameter list.
	Params []Expr `cbor:"p,omitempty"`

	// Window is the inline window specification (nil if using WindowRef).
	Window *WindowSpec `cbor:"w,omitempty"`

	// WindowRef is the name of a referenced WINDOW clause (empty if inline).
	WindowRef string `cbor:"wr,omitempty"`
}

// BinaryData holds a binary operation.
// Op values: "AND", "OR", "+", "-", "*", "/", "%", "||",
//
//	"=", "!=", "<", ">", "<=", ">=",
//	"IN", "NOT IN", "GLOBAL IN", "GLOBAL NOT IN",
//	"LIKE", "NOT LIKE", "ILIKE", "NOT ILIKE"
type BinaryData struct {
	Op    string `cbor:"op"`
	Left  Expr   `cbor:"l"`
	Right Expr   `cbor:"r"`
}

// UnaryData holds a unary operation.
// Op values: "NOT", "-"
type UnaryData struct {
	Op   string `cbor:"op"`
	Expr Expr   `cbor:"e"`
}

// BetweenData holds a [NOT] BETWEEN expression.
// Grammar: columnExpr NOT? BETWEEN columnExpr AND columnExpr
type BetweenData struct {
	Expr   Expr `cbor:"e"`
	Low    Expr `cbor:"lo"`
	High   Expr `cbor:"hi"`
	Negate bool `cbor:"neg,omitempty"` // true for NOT BETWEEN
}

// IsNullData holds an IS [NOT] NULL expression.
// Grammar: columnExpr IS NOT? NULL_SQL
type IsNullData struct {
	Expr   Expr `cbor:"e"`
	Negate bool `cbor:"neg,omitempty"` // true for IS NOT NULL
}

// TernaryData holds a ternary (conditional) expression.
// Grammar: columnExpr QUERY columnExpr COLON columnExpr
type TernaryData struct {
	Cond Expr `cbor:"c"`
	Then Expr `cbor:"t"`
	Else Expr `cbor:"e"`
}

// CaseData holds a CASE expression.
// Grammar: CASE columnExpr? (WHEN columnExpr THEN columnExpr)+ (ELSE columnExpr)? END
type CaseData struct {
	// Operand is the CASE operand (nil for searched CASE: CASE WHEN ...).
	Operand *Expr      `cbor:"op,omitempty"`
	Whens   []CaseWhen `cbor:"whens"`
	Else    *Expr      `cbor:"else,omitempty"`
}

// CaseWhen holds a single WHEN ... THEN ... branch.
type CaseWhen struct {
	When Expr `cbor:"w"`
	Then Expr `cbor:"t"`
}

// IntervalData holds an INTERVAL expression.
// Grammar: INTERVAL columnExpr interval
// interval: SECOND | MINUTE | HOUR | DAY | WEEK | MONTH | QUARTER | YEAR
type IntervalData struct {
	Value Expr   `cbor:"v"`
	Unit  string `cbor:"u"` // "SECOND", "MINUTE", "HOUR", "DAY", "WEEK", "MONTH", "QUARTER", "YEAR"
}

// LambdaData holds a lambda expression.
// Grammar: columnLambdaExpr → (LPAREN identifier (COMMA identifier)* RPAREN | identifier (COMMA identifier)*) ARROW columnExpr
type LambdaData struct {
	Params []string `cbor:"p"`
	Body   Expr     `cbor:"b"`
}

// AliasData holds an aliased expression.
// Grammar: columnExpr (alias | AS identifier)
type AliasData struct {
	Expr Expr   `cbor:"e"`
	Name string `cbor:"n"`
}

// SubqueryData holds a scalar subquery expression.
// Grammar: LPAREN selectUnionStmt RPAREN
type SubqueryData struct {
	Query SelectUnion `cbor:"q"`
}

// AsteriskData holds a [table.]* expression.
// Only present when schema resolver is not available.
// Grammar: (tableIdentifier DOT)? ASTERISK
type AsteriskData struct {
	Table string `cbor:"tbl,omitempty"` // table qualifier, empty for bare *
}

// DynColumnData holds a COLUMNS('regex') expression.
// Only present when schema resolver is not available.
// Grammar: dynamicColumnSelection → COLUMNS LPAREN STRING_LITERAL RPAREN
type DynColumnData struct {
	Pattern string `cbor:"pat"`
}

// ============================================================================
// Type Expressions
// ============================================================================

// TypeExpr represents a ClickHouse type.
// Grammar: columnTypeExpr (5 alternatives)
type TypeExpr struct {
	// Name is the type name (e.g. "UInt64", "Array", "Tuple", "Enum8", "FixedString").
	Name string `cbor:"n"`

	// Args is the list of type arguments (e.g. Array(UInt64) → Args: [{Name:"UInt64"}]).
	// Empty for simple types.
	Args []TypeExpr `cbor:"a,omitempty"`

	// EnumValues holds enum definitions (for Enum8/Enum16 types).
	EnumValues []EnumValue `cbor:"ev,omitempty"`

	// NamedFields holds named fields (for Nested type).
	NamedFields []NamedField `cbor:"nf,omitempty"`

	// Params holds expression parameters (e.g. FixedString(N) → Params: [N]).
	Params []Expr `cbor:"p,omitempty"`
}

// EnumValue represents a single enum entry: 'name' = number.
type EnumValue struct {
	Name  string `cbor:"n"`
	Value string `cbor:"v"` // number as string
}

// NamedField represents a named field in Nested type: name Type.
type NamedField struct {
	Name string   `cbor:"n"`
	Type TypeExpr `cbor:"t"`
}
