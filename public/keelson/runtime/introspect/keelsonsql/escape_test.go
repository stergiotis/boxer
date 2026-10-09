package keelsonsql

import (
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
	got, err := PreludeParams(TopLevelSets(pr), map[string]string{"a": "request", "d": "kept"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"a": "z", "b": "16", "c": "-1.5", "d": "kept"}, got)

	pr, err = nanopass.Parse(`SET param_a = 'x\\ty'; SELECT 1`)
	require.NoError(t, err)
	got, err = PreludeParams(TopLevelSets(pr), nil)
	require.NoError(t, err)
	assert.Equal(t, `x\ty`, got["a"], "the literal is decoded; the parameter's own escapes are read where it binds")

	pr, err = nanopass.Parse(`SET param_a = NULL; SELECT 1`)
	require.NoError(t, err)
	_, err = PreludeParams(TopLevelSets(pr), nil)
	assert.Error(t, err)
}

// A SET param_ prelude binds a keelson() argument natively as it does in the
// trivial evaluator: one statement text in every host (ADR-0290 C1).
func TestExpandWithArgs_SetPreludeBinds(t *testing.T) {
	r := argsReg(t)
	_, calls, err := ExpandWithArgs(r, "", `SET param_k = 4, param_l = 'a\\tb'; SELECT * FROM keelson('seq', n = {k:UInt64}, label = {l:String})`, nil)
	require.NoError(t, err)
	require.Len(t, calls, 1)
	assert.Equal(t, map[string]string{"n": "4", "label": "a\tb"}, calls[0].Raw)
}
