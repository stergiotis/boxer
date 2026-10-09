package keelsonsql

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// The cases are what clickhouse-local answers for SELECT '<literal>'.
func TestUnquoteString_AsClickHouseReadsALiteral(t *testing.T) {
	for _, c := range []struct{ literal, want string }{
		{`'plain'`, "plain"},
		{`'it''s'`, "it's"},
		{`'it\'s'`, "it's"},
		{`'a\nb\tc\rd\0e'`, "a\nb\tc\rd\x00e"},
		{`'\b\f\v\a\e'`, "\b\f\v\a\x1b"},
		{`'a\\b'`, `a\b`},
		{`'\"\/` + "\\`" + `'`, "\"/`"},
		{`'a\x41'`, "aA"},
		{`'a\N'`, "a"},
		{`'100\%'`, `100\%`},
		{`'\z'`, `\z`},
	} {
		assert.Equal(t, c.want, unquoteString(c.literal), c.literal)
	}
}

func TestParamText_AsClickHouseReadsAParameter(t *testing.T) {
	v, err := paramText(`a\tb`)
	require.NoError(t, err)
	assert.Equal(t, "a\tb", v)
	v, err = paramText(`it''s`)
	require.NoError(t, err)
	assert.Equal(t, "it''s", v, "a doubled quote is only a literal's spelling")
	_, err = paramText("a\tb")
	assert.Error(t, err, "a raw tab ends an escaped value")
}

func TestPreludeParams(t *testing.T) {
	pr, err := nanopass.Parse(`SET param_a = 'x\\ty', param_b = 0x10; SET param_c = -1.5, max_threads = 1; SET param_a = 'z'; SELECT 1`)
	require.NoError(t, err)
	got, opaque, err := PreludeParams(TopLevelSets(pr), map[string]string{"a": "request", "d": "kept"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"a": "z", "b": "16", "c": "-1.5", "d": "kept"}, got)
	assert.Empty(t, opaque)

	pr, err = nanopass.Parse(`SET param_a = 'x\\ty'; SELECT 1`)
	require.NoError(t, err)
	got, _, err = PreludeParams(TopLevelSets(pr), nil)
	require.NoError(t, err)
	assert.Equal(t, `x\ty`, got["a"], "the literal is decoded; the parameter's own escapes are read where it binds")

	// A number binds its value as ClickHouse spells it (clickhouse-local:
	// SET param_k = <v>; SELECT {k:String}).
	pr, err = nanopass.Parse(`SET param_a = 1.50, param_b = 1e3, param_c = -0.0, param_d = -0, param_e = 1e21; SELECT 1`)
	require.NoError(t, err)
	got, _, err = PreludeParams(TopLevelSets(pr), nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"a": "1.5", "b": "1000.", "c": "-0.", "d": "0", "e": "1e21"}, got)

	// A value that is not a scalar literal is not modelled, and not an error:
	// only a constant reading it fails.
	pr, err = nanopass.Parse(`SET param_a = NULL, param_b = [1, 2], param_c = 3; SELECT 1`)
	require.NoError(t, err)
	got, opaque, err = PreludeParams(TopLevelSets(pr), map[string]string{"a": "request"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"c": "3"}, got)
	assert.Equal(t, map[string]struct{}{"a": {}, "b": {}}, opaque)
}

// A SET param_ prelude binds a keelson() argument natively as it does in the
// trivial evaluator: one statement text in every host (ADR-0290 C1).
func TestExpandWithArgs_SetPreludeBinds(t *testing.T) {
	// A SET this package does not model fails only the constant that reads it.
	_, calls, err := ExpandWithArgs(argsReg(t), "", `SET param_ids = [1, 2], param_k = 2; SELECT {ids:Array(UInt8)} AS a FROM keelson('seq', n = {k:UInt64})`, nil)
	require.NoError(t, err)
	require.Len(t, calls, 1)
	_, _, err = ExpandWithArgs(argsReg(t), "", `SET param_k = NULL; SELECT * FROM keelson('seq', n = {k:UInt64})`, nil)
	assert.True(t, errors.Is(err, ErrNotConstant), "%v", err)

	r := argsReg(t)
	_, calls, err = ExpandWithArgs(r, "", `SET param_k = 4, param_l = 'a\\tb'; SELECT * FROM keelson('seq', n = {k:UInt64}, label = {l:String})`, nil)
	require.NoError(t, err)
	require.Len(t, calls, 1)
	assert.Equal(t, map[string]string{"n": "4", "label": "a\tb"}, calls[0].Raw)
}
