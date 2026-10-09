package keelsonsql

import (
	"errors"
	"math"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// firstColumn evaluates the first item of `SELECT <expr>` in scope.
func firstColumn(t *testing.T, sql string, params map[string]string) (c Constant, err error) {
	t.Helper()
	pr, pErr := nanopass.Parse(sql)
	require.NoError(t, pErr, sql)
	scope, sErr := NewConstScope(pr, params)
	require.NoError(t, sErr, sql)
	var col *grammar1.ColumnsExprColumnContext
	for _, n := range nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		_, ok := ctx.(*grammar1.ProjectionClauseContext)
		return ok
	}) {
		cols := n.(*grammar1.ProjectionClauseContext).ColumnExprList().(*grammar1.ColumnExprListContext).AllColumnsExpr()
		col = cols[0].(*grammar1.ColumnsExprColumnContext)
	}
	expr, _, _ := Aliased(col.ColumnExpr())
	return EvalConstant(expr, scope)
}

// The types are what clickhouse-local reports for toTypeName of the same
// literal.
func TestEvalConstant_TypesAsClickHouse(t *testing.T) {
	for _, c := range []struct {
		expr string
		t    ScalarTypeE
	}{
		{"1", ScalarTypeUInt8}, {"255", ScalarTypeUInt8}, {"256", ScalarTypeUInt16}, {"65536", ScalarTypeUInt32},
		{"4294967296", ScalarTypeUInt64}, {"18446744073709551615", ScalarTypeUInt64},
		{"-1", ScalarTypeInt8}, {"-128", ScalarTypeInt8}, {"-129", ScalarTypeInt16}, {"-9223372036854775808", ScalarTypeInt64},
		{"-0", ScalarTypeUInt8}, {"010", ScalarTypeUInt8}, {"0x10", ScalarTypeUInt8}, {"+1", ScalarTypeUInt8},
		{"1.5", ScalarTypeFloat64}, {"1e3", ScalarTypeFloat64}, {"inf", ScalarTypeFloat64}, {"-nan", ScalarTypeFloat64},
		{"'x'", ScalarTypeString}, {"TRUE", ScalarTypeBool}, {"false", ScalarTypeBool}, {"(7)", ScalarTypeUInt8},
	} {
		v, err := firstColumn(t, "SELECT "+c.expr, nil)
		require.NoError(t, err, c.expr)
		assert.Equal(t, c.t, v.Type, c.expr)
	}
	v, err := firstColumn(t, "SELECT -9223372036854775808", nil)
	require.NoError(t, err)
	assert.Equal(t, int64(math.MinInt64), v.Int)
	v, err = firstColumn(t, "SELECT NULL", nil)
	require.NoError(t, err)
	assert.True(t, v.Null)
}

func TestEvalConstant_ParamsAndWith(t *testing.T) {
	v, err := firstColumn(t, "SELECT {p:Int16} AS p", map[string]string{"p": "-7"})
	require.NoError(t, err)
	assert.Equal(t, Constant{Type: ScalarTypeInt16, Int: -7, Origin: ConstOriginParam}, v)

	v, err = firstColumn(t, "WITH {p:String} AS s, s AS r SELECT r", map[string]string{"p": `a\tb`})
	require.NoError(t, err)
	assert.Equal(t, "a\tb", v.Str)
	assert.Equal(t, ConstOriginWith, v.Origin)

	// ClickHouse wraps 300 into Int8; that is not modelled.
	_, err = firstColumn(t, "SELECT {p:Int8} AS p", map[string]string{"p": "300"})
	assert.True(t, errors.Is(err, ErrNotConstant), "%v", err)
	_, err = firstColumn(t, "SELECT {q:Int8} AS q", map[string]string{"p": "1"})
	assert.Error(t, err)
	assert.False(t, errors.Is(err, ErrNotConstant), "an unbound parameter is the statement's error")
}

// The spellings are what clickhouse-local reads for SELECT {p:Type}; every
// one outside them is refused, whatever ClickHouse makes of it.
func TestParseAs_TheMeasuredSpellings(t *testing.T) {
	inf := math.Inf(1)
	for _, c := range []struct {
		t    ScalarTypeE
		text string
		want Constant
	}{
		{ScalarTypeUInt8, "5", Constant{Uint: 5}}, {ScalarTypeUInt8, "+5", Constant{Uint: 5}}, {ScalarTypeUInt8, "05", Constant{Uint: 5}},
		{ScalarTypeInt8, "+5", Constant{Int: 5}}, {ScalarTypeInt8, "-5", Constant{Int: -5}}, {ScalarTypeInt8, "-0", Constant{}},
		{ScalarTypeFloat64, "1.5", Constant{Float: 1.5}}, {ScalarTypeFloat64, "+1.5", Constant{Float: 1.5}}, {ScalarTypeFloat64, ".5", Constant{Float: 0.5}},
		{ScalarTypeFloat64, "5.", Constant{Float: 5}}, {ScalarTypeFloat64, "1E+3", Constant{Float: 1000}}, {ScalarTypeFloat64, "1.e3", Constant{Float: 1000}},
		{ScalarTypeFloat64, "00.5", Constant{Float: 0.5}}, {ScalarTypeFloat64, "INF", Constant{Float: inf}}, {ScalarTypeFloat64, "-infinity", Constant{Float: -inf}},
		{ScalarTypeFloat64, "1e400", Constant{Float: inf}}, {ScalarTypeFloat64, "-1e400", Constant{Float: -inf}}, {ScalarTypeFloat64, "1e-400", Constant{}},
		{ScalarTypeFloat32, "1e40", Constant{Float: inf}},
		{ScalarTypeBool, "Yes", Constant{Bool: true}}, {ScalarTypeBool, "OFF", Constant{}}, {ScalarTypeBool, "t", Constant{Bool: true}},
		{ScalarTypeBool, "Disabled", Constant{}}, {ScalarTypeBool, "1", Constant{Bool: true}},
	} {
		got, err := ParseAs(c.t, c.text)
		require.NoError(t, err, "%s %q", c.t, c.text)
		c.want.Type = c.t
		assert.Equal(t, c.want, got, "%s %q", c.t, c.text)
	}
	got, err := ParseAs(ScalarTypeFloat64, "-nan")
	require.NoError(t, err)
	assert.True(t, math.IsNaN(got.Float))

	for _, c := range []struct {
		t    ScalarTypeE
		text string
	}{
		{ScalarTypeUInt8, "-0"}, {ScalarTypeUInt8, " 5"}, {ScalarTypeUInt8, "5 "}, {ScalarTypeUInt8, "0x10"}, {ScalarTypeUInt8, "1e2"},
		{ScalarTypeUInt8, "1.0"}, {ScalarTypeUInt8, "5_0"}, {ScalarTypeUInt8, ""}, {ScalarTypeUInt8, "256"}, {ScalarTypeInt8, "-"}, {ScalarTypeInt8, "+-5"},
		{ScalarTypeFloat64, "1_000"}, {ScalarTypeFloat64, "0x1p3"}, {ScalarTypeFloat64, "0x10"}, {ScalarTypeFloat64, " 1"}, {ScalarTypeFloat64, "1 "},
		{ScalarTypeFloat64, "1.5.5"}, {ScalarTypeFloat64, "e3"}, {ScalarTypeFloat64, ""}, {ScalarTypeFloat64, "1e"}, {ScalarTypeFloat64, "."},
		{ScalarTypeBool, "2"}, {ScalarTypeBool, "truex"}, {ScalarTypeBool, " true"}, {ScalarTypeBool, ""},
	} {
		_, err := ParseAs(c.t, c.text)
		assert.True(t, errors.Is(err, ErrNotConstant), "%s %q", c.t, c.text)
	}
}

func TestEvalConstant_NotConstants(t *testing.T) {
	for _, sql := range []string{
		"SELECT now()", "SELECT 1 + 1", "SELECT v", "SELECT - - 1", "SELECT 18446744073709551616",
		"WITH now() AS t SELECT t", "SELECT {p:DateTime} AS p", "SELECT [1, 2]",
	} {
		_, err := firstColumn(t, sql, map[string]string{"p": "1"})
		assert.True(t, errors.Is(err, ErrNotConstant), "%s: %v", sql, err)
	}
}

// A WITH constant names a keelson() argument natively as it does in the
// trivial evaluator; a WITH item that is not a constant is an error there.
func TestExpandWithArgs_WithConstants(t *testing.T) {
	r := argsReg(t)
	got, calls, err := ExpandWithArgs(r, "", "WITH 3 AS k, 'it''s' AS l SELECT * FROM keelson('seq', n = k, label = l)", nil)
	require.NoError(t, err)
	require.Len(t, calls, 1)
	assert.Equal(t, map[string]string{"n": "3", "label": "it's"}, calls[0].Raw)
	assert.Equal(t, "WITH 3 AS k, 'it''s' AS l SELECT * FROM "+calls[0].Temp, got)

	_, _, err = ExpandWithArgs(r, "", "WITH now() AS k SELECT * FROM keelson('seq', n = k)", nil)
	assert.Error(t, err)
	_, _, err = ExpandWithArgs(r, "", "SELECT * FROM keelson('seq', n = 0x10)", nil)
	assert.NoError(t, err, "a hexadecimal argument reads as its value")
}
