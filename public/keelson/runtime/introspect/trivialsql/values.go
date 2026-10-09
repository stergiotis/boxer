package trivialsql

import (
	"math"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// valuesColumn is one column of a values() table: its name, type and
// whether it holds NULL.
type valuesColumn struct {
	name     string
	t        keelsonsql.ScalarTypeE
	nullable bool
}

// values answers ClickHouse's values() table function over constants:
// values('a UInt8, b String', (1, 'x'), …), whose rows are converted to the
// declared types, or values((1, 'x'), …), whose columns c1, c2, … take the
// type every row's value fits. A conversion ClickHouse rejects is an error of
// the statement; one this evaluator does not model is refused.
func (inst *evaluator) values(f *grammar1.TableFunctionExprContext) (batch arrow.RecordBatch, err error) {
	if f.TableArgList() == nil {
		return nil, eb.Build().Errorf("trivialsql: values() needs rows")
	}
	args := f.TableArgList().AllTableArgExpr()
	var cols []valuesColumn
	// The first argument is the structure when rows follow it and it reads
	// as one; otherwise it is a row, as ClickHouse decides.
	if len(args) > 1 {
		first, firstErr := keelsonsql.EvalTableArg(args[0], inst.scope)
		if firstErr == nil && !first.Null && first.Type == keelsonsql.ScalarTypeString {
			cols, err = parseStructure(first.Str)
			if err != nil {
				return nil, err
			}
			if cols != nil {
				args = args[1:]
			}
		}
	}
	rows := make([][]keelsonsql.Constant, 0, len(args))
	for _, a := range args {
		var row []keelsonsql.Constant
		row, err = inst.valuesRow(a)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, eb.Build().Errorf("trivialsql: values() needs rows")
	}
	if cols == nil {
		cols, err = inferColumns(rows)
		if err != nil {
			return nil, err
		}
	}
	for i, row := range rows {
		if len(row) != len(cols) {
			return nil, eb.Build().Int("row", i+1).Int("values", len(row)).Int("columns", len(cols)).Errorf("trivialsql: a values() row does not have one value per column")
		}
	}
	fields := make([]arrow.Field, len(cols))
	arrs := make([]arrow.Array, 0, len(cols))
	defer func() {
		for _, a := range arrs {
			a.Release()
		}
	}()
	for c, col := range cols {
		fields[c] = arrow.Field{Name: col.name, Type: col.t.Arrow(), Nullable: col.nullable}
		b := array.NewBuilder(memory.DefaultAllocator, col.t.Arrow())
		for _, row := range rows {
			v, cErr := convert(row[c], col)
			if cErr != nil {
				b.Release()
				return nil, cErr
			}
			appendConstant(b, v)
		}
		arrs = append(arrs, b.NewArray())
		b.Release()
	}
	return array.NewRecordBatch(arrow.NewSchema(fields, nil), arrs, int64(len(rows))), nil
}

// valuesRow reads one row: a tuple of constants, or one constant.
func (inst *evaluator) valuesRow(a grammar1.ITableArgExprContext) (row []keelsonsql.Constant, err error) {
	if tup, isTuple := a.ColumnExpr().(*grammar1.ColumnExprTupleContext); isTuple && tup != nil {
		for _, ce := range tup.ColumnExprList().(*grammar1.ColumnExprListContext).AllColumnsExpr() {
			col, isCol := ce.(*grammar1.ColumnsExprColumnContext)
			if !isCol {
				return nil, refuse("a values() row holding " + ce.GetText())
			}
			v, vErr := keelsonsql.EvalConstant(col.ColumnExpr(), inst.scope)
			if vErr != nil {
				return nil, constantErr(vErr)
			}
			row = append(row, v)
		}
		return row, nil
	}
	v, err := keelsonsql.EvalTableArg(a, inst.scope)
	if err != nil {
		return nil, constantErr(err)
	}
	return []keelsonsql.Constant{v}, nil
}

// parseStructure reads `name Type, …`, a type being a ScalarTypeE or
// Nullable of one, and a name a plain identifier; cols is nil when s is not
// shaped as a structure (no space, a quote in a name), and so is data. A
// type outside that set, or a backquoted or unbalanced name, is refused:
// whether ClickHouse reads a structure there (DateTime, `a`) or data
// ('hello world') is not modelled here.
func parseStructure(s string) (cols []valuesColumn, err error) {
	for _, part := range splitTopLevel(s) {
		part = strings.TrimSpace(part)
		name, typ, ok := strings.Cut(part, " ")
		if !ok || name == "" || strings.ContainsAny(name, "'\"") {
			return nil, nil
		}
		if strings.ContainsAny(name, "`()") {
			// A quoted or unbalanced name: ClickHouse may read a structure
			// here, or data; which is not modelled.
			return nil, refuse("a values() structure naming a column " + name)
		}
		col := valuesColumn{name: nanopass.DecodeIdentifier(name)}
		typ = strings.TrimSpace(typ)
		if inner, isNullable := strings.CutPrefix(typ, "Nullable("); isNullable && strings.HasSuffix(inner, ")") {
			col.nullable = true
			typ = strings.TrimSpace(strings.TrimSuffix(inner, ")"))
		}
		t, known := keelsonsql.ParseScalarType(typ)
		if !known {
			return nil, refuse("a values() column of type " + typ)
		}
		col.t = t
		cols = append(cols, col)
	}
	return
}

// splitTopLevel splits s at commas outside parentheses and backquotes; an
// unbalanced s is one part, which then does not read as a structure.
func splitTopLevel(s string) (parts []string) {
	depth, start := 0, 0
	quoted := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '`':
			quoted = !quoted
		case quoted:
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ',' && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	if depth != 0 || quoted {
		return []string{s}
	}
	return append(parts, s[start:])
}

// inferColumns is values() without a structure: columns c1, c2, … whose
// type is the least type every row's value converts to, as ClickHouse finds
// it — a UInt wide enough, an Int when one is negative, Float32 when every
// float is one and the integers are at most 16 bits, Float64 when a float
// joins integers of at most 32 bits — and Nullable when one is NULL.
// Types with no such common type are refused, where ClickHouse would fail.
func inferColumns(rows [][]keelsonsql.Constant) (cols []valuesColumn, err error) {
	width := len(rows[0])
	for c := range width {
		col := valuesColumn{name: "c" + strconv.Itoa(c+1)}
		var uBits, sBits, intBits, floatBits int
		var hasString, hasBool, hasValue bool
		for _, row := range rows {
			if c >= len(row) {
				return nil, eb.Build().Errorf("trivialsql: values() rows differ in their number of values")
			}
			v := row[c]
			if v.Null {
				col.nullable = true
				continue
			}
			hasValue = true
			switch {
			case v.Type.IsUnsigned():
				uBits = max(uBits, v.Type.Bits())
				intBits = max(intBits, v.Type.Bits())
			case v.Type.IsSigned():
				sBits = max(sBits, v.Type.Bits())
				intBits = max(intBits, v.Type.Bits())
			case v.Type.IsFloat():
				floatBits = max(floatBits, v.Type.Bits())
			case v.Type == keelsonsql.ScalarTypeString:
				hasString = true
			case v.Type == keelsonsql.ScalarTypeBool:
				hasBool = true
			}
		}
		hasInt, hasFloat := intBits > 0, floatBits > 0
		switch {
		case !hasValue:
			return nil, refuse("a values() column holding only NULL")
		case hasString && !hasInt && !hasFloat && !hasBool:
			col.t = keelsonsql.ScalarTypeString
		case hasBool && !hasInt && !hasFloat && !hasString:
			col.t = keelsonsql.ScalarTypeBool
		case hasString || hasBool:
			return nil, refuse("a values() column mixing " + mixed(hasInt, hasFloat, hasString, hasBool))
		case hasFloat && intBits == 64:
			return nil, refuse("a values() column mixing 64-bit integers and floats")
		case floatBits == 32 && intBits <= 16:
			// Float32 holds every value when the integers need at most
			// its 24 mantissa bits, and ClickHouse keeps it.
			col.t = keelsonsql.ScalarTypeFloat32
		case hasFloat:
			col.t = keelsonsql.ScalarTypeFloat64
		case sBits == 0:
			col.t = unsignedOfBits(uBits)
		default:
			bits := max(sBits, 2*uBits)
			if bits > 64 {
				return nil, refuse("a values() column mixing UInt64 and negative integers")
			}
			col.t = signedOfBits(bits)
		}
		cols = append(cols, col)
	}
	return
}

func mixed(hasInt, hasFloat, hasString, hasBool bool) string {
	var kinds []string
	for _, k := range []struct {
		has  bool
		name string
	}{{hasInt, "integers"}, {hasFloat, "floats"}, {hasString, "strings"}, {hasBool, "Bools"}} {
		if k.has {
			kinds = append(kinds, k.name)
		}
	}
	return strings.Join(kinds, " and ")
}

func unsignedOfBits(bits int) keelsonsql.ScalarTypeE {
	switch bits {
	case 8:
		return keelsonsql.ScalarTypeUInt8
	case 16:
		return keelsonsql.ScalarTypeUInt16
	case 32:
		return keelsonsql.ScalarTypeUInt32
	}
	return keelsonsql.ScalarTypeUInt64
}

func signedOfBits(bits int) keelsonsql.ScalarTypeE {
	switch bits {
	case 8:
		return keelsonsql.ScalarTypeInt8
	case 16:
		return keelsonsql.ScalarTypeInt16
	case 32:
		return keelsonsql.ScalarTypeInt32
	}
	return keelsonsql.ScalarTypeInt64
}

// convert is v as a value of col, as ClickHouse converts a values() row: an
// integer or an integral float into an integer type it fits, a number into
// a float (a finite one only within Float32's range), a string into any
// type by reading it, an integer into String by its digits, 0 and 1 into
// Bool, and NULL only into a Nullable column. Any other pairing is refused.
func convert(v keelsonsql.Constant, col valuesColumn) (out keelsonsql.Constant, err error) {
	t := col.t
	out = keelsonsql.Constant{Type: t}
	if v.Null {
		if !col.nullable {
			return out, eb.Build().Str("column", col.name).Str("type", t.String()).Errorf("trivialsql: values() cannot convert NULL to a column that is not Nullable")
		}
		out.Null = true
		return out, nil
	}
	if v.Type == keelsonsql.ScalarTypeString && t != keelsonsql.ScalarTypeString {
		out, err = keelsonsql.ParseAs(t, v.Str)
		return out, constantErr(err)
	}
	isInt := v.Type.IsUnsigned() || v.Type.IsSigned()
	switch {
	case t == v.Type:
		v.Origin, v.Ref = keelsonsql.ConstOriginLiteral, ""
		return v, nil
	case t.IsUnsigned() || t.IsSigned():
		var f float64
		switch {
		case v.Type.IsUnsigned():
			if t.IsUnsigned() {
				if v.Uint > maxUnsigned(t) {
					return out, outOfRange(v, col)
				}
				out.Uint = v.Uint
				return out, nil
			}
			if v.Uint > uint64(maxSigned(t)) {
				return out, outOfRange(v, col)
			}
			out.Int = int64(v.Uint)
			return out, nil
		case v.Type.IsSigned():
			if t.IsUnsigned() {
				return out, outOfRange(v, col)
			}
			if v.Int < -maxSigned(t)-1 || v.Int > maxSigned(t) {
				return out, outOfRange(v, col)
			}
			out.Int = v.Int
			return out, nil
		case v.Type.IsFloat():
			f = v.Float
		default:
			return out, refuse("a values() " + v.Type.String() + " in a " + t.String() + " column")
		}
		if f != math.Trunc(f) || math.IsInf(f, 0) || math.IsNaN(f) {
			return out, outOfRange(v, col)
		}
		// The bounds are powers of two, exact as float64; MaxUint64 and
		// MaxInt64 are not, and round up to them.
		if t.IsUnsigned() {
			if f < 0 || f >= math.Ldexp(1, t.Bits()) {
				return out, outOfRange(v, col)
			}
			out.Uint = uint64(f)
		} else {
			if f < -math.Ldexp(1, t.Bits()-1) || f >= math.Ldexp(1, t.Bits()-1) {
				return out, outOfRange(v, col)
			}
			out.Int = int64(f)
		}
		return out, nil
	case t.IsFloat():
		switch {
		case v.Type.IsUnsigned():
			out.Float = float64(v.Uint)
		case v.Type.IsSigned():
			out.Float = float64(v.Int)
		case v.Type.IsFloat():
			out.Float = v.Float
		default:
			return out, refuse("a values() " + v.Type.String() + " in a " + t.String() + " column")
		}
		if t == keelsonsql.ScalarTypeFloat32 {
			// A finite value beyond Float32's range is ClickHouse's error,
			// not an infinity; a smaller one rounds, to 0 if need be.
			if !math.IsInf(out.Float, 0) && math.Abs(out.Float) > math.MaxFloat32 {
				return out, outOfRange(v, col)
			}
			out.Float = float64(float32(out.Float))
		}
		return out, nil
	case t == keelsonsql.ScalarTypeString && isInt:
		out.Str, _ = v.Text()
		return out, nil
	case t == keelsonsql.ScalarTypeBool && v.Type.IsUnsigned() && v.Uint <= 1:
		out.Bool = v.Uint == 1
		return out, nil
	}
	return out, refuse("a values() " + v.Type.String() + " in a " + t.String() + " column")
}

func maxUnsigned(t keelsonsql.ScalarTypeE) uint64 {
	return math.MaxUint64 >> (64 - t.Bits())
}

func maxSigned(t keelsonsql.ScalarTypeE) int64 {
	return math.MaxInt64 >> (64 - t.Bits())
}

func outOfRange(v keelsonsql.Constant, col valuesColumn) error {
	text, _ := v.Text()
	return eb.Build().Str("column", col.name).Str("type", col.t.String()).Str("value", text).Errorf("trivialsql: a values() value cannot be represented in its column's type")
}
