//go:build llm_generated_opus46

package ast

import "fmt"

type ExprKind uint8

const (
	KindLiteral      ExprKind = iota // 42, 'hello', NULL, true, false
	KindParamSlot                    // {name: Type}
	KindColumnRef                    // "db"."table"."column"."nested"
	KindFunctionCall                 // f(args) — includes all normalized sugar
	KindWindowFunc                   // f(args) OVER (spec | name)
	KindBinary                       // lhs OP rhs
	KindUnary                        // NOT expr, -expr
	KindBetween                      // expr [NOT] BETWEEN expr AND expr
	KindIsNull                       // expr IS [NOT] NULL
	KindInterval                     // INTERVAL expr unit
	KindLambda                       // (params) -> body
	KindAlias                        // expr AS name
	KindSubquery                     // (SELECT ...)
	KindAsterisk                     // [table.]*
	KindDynColumn                    // COLUMNS('regex')
)

var _exprKindNames = [...]string{
	KindLiteral: "Literal", KindParamSlot: "ParamSlot", KindColumnRef: "ColumnRef",
	KindFunctionCall: "FunctionCall", KindWindowFunc: "WindowFunc",
	KindBinary: "Binary", KindUnary: "Unary", KindBetween: "Between",
	KindIsNull: "IsNull", KindInterval: "Interval", KindLambda: "Lambda",
	KindAlias: "Alias", KindSubquery: "Subquery", KindAsterisk: "Asterisk",
	KindDynColumn: "DynColumn",
}

func (inst ExprKind) String() string {
	if int(inst) < len(_exprKindNames) {
		return _exprKindNames[inst]
	}
	return fmt.Sprintf("ExprKind(%d)", inst)
}

type JoinExprKindE uint8

const (
	JoinExprTable JoinExprKindE = iota
	JoinExprOp
	JoinExprCross
)

type TableKindE uint8

const (
	TableKindRef TableKindE = iota
	TableKindFunc
	TableKindSubquery
)

type JoinKindE uint8

const (
	JoinKindInner JoinKindE = iota
	JoinKindLeft
	JoinKindRight
	JoinKindFull
)

type JoinStrictnessE uint8

const (
	JoinStrictnessNone JoinStrictnessE = iota
	JoinStrictnessAll
	JoinStrictnessAny
	JoinStrictnessSemi
	JoinStrictnessAnti
	JoinStrictnessAsof
)

type JoinConstraintKindE uint8

const (
	JoinConstraintOn JoinConstraintKindE = iota
	JoinConstraintUsing
)

type OrderDirE uint8

const (
	OrderDirDefault OrderDirE = iota
	OrderDirAsc
	OrderDirDesc
)

type OrderNullsE uint8

const (
	OrderNullsDefault OrderNullsE = iota
	OrderNullsFirst
	OrderNullsLast
)

type FrameUnitE uint8

const (
	FrameUnitRows FrameUnitE = iota
	FrameUnitRange
)

type FrameBoundKindE uint8

const (
	FrameBoundCurrentRow FrameBoundKindE = iota
	FrameBoundUnboundedPreceding
	FrameBoundUnboundedFollowing
	FrameBoundNPreceding
	FrameBoundNFollowing
)

type UnionOpE uint8

const (
	UnionOpUnion UnionOpE = iota
	UnionOpExcept
	UnionOpIntersect
)

type UnionModE uint8

const (
	UnionModDefault UnionModE = iota
	UnionModAll
	UnionModDistinct
)

type GroupByModE uint8

const (
	GroupByModNone GroupByModE = iota
	GroupByModCube
	GroupByModRollup
)

type ArrayJoinKindE uint8

const (
	ArrayJoinDefault ArrayJoinKindE = iota
	ArrayJoinLeft
	ArrayJoinInner
)

type BinaryOpE uint8

const (
	BinOpAnd BinaryOpE = iota
	BinOpOr
	BinOpPlus
	BinOpMinus
	BinOpMultiply
	BinOpDivide
	BinOpModulo
	BinOpConcat
	BinOpEq
	BinOpNotEq
	BinOpLt
	BinOpGt
	BinOpLe
	BinOpGe
	BinOpIn
	BinOpNotIn
	BinOpGlobalIn
	BinOpGlobalNotIn
	BinOpLike
	BinOpNotLike
	BinOpILike
	BinOpNotILike
)

type UnaryOpE uint8

const (
	UnaryOpNot UnaryOpE = iota
	UnaryOpNegate
)

type IntervalUnitE uint8

const (
	IntervalSecond IntervalUnitE = iota
	IntervalMinute
	IntervalHour
	IntervalDay
	IntervalWeek
	IntervalMonth
	IntervalQuarter
	IntervalYear
)

func ParseIntervalUnit(s string) (unit IntervalUnitE, err error) {
	switch s {
	case "SECOND":
		unit = IntervalSecond
	case "MINUTE":
		unit = IntervalMinute
	case "HOUR":
		unit = IntervalHour
	case "DAY":
		unit = IntervalDay
	case "WEEK":
		unit = IntervalWeek
	case "MONTH":
		unit = IntervalMonth
	case "QUARTER":
		unit = IntervalQuarter
	case "YEAR":
		unit = IntervalYear
	default:
		err = fmt.Errorf("unknown interval unit %q", s)
	}
	return
}
