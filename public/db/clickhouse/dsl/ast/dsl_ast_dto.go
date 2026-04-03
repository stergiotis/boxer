//go:build llm_generated_opus46

package ast

// ============================================================================
// Top-level
// ============================================================================

type Query struct {
	Settings []SettingPair `cbor:"settings,omitempty"`
	CTEs     []CTE         `cbor:"ctes,omitempty"`
	Body     SelectUnion   `cbor:"body"`
	Format   string        `cbor:"format,omitempty"`
}

// ============================================================================
// SELECT / UNION
// ============================================================================

type SelectUnion struct {
	Head  Select            `cbor:"head"`
	Items []SelectUnionItem `cbor:"items,omitempty"`
}

// SelectUnionItem is a single set operation in a UNION chain.
// The Body is a SelectUnion (not a bare Select) because parenthesized
// UNION groups are valid: SELECT 1 UNION ALL (SELECT 2 UNION ALL SELECT 3).
type SelectUnionItem struct {
	Op       UnionOpE    `cbor:"op"`
	Modifier UnionModE   `cbor:"mod,omitempty"`
	Body     SelectUnion `cbor:"body"`
}

type Select struct {
	Distinct      bool             `cbor:"distinct,omitempty"`
	Top           *TopClause       `cbor:"top,omitempty"`
	Projection    []Expr           `cbor:"projection"`
	ExceptColumns *ExceptClause    `cbor:"except,omitempty"`
	With          []Expr           `cbor:"with,omitempty"`
	From          *JoinExpr        `cbor:"from,omitempty"`
	ArrayJoin     *ArrayJoinClause `cbor:"array_join,omitempty"`
	WindowDef     *WindowDefClause `cbor:"window_def,omitempty"`
	Qualify       *Expr            `cbor:"qualify,omitempty"`
	Prewhere      *Expr            `cbor:"prewhere,omitempty"`
	Where         *Expr            `cbor:"where,omitempty"`
	GroupBy       *GroupByClause   `cbor:"group_by,omitempty"`
	Having        *Expr            `cbor:"having,omitempty"`
	OrderBy       *OrderByClause   `cbor:"order_by,omitempty"`
	LimitBy       *LimitByClause   `cbor:"limit_by,omitempty"`
	Limit         *LimitClause     `cbor:"limit,omitempty"`
	Settings      []SettingPair    `cbor:"settings,omitempty"`
}

// ============================================================================
// Clauses
// ============================================================================

type TopClause struct {
	N        uint64 `cbor:"n"`
	WithTies bool   `cbor:"with_ties,omitempty"`
}

type ExceptClause struct {
	Static  []string `cbor:"static,omitempty"`
	Dynamic string   `cbor:"dynamic,omitempty"`
}

type ArrayJoinClause struct {
	Kind  ArrayJoinKindE `cbor:"kind,omitempty"`
	Exprs []Expr         `cbor:"exprs"`
}

type WindowDefClause struct {
	Name   string     `cbor:"name"`
	Window WindowSpec `cbor:"window"`
}

type GroupByClause struct {
	Exprs      []Expr      `cbor:"exprs"`
	Modifier   GroupByModE `cbor:"mod,omitempty"`
	WithTotals bool        `cbor:"with_totals,omitempty"`
}

type OrderByClause struct {
	Items []OrderItem `cbor:"items"`
}

type OrderItem struct {
	Expr    Expr        `cbor:"expr"`
	Dir     OrderDirE   `cbor:"dir,omitempty"`
	Nulls   OrderNullsE `cbor:"nulls,omitempty"`
	Collate string      `cbor:"collate,omitempty"`
}

type LimitByClause struct {
	Limit   LimitSpec `cbor:"limit"`
	Columns []Expr    `cbor:"columns"`
}

type LimitClause struct {
	Limit    LimitSpec `cbor:"limit"`
	WithTies bool      `cbor:"with_ties,omitempty"`
}

type LimitSpec struct {
	Limit  Expr  `cbor:"n"`
	Offset *Expr `cbor:"offset,omitempty"`
}

type SettingPair struct {
	Key      string `cbor:"key"`
	ValueSQL string `cbor:"val"`
}

type CTE struct {
	Name          string   `cbor:"name"`
	ColumnAliases []string `cbor:"col_aliases,omitempty"`
	Body          Query    `cbor:"body"`
}

// ============================================================================
// FROM / JOIN
// ============================================================================

type JoinExpr struct {
	Kind  JoinExprKindE  `cbor:"k"`
	Table *JoinTableData `cbor:"table,omitempty"`
	Op    *JoinOpData    `cbor:"op,omitempty"`
	Cross *JoinCrossData `cbor:"cross,omitempty"`
}

type JoinTableData struct {
	TableKind TableKindE    `cbor:"tk"`
	Database  string        `cbor:"db,omitempty"`
	Table     string        `cbor:"tbl,omitempty"`
	FuncName  string        `cbor:"fn,omitempty"`
	FuncArgs  []Expr        `cbor:"fn_args,omitempty"`
	Subquery  *SelectUnion  `cbor:"sq,omitempty"`
	Alias     string        `cbor:"alias,omitempty"`
	Final     bool          `cbor:"final,omitempty"`
	Sample    *SampleClause `cbor:"sample,omitempty"`
}

type JoinOpData struct {
	Left       JoinExpr        `cbor:"left"`
	Right      JoinExpr        `cbor:"right"`
	Kind       JoinKindE       `cbor:"kind"`
	Strictness JoinStrictnessE `cbor:"strict,omitempty"`
	Global     bool            `cbor:"global,omitempty"`
	Local      bool            `cbor:"local,omitempty"`
	Constraint JoinConstraint  `cbor:"constraint"`
}

type JoinCrossData struct {
	Left   JoinExpr `cbor:"left"`
	Right  JoinExpr `cbor:"right"`
	Global bool     `cbor:"global,omitempty"`
	Local  bool     `cbor:"local,omitempty"`
}

type JoinConstraint struct {
	Kind  JoinConstraintKindE `cbor:"kind"`
	Exprs []Expr              `cbor:"exprs"`
}

type SampleClause struct {
	Ratio  RatioExpr  `cbor:"ratio"`
	Offset *RatioExpr `cbor:"offset,omitempty"`
}

type RatioExpr struct {
	Numerator   string `cbor:"num"`
	Denominator string `cbor:"den,omitempty"`
}

// ============================================================================
// Window
// ============================================================================

type WindowSpec struct {
	PartitionBy []Expr       `cbor:"partition,omitempty"`
	OrderBy     []OrderItem  `cbor:"order_by,omitempty"`
	Frame       *WindowFrame `cbor:"frame,omitempty"`
}

type WindowFrame struct {
	Unit  FrameUnitE  `cbor:"unit"`
	Start FrameBound  `cbor:"start"`
	End   *FrameBound `cbor:"end,omitempty"`
}

type FrameBound struct {
	Kind FrameBoundKindE `cbor:"kind"`
	N    string          `cbor:"n,omitempty"`
}

// ============================================================================
// Expressions
// ============================================================================

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
	Interval *IntervalData   `cbor:"intv,omitempty"`
	Lambda   *LambdaData     `cbor:"lam,omitempty"`
	Alias    *AliasData      `cbor:"als,omitempty"`
	Subquery *SubqueryData   `cbor:"sq,omitempty"`
	Asterisk *AsteriskData   `cbor:"star,omitempty"`
	DynCol   *DynColumnData  `cbor:"dyn,omitempty"`
}

type LiteralData struct {
	SQL string `cbor:"s"`
}

type ParamSlotData struct {
	Name string `cbor:"n"`
	Type string `cbor:"t"`
}

type ColumnRefData struct {
	Database string `cbor:"db,omitempty"`
	Table    string `cbor:"tbl,omitempty"`
	Column   string `cbor:"col"`
	Nested   string `cbor:"nest,omitempty"`
}

type FuncCallData struct {
	Name     string `cbor:"n"`
	Args     []Expr `cbor:"a,omitempty"`
	Params   []Expr `cbor:"p,omitempty"`
	Distinct bool   `cbor:"distinct,omitempty"`
}

type WindowFuncData struct {
	Name      string      `cbor:"n"`
	Args      []Expr      `cbor:"a,omitempty"`
	Params    []Expr      `cbor:"p,omitempty"`
	Window    *WindowSpec `cbor:"w,omitempty"`
	WindowRef string      `cbor:"wr,omitempty"`
}

type BinaryData struct {
	Op    BinaryOpE `cbor:"o"`
	Left  Expr      `cbor:"l"`
	Right Expr      `cbor:"r"`
}

type UnaryData struct {
	Op   UnaryOpE `cbor:"o"`
	Expr Expr     `cbor:"e"`
}

type BetweenData struct {
	Expr   Expr `cbor:"e"`
	Low    Expr `cbor:"lo"`
	High   Expr `cbor:"hi"`
	Negate bool `cbor:"neg,omitempty"`
}

type IsNullData struct {
	Expr   Expr `cbor:"e"`
	Negate bool `cbor:"neg,omitempty"`
}

type IntervalData struct {
	Value Expr          `cbor:"v"`
	Unit  IntervalUnitE `cbor:"u"`
}

type LambdaData struct {
	Params []string `cbor:"p"`
	Body   Expr     `cbor:"b"`
}

type AliasData struct {
	Expr Expr   `cbor:"e"`
	Name string `cbor:"n"`
}

type SubqueryData struct {
	Query SelectUnion `cbor:"q"`
}

type AsteriskData struct {
	Table string `cbor:"tbl,omitempty"`
}

type DynColumnData struct {
	Pattern string `cbor:"pat"`
}
