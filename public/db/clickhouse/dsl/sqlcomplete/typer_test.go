package sqlcomplete_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/chtype"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlcomplete"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testTyper() *sqlcomplete.Typer {
	p := &sqlcomplete.Providers{
		ComponentType: func(kind string) (chtype.Type, bool) {
			s, hit := fixtureTypes[kind]
			if !hit {
				return chtype.Type{}, false
			}
			t, err := chtype.Parse(s)
			return t, err == nil
		},
		Catalog: sqlcomplete.Catalog{
			ColumnType: func(table string, column string) (chtype.Type, bool) {
				if column != "pair" {
					return chtype.Type{}, false
				}
				t, err := chtype.Parse("Tuple(lo UInt64, hi UInt64)")
				return t, err == nil
			},
		},
	}
	return &sqlcomplete.Typer{Providers: p}
}

func TestTyperShapes(t *testing.T) {
	ty := testTyper()
	cases := []struct{ expr, want string }{
		{"'x'", "String"},
		{"42", "UInt64"},
		{"-42", "Int64"},
		{"1.5", "Float64"},
		{"TRUE", "Bool"},
		{"LW_COMPONENT('SysCPU')", "Tuple(Id UInt64, LoadAvg1 Float32)"},
		{"CAST(x, 'Tuple(a UInt8)')", "Tuple(a UInt8)"},
		{"CAST(x AS Nullable(Float32))", "Nullable(Float32)"},
		{"accurateCast(x, 'UInt8')", "UInt8"},
		{"x :: Array(String)", "Array(String)"},
		{"tuple('a', 1)", "Tuple(String, UInt64)"},
		{"tupleElement(LW_COMPONENT('SysCPU'), 'LoadAvg1')", "Float32"},
		{"tupleElement(LW_COMPONENT('SysCPU'), 2)", "Float32"},
		{"tupleElement(tuple('a', 1), 1)", "String"},
		{"pair", "Tuple(lo UInt64, hi UInt64)"},
		{"tupleElement(pair, 'hi')", "UInt64"},
		{"  LW_COMPONENT( 'SysCPU' )  ", "Tuple(Id UInt64, LoadAvg1 Float32)"},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			got, ok := ty.TypeOf(c.expr)
			require.Truef(t, ok, "expected %q to type", c.expr)
			assert.Equal(t, c.want, got.String())
		})
	}
}

// Outside the closed list the answer is unknown — never a guess (§SD5).
func TestTyperUnknowns(t *testing.T) {
	ty := testTyper()
	for _, expr := range []string{
		"",
		"nosuchcolumn",
		"LW_COMPONENT('Nonesuch')",
		"LW_COMPONENT(kindExpr)",
		"someFunction(x)",
		"a + b",
		"tupleElement(nosuch, 'x')",
		"tupleElement(LW_COMPONENT('SysCPU'), 'nope')",
		"tupleElement(LW_COMPONENT('SysCPU'), 99)",
		"CAST(x, notALiteral)",
	} {
		t.Run(expr, func(t *testing.T) {
			_, ok := ty.TypeOf(expr)
			assert.False(t, ok)
		})
	}
}

// An alias defined in terms of itself is a buffer someone is editing, not a
// reason to hang.
func TestTyperDepthGuard(t *testing.T) {
	ty := testTyper()
	ty.Scope = &sqlcomplete.Scope{Aliases: map[string]string{
		"a": "b", "b": "c", "c": "a",
	}}
	_, ok := ty.TypeOf("a")
	assert.False(t, ok)
}

func TestTyperFollowsScopeAliases(t *testing.T) {
	ty := testTyper()
	ty.Scope = &sqlcomplete.Scope{Aliases: map[string]string{
		"m": "LW_COMPONENT('SysMem')",
		"n": "tupleElement(m, 'TotalBytes')",
	}}
	got, ok := ty.TypeOf("m")
	require.True(t, ok)
	assert.Equal(t, "UInt64", mustElement(t, got, "TotalBytes"))

	got, ok = ty.TypeOf("n")
	require.True(t, ok)
	assert.Equal(t, "UInt64", got.String())

	got, ok = ty.TypeOf("m.FreeBytes")
	require.True(t, ok)
	assert.Equal(t, "UInt64", got.String())
}

func mustElement(t *testing.T, ty chtype.Type, name string) string {
	t.Helper()
	a, ok := ty.Element(name)
	require.True(t, ok)
	require.NotNil(t, a.Type)
	return a.Type.String()
}
