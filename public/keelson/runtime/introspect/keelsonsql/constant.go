package keelsonsql

import (
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ScalarTypeE is the ClickHouse type of an inline constant: the types a
// literal, a {slot:Type} parameter or a values() structure can give one
// (ADR-0290 §SD3). ScalarTypeNothing is NULL's type, which no column takes.
type ScalarTypeE uint8

const (
	ScalarTypeNothing ScalarTypeE = iota
	ScalarTypeUInt8
	ScalarTypeUInt16
	ScalarTypeUInt32
	ScalarTypeUInt64
	ScalarTypeInt8
	ScalarTypeInt16
	ScalarTypeInt32
	ScalarTypeInt64
	ScalarTypeFloat32
	ScalarTypeFloat64
	ScalarTypeString
	ScalarTypeBool
)

var scalarTypeNames = [...]string{"Nothing", "UInt8", "UInt16", "UInt32", "UInt64", "Int8", "Int16", "Int32", "Int64", "Float32", "Float64", "String", "Bool"}

func (inst ScalarTypeE) String() string {
	if int(inst) < len(scalarTypeNames) {
		return scalarTypeNames[inst]
	}
	return "unknown"
}

// ParseScalarType reads a ClickHouse type name, spelled as ClickHouse
// spells it; a type outside ScalarTypeE, or an alias, is not one.
func ParseScalarType(name string) (t ScalarTypeE, ok bool) {
	for i := ScalarTypeUInt8; int(i) < len(scalarTypeNames); i++ {
		if scalarTypeNames[i] == name {
			return i, true
		}
	}
	return ScalarTypeNothing, false
}

// IsUnsigned reports a UInt type.
func (inst ScalarTypeE) IsUnsigned() bool {
	return inst >= ScalarTypeUInt8 && inst <= ScalarTypeUInt64
}

// IsSigned reports an Int type.
func (inst ScalarTypeE) IsSigned() bool { return inst >= ScalarTypeInt8 && inst <= ScalarTypeInt64 }

// IsFloat reports a Float type.
func (inst ScalarTypeE) IsFloat() bool { return inst == ScalarTypeFloat32 || inst == ScalarTypeFloat64 }

// Bits is an integer or float type's width; zero for the others.
func (inst ScalarTypeE) Bits() int {
	switch inst {
	case ScalarTypeUInt8, ScalarTypeInt8:
		return 8
	case ScalarTypeUInt16, ScalarTypeInt16:
		return 16
	case ScalarTypeUInt32, ScalarTypeInt32, ScalarTypeFloat32:
		return 32
	case ScalarTypeUInt64, ScalarTypeInt64, ScalarTypeFloat64:
		return 64
	}
	return 0
}

// Arrow is the Arrow type a column of t is built as.
func (inst ScalarTypeE) Arrow() arrow.DataType {
	switch inst {
	case ScalarTypeUInt8:
		return arrow.PrimitiveTypes.Uint8
	case ScalarTypeUInt16:
		return arrow.PrimitiveTypes.Uint16
	case ScalarTypeUInt32:
		return arrow.PrimitiveTypes.Uint32
	case ScalarTypeUInt64:
		return arrow.PrimitiveTypes.Uint64
	case ScalarTypeInt8:
		return arrow.PrimitiveTypes.Int8
	case ScalarTypeInt16:
		return arrow.PrimitiveTypes.Int16
	case ScalarTypeInt32:
		return arrow.PrimitiveTypes.Int32
	case ScalarTypeInt64:
		return arrow.PrimitiveTypes.Int64
	case ScalarTypeFloat32:
		return arrow.PrimitiveTypes.Float32
	case ScalarTypeFloat64:
		return arrow.PrimitiveTypes.Float64
	case ScalarTypeString:
		return arrow.BinaryTypes.String
	case ScalarTypeBool:
		return arrow.FixedWidthTypes.Boolean
	}
	return arrow.Null
}

// UnsignedFor is the smallest UInt type holding u, as ClickHouse types an
// unsigned integer literal.
func UnsignedFor(u uint64) ScalarTypeE {
	switch {
	case u <= math.MaxUint8:
		return ScalarTypeUInt8
	case u <= math.MaxUint16:
		return ScalarTypeUInt16
	case u <= math.MaxUint32:
		return ScalarTypeUInt32
	}
	return ScalarTypeUInt64
}

// SignedFor is the smallest Int type holding i.
func SignedFor(i int64) ScalarTypeE {
	switch {
	case i >= math.MinInt8 && i <= math.MaxInt8:
		return ScalarTypeInt8
	case i >= math.MinInt16 && i <= math.MaxInt16:
		return ScalarTypeInt16
	case i >= math.MinInt32 && i <= math.MaxInt32:
		return ScalarTypeInt32
	}
	return ScalarTypeInt64
}

// ConstOriginE is where a constant's value was spelled, which decides its
// default column name.
type ConstOriginE uint8

const (
	// ConstOriginLiteral is a literal: named after its value.
	ConstOriginLiteral ConstOriginE = iota
	// ConstOriginParam is a {slot:Type} parameter, which ClickHouse names
	// _CAST(…); a projection of one needs an alias here.
	ConstOriginParam
	// ConstOriginWith is a WITH constant, named after its alias (Ref).
	ConstOriginWith
)

// Constant is one inline value with its ClickHouse type: a literal, a
// parameter, true or false, or a WITH alias of one. Only the field of its
// type is set — Uint for UInt*, Int for Int*, Float for Float*, Str and
// Bool — and none when Null.
type Constant struct {
	Type   ScalarTypeE
	Null   bool
	Uint   uint64
	Int    int64
	Float  float64
	Str    string
	Bool   bool
	Origin ConstOriginE
	// Ref is the WITH alias of a ConstOriginWith constant.
	Ref string
}

// Text is the constant as a keelson() argument's value text, which
// introspect.ResolveArgs reads by the argument's declared type.
func (inst Constant) Text() (text string, err error) {
	switch {
	case inst.Null:
		return "", eh.Errorf("keelsonsql: a keelson() argument cannot be NULL; leave it out")
	case inst.Type.IsUnsigned():
		return strconv.FormatUint(inst.Uint, 10), nil
	case inst.Type.IsSigned():
		return strconv.FormatInt(inst.Int, 10), nil
	case inst.Type.IsFloat():
		return strconv.FormatFloat(inst.Float, 'g', -1, 64), nil
	case inst.Type == ScalarTypeBool:
		return strconv.FormatBool(inst.Bool), nil
	}
	return inst.Str, nil
}

// ErrNotConstant is the error of an expression that is not an inline
// constant — a function call, a column, an operator: something a server
// evaluates.
var ErrNotConstant = eh.Errorf("keelsonsql: not a constant")

// NotConstantError names the expression that is not a constant. It is
// ErrNotConstant to errors.Is.
type NotConstantError struct {
	Found string
}

func (inst *NotConstantError) Error() string {
	return ErrNotConstant.Error() + ": " + inst.Found
}

func (inst *NotConstantError) Is(target error) bool { return target == ErrNotConstant }

func notConstant(found string) error { return &NotConstantError{Found: found} }

// ConstScope is what a constant expression may name: the run's parameters,
// folded with the statement's SET prelude (PreludeParams), and its WITH
// constants.
type ConstScope struct {
	Params map[string]string
	// opaque are parameters a SET bound to a value not modelled here.
	opaque map[string]struct{}
	with   map[string]Constant
	// notConst are WITH aliases of expressions that are not constants.
	notConst map[string]struct{}
}

// NewConstScope reads the statement pr parsed into a scope over params: its
// `SET param_` prelude folded in (PreludeParams), then its WITH constants —
// `WITH <constant> AS name`, each able to name the ones before it. A WITH
// item that is not a constant is no error here; naming it where a constant
// is needed is.
func NewConstScope(pr *nanopass.ParseResult, params map[string]string) (scope *ConstScope, err error) {
	scope = &ConstScope{}
	scope.Params, scope.opaque, err = PreludeParams(TopLevelSets(pr), params)
	if err != nil {
		return nil, err
	}
	qs, ok := pr.Tree.(*grammar1.QueryStmtContext)
	if !ok {
		return
	}
	var su grammar1.ISelectUnionStmtContext
	if q, isQuery := qs.Query().(*grammar1.QueryContext); isQuery && q != nil {
		su = q.SelectUnionStmt()
	} else if ins := pr.InsertStmt(); ins != nil {
		su = ins.SelectUnionStmt()
	}
	if su == nil {
		return
	}
	err = scope.AddWith(su.(*grammar1.SelectUnionStmtContext).Ctes())
	return
}

// AddWith adds the constant items of a WITH list to the scope.
func (inst *ConstScope) AddWith(ctes grammar1.ICtesContext) (err error) {
	c, ok := ctes.(*grammar1.CtesContext)
	if !ok || c == nil {
		return nil
	}
	for _, wi := range c.AllWithItem() {
		item, isExpr := wi.(*grammar1.WithItemColumnsExprContext)
		if !isExpr {
			continue
		}
		col, isCol := item.ColumnsExpr().(*grammar1.ColumnsExprColumnContext)
		if !isCol {
			continue
		}
		expr, name, aliased := Aliased(col.ColumnExpr())
		if !aliased {
			continue
		}
		_, isConst := inst.with[name]
		_, isOther := inst.notConst[name]
		if isConst || isOther {
			// ClickHouse: MULTIPLE_EXPRESSIONS_FOR_ALIAS.
			return eb.Build().Str("with", name).Errorf("keelsonsql: WITH names an expression twice")
		}
		v, evalErr := EvalConstant(expr, inst)
		switch {
		case evalErr == nil:
			v.Origin, v.Ref = ConstOriginWith, name
			if inst.with == nil {
				inst.with = make(map[string]Constant)
			}
			inst.with[name] = v
		case IsNotConstant(evalErr):
			if inst.notConst == nil {
				inst.notConst = make(map[string]struct{})
			}
			inst.notConst[name] = struct{}{}
		default:
			return eb.Build().Str("with", name).Errorf("%w", evalErr)
		}
	}
	return nil
}

// NotConstants are the aliases of WITH expressions that are not constants.
func (inst *ConstScope) NotConstants() (names []string) {
	for n := range inst.notConst {
		names = append(names, n)
	}
	slices.Sort(names)
	return
}

// IsWithName reports that a WITH expression, constant or not, has the alias
// name.
func (inst *ConstScope) IsWithName(name string) bool {
	_, isConst := inst.with[name]
	_, isOther := inst.notConst[name]
	return isConst || isOther
}

// IsNotConstant reports err is, or wraps, a NotConstantError.
func IsNotConstant(err error) bool {
	var nc *NotConstantError
	return errors.As(err, &nc)
}

// Aliased splits `expr AS name`; aliased is false for an expression without
// one. An alias without AS (`expr name`) is not split, so it is no constant:
// the grammar reads 0b11 and 1_000 as a number and such an alias (b11,
// _000), which ClickHouse reads as one number, and only AS keeps the two
// apart.
func Aliased(e grammar1.IColumnExprContext) (expr grammar1.IColumnExprContext, name string, aliased bool) {
	a, ok := e.(*grammar1.ColumnExprAliasContext)
	if !ok || a.AS() == nil || a.Identifier() == nil {
		return e, "", false
	}
	return a.ColumnExpr(), nanopass.DecodeIdentifier(a.Identifier().GetText()), true
}

// IsBareAlias reports `expr name`, an alias without AS.
func IsBareAlias(e grammar1.IColumnExprContext) bool {
	a, ok := e.(*grammar1.ColumnExprAliasContext)
	return ok && a.AS() == nil
}

// EvalConstant evaluates e, which must be an inline constant: a number or
// string literal, NULL, true or false, a {slot:Type} parameter, a negated
// number, a parenthesised constant, or the name of a WITH constant. A
// literal is typed as ClickHouse types it: an integer as the smallest UInt
// holding it, or Int when negative, anything with a point or an exponent as
// Float64. Anything else is ErrNotConstant; a parameter that is not bound
// or does not read as its type is an error of the statement.
func EvalConstant(e grammar1.IColumnExprContext, scope *ConstScope) (c Constant, err error) {
	switch v := e.(type) {
	case *grammar1.ColumnExprLiteralContext:
		return literalConstant(v.Literal())
	case *grammar1.ColumnExprParensContext:
		return EvalConstant(v.ColumnExpr(), scope)
	case *grammar1.ColumnExprNegateContext:
		lit, isLit := v.ColumnExpr().(*grammar1.ColumnExprLiteralContext)
		if !isLit || lit.Literal().NumberLiteral() == nil {
			return c, notConstant("-" + v.ColumnExpr().GetText())
		}
		return numberConstant("-" + lit.Literal().GetText())
	case *grammar1.ColumnExprParamSlotContext:
		return paramConstant(v.ParamSlot().(*grammar1.ParamSlotContext), scope)
	case *grammar1.ColumnExprIdentifierContext:
		return identConstant(nanopass.DecodeIdentifier(v.GetText()), scope)
	}
	return c, notConstant(e.GetText())
}

// EvalTableArg evaluates a table-function argument as EvalConstant does.
func EvalTableArg(a grammar1.ITableArgExprContext, scope *ConstScope) (c Constant, err error) {
	switch {
	case a.Literal() != nil:
		return literalConstant(a.Literal())
	case a.NestedIdentifier() != nil:
		return identConstant(nanopass.DecodeIdentifier(a.NestedIdentifier().GetText()), scope)
	case a.ColumnExpr() != nil:
		return EvalConstant(a.ColumnExpr(), scope)
	}
	return c, notConstant(a.GetText())
}

func identConstant(name string, scope *ConstScope) (c Constant, err error) {
	switch {
	case strings.EqualFold(name, "true"):
		return Constant{Type: ScalarTypeBool, Bool: true}, nil
	case strings.EqualFold(name, "false"):
		return Constant{Type: ScalarTypeBool}, nil
	}
	if scope != nil {
		if v, ok := scope.with[name]; ok {
			return v, nil
		}
		if _, ok := scope.notConst[name]; ok {
			return c, notConstant("the WITH item " + name + ", which is not a constant")
		}
	}
	return c, notConstant("a column reference (" + name + ")")
}

func literalConstant(lit grammar1.ILiteralContext) (c Constant, err error) {
	switch {
	case lit.NumberLiteral() != nil:
		return numberConstant(lit.GetText())
	case lit.STRING_LITERAL() != nil:
		str, uErr := unquoteString(lit.GetText())
		if uErr != nil {
			return c, uErr
		}
		return Constant{Type: ScalarTypeString, Str: str}, nil
	}
	return Constant{Null: true}, nil
}

// numberConstant types a number literal's text, sign included.
func numberConstant(text string) (c Constant, err error) {
	neg := false
	body := text
	switch {
	case strings.HasPrefix(body, "-"):
		neg, body = true, body[1:]
	case strings.HasPrefix(body, "+"):
		body = body[1:]
	}
	lower := strings.ToLower(body)
	if strings.HasPrefix(body, ".") {
		return c, notConstant("the number " + text + ", which starts with a point")
	}
	switch {
	case lower == "inf" || lower == "nan":
		c = Constant{Type: ScalarTypeFloat64, Float: math.Inf(1)}
		if lower == "nan" {
			c.Float = math.NaN()
		}
	case strings.HasPrefix(lower, "0x"):
		u, pErr := strconv.ParseUint(lower[2:], 16, 64)
		if pErr != nil {
			return c, notConstant("the number " + text + ", which does not fit 64 bits")
		}
		c = Constant{Type: UnsignedFor(u), Uint: u}
	case strings.ContainsAny(lower, ".e"):
		f, pErr := strconv.ParseFloat(body, 64)
		if pErr != nil {
			return c, notConstant("the number " + text)
		}
		c = Constant{Type: ScalarTypeFloat64, Float: f}
	default:
		u, pErr := strconv.ParseUint(body, 10, 64)
		if pErr != nil {
			return c, notConstant("the number " + text + ", which does not fit 64 bits")
		}
		c = Constant{Type: UnsignedFor(u), Uint: u}
	}
	if neg {
		return negate(c, text)
	}
	return c, nil
}

// negate is ClickHouse's negative literal: -0 stays UInt8 0, and a negative
// integer takes the smallest Int type holding it.
func negate(c Constant, text string) (out Constant, err error) {
	switch {
	case c.Type == ScalarTypeFloat64:
		c.Float = -c.Float
		return c, nil
	case c.Uint == 0:
		return c, nil
	case c.Uint > 1<<63:
		return c, notConstant("the number " + text + ", which does not fit 64 bits")
	}
	i := -int64(c.Uint-1) - 1
	return Constant{Type: SignedFor(i), Int: i}, nil
}

// paramConstant reads a {slot:Type} parameter as ClickHouse binds it: the
// value text in the escaped format, parsed as Type.
func paramConstant(ps *grammar1.ParamSlotContext, scope *ConstScope) (c Constant, err error) {
	slot := nanopass.DecodeIdentifier(ps.Identifier().GetText())
	typeName := ps.ColumnTypeExpr().GetText()
	t, ok := ParseScalarType(typeName)
	if !ok {
		return c, notConstant("a parameter of type " + typeName)
	}
	var raw string
	if scope != nil {
		if _, isOpaque := scope.opaque[slot]; isOpaque {
			return c, notConstant("the parameter " + slot + ", which a SET binds to a value that is not a scalar literal")
		}
		raw, ok = scope.Params[slot]
	}
	if !ok {
		return c, eb.Build().Str("param", slot).Errorf("keelsonsql: the query parameter is not bound")
	}
	text, err := paramText(raw)
	if err != nil {
		return c, err
	}
	c, err = ParseAs(t, text)
	if err != nil {
		return c, eb.Build().Str("param", slot).Str("type", typeName).Str("value", text).Errorf("keelsonsql: the query parameter does not read as its type: %w", err)
	}
	c.Origin = ConstOriginParam
	return
}

// FormatFloat writes v as ClickHouse's text formats do: the shortest text
// that reads back as v at its width, in positional notation from 1e-6 up to
// below 1e21 and in exponent notation outside it, with no '+' in an
// exponent; inf, -inf and nan by name, and -0 keeping its sign.
func FormatFloat(v float64, bits int) string {
	switch {
	case math.IsNaN(v):
		return "nan"
	case math.IsInf(v, 1):
		return "inf"
	case math.IsInf(v, -1):
		return "-inf"
	case v == 0:
		if math.Signbit(v) {
			return "-0"
		}
		return "0"
	}
	var sb strings.Builder
	if v < 0 {
		sb.WriteByte('-')
		v = -v
	}
	// d.ddde±x: the shortest digits and the exponent of the first one.
	e := strconv.FormatFloat(v, 'e', -1, bits)
	mant, expText, _ := strings.Cut(e, "e")
	exp, _ := strconv.Atoi(expText)
	digits := strings.Replace(mant, ".", "", 1)
	k := len(digits)
	n := exp + 1 // the decimal point's position after the first digit
	switch {
	case n > 21 || n <= -6:
		sb.WriteByte(digits[0])
		if k > 1 {
			sb.WriteByte('.')
			sb.WriteString(digits[1:])
		}
		sb.WriteByte('e')
		sb.WriteString(strconv.Itoa(n - 1))
	case n >= k:
		sb.WriteString(digits)
		sb.WriteString(strings.Repeat("0", n-k))
	case n > 0:
		sb.WriteString(digits[:n])
		sb.WriteByte('.')
		sb.WriteString(digits[n:])
	default:
		sb.WriteString("0.")
		sb.WriteString(strings.Repeat("0", -n))
		sb.WriteString(digits)
	}
	return sb.String()
}

// FloatFieldText is a float literal's value as ClickHouse spells it outside
// a text format — a column's default name, a SET param_ value bound as
// text: FormatFloat with a trailing point when that reads as an integer
// (1000., -0.).
func FloatFieldText(v float64) string {
	s := FormatFloat(v, 64)
	if strings.Trim(s, "-0123456789") == "" {
		s += "."
	}
	return s
}

// ParseAs reads text as a value of t, as ClickHouse reads a parameter's
// value — within the spellings measured against clickhouse-local, and only
// those: an integer is digits after an optional sign (+ only, for UInt*); a
// float is decimal digits with an optional point and exponent, or inf,
// infinity or nan in any case, after an optional sign, and one beyond its
// type's range reads as an infinity or zero; a Bool is one of
// boolSpellings in any case. Anything else is ErrNotConstant — including an
// integer outside its type, which ClickHouse wraps — since what ClickHouse
// makes of it is not modelled here.
func ParseAs(t ScalarTypeE, text string) (c Constant, err error) {
	c.Type = t
	bad := func() error { return notConstant("the value " + strconv.Quote(text) + " read as " + t.String()) }
	switch {
	case t.IsUnsigned():
		digits := strings.TrimPrefix(text, "+")
		if !allDigits(digits) {
			return c, bad()
		}
		if c.Uint, err = strconv.ParseUint(digits, 10, t.Bits()); err != nil {
			return c, bad()
		}
	case t.IsSigned():
		digits := strings.TrimLeft(text, "+-")
		if len(text)-len(digits) > 1 || !allDigits(digits) {
			return c, bad()
		}
		if c.Int, err = strconv.ParseInt(strings.TrimPrefix(text, "+"), 10, t.Bits()); err != nil {
			return c, bad()
		}
	case t.IsFloat():
		var ok bool
		if c.Float, ok = parseFloatText(text, t.Bits()); !ok {
			return c, bad()
		}
	case t == ScalarTypeString:
		c.Str = text
	case t == ScalarTypeBool:
		v, known := boolSpellings[strings.ToLower(text)]
		if !known {
			return c, bad()
		}
		c.Bool = v
	default:
		return c, bad()
	}
	return c, nil
}

// boolSpellings are the Bool texts ClickHouse reads, lower-cased.
var boolSpellings = map[string]bool{
	"true": true, "false": false, "1": true, "0": false,
	"yes": true, "no": false, "on": true, "off": false, "y": true, "n": false, "t": true, "f": false,
	"enable": true, "disable": false, "enabled": true, "disabled": false,
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// parseFloatText reads the float spellings ParseAs admits.
func parseFloatText(text string, bits int) (f float64, ok bool) {
	body := text
	neg := false
	if body != "" && (body[0] == '+' || body[0] == '-') {
		neg, body = body[0] == '-', body[1:]
	}
	switch strings.ToLower(body) {
	case "inf", "infinity":
		if neg {
			return math.Inf(-1), true
		}
		return math.Inf(1), true
	case "nan":
		return math.NaN(), true
	}
	mant, exp, hasExp := strings.Cut(strings.ToLower(body), "e")
	intPart, frac, _ := strings.Cut(mant, ".")
	if intPart+frac == "" || (intPart != "" && !allDigits(intPart)) || (frac != "" && !allDigits(frac)) {
		return 0, false
	}
	if hasExp && !allDigits(strings.TrimLeft(exp, "+-")) || len(exp)-len(strings.TrimLeft(exp, "+-")) > 1 {
		return 0, false
	}
	f, err := strconv.ParseFloat(text, bits)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, false
	}
	return f, true
}
