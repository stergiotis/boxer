package chlocalbroker

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParamPrelude(t *testing.T) {
	assert.Empty(t, paramPrelude(nil))
	assert.Empty(t, paramPrelude(map[string]string{}))

	// Sorted by name; values quoted through sqlQuoteString.
	got := paramPrelude(map[string]string{
		"z":   "plain",
		"a":   "it's",
		"mid": `back\slash`,
	})
	want := "SET param_a = 'it\\'s';\n" +
		"SET param_mid = 'back\\\\slash';\n" +
		"SET param_z = 'plain';\n"
	assert.Equal(t, want, got)
}

func TestValidParamName(t *testing.T) {
	for _, ok := range []string{"a", "lim", "selection_id", "A9_b"} {
		assert.True(t, validParamName(ok), ok)
	}
	for _, bad := range []string{"", "9lead", "has-dash", "has space", strings.Repeat("x", 65)} {
		assert.False(t, validParamName(bad), bad)
	}
}

func TestFoldParamsKeyDiscipline(t *testing.T) {
	base := computeCacheKey("SELECT {x:UInt64}", "TabSeparated", nil)

	// No params: the base passes through untouched.
	assert.Equal(t, base, foldParams(base, nil))

	// Same bindings, any map: deterministic.
	k1 := foldParams(base, map[string]string{"x": "1", "y": "2"})
	k2 := foldParams(base, map[string]string{"y": "2", "x": "1"})
	assert.Equal(t, k1, k2)
	assert.NotEqual(t, base, k1)

	// A changed binding is a different key.
	assert.NotEqual(t, k1, foldParams(base, map[string]string{"x": "1", "y": "3"}))

	// Domain separation from the input-table fold: the same name/value
	// pair must not alias across the two folds (a param is not a table).
	pk := foldParams(base, map[string]string{"a": "b"})
	tk := foldInputTables(base, map[string][]byte{"a": []byte("b")})
	assert.NotEqual(t, pk, tk)
}

func TestWireRequestParamsRoundTrip(t *testing.T) {
	b, err := encodeRequest(wireRequest{
		SQL:    "SELECT {x:UInt64}",
		Params: map[string]string{"x": "5"},
	})
	require.NoError(t, err)
	req, err := decodeRequest(b)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"x": "5"}, req.Params)
}

// TestExecOnPool_ParamBinding is the ADR-0133 §SD2 end-to-end: the broker's
// SET-prelude reaches clickhouse-local and the engine substitutes the
// placeholder — including a value whose quoting must survive.
func TestExecOnPool_ParamBinding(t *testing.T) {
	_, caller := newTestBroker(t)

	rep, err := ExecOnPool(context.Background(), caller, "scratchpad", ExecRequest{
		SQL:    "SELECT {lim:UInt64} + 1",
		Format: "TabSeparated",
		Params: map[string]string{"lim": "5"},
	})
	require.NoError(t, err)
	body, err := io.ReadAll(rep)
	require.NoError(t, err)
	require.NoError(t, rep.Err())
	assert.Equal(t, "6", strings.TrimSpace(string(body)))

	rep, err = ExecOnPool(context.Background(), caller, "scratchpad", ExecRequest{
		SQL:    "SELECT {s:String}",
		Format: "TabSeparated",
		Params: map[string]string{"s": `it's a \ test`},
	})
	require.NoError(t, err)
	body, err = io.ReadAll(rep)
	require.NoError(t, err)
	require.NoError(t, rep.Err())
	assert.Equal(t, `it\'s a \\ test`, strings.TrimSpace(string(body)),
		"TabSeparated escapes the quote and backslash on output; the binding itself is verbatim")
}

func TestExecOnPool_ParamCacheKeying(t *testing.T) {
	_, caller := newTestBroker(t)

	run := func(lim string) (body string, hit bool) {
		rep, err := ExecOnPool(context.Background(), caller, "scratchpad", ExecRequest{
			SQL:       "SELECT {lim:UInt64} * 10",
			Format:    "TabSeparated",
			Cacheable: true,
			Params:    map[string]string{"lim": lim},
		})
		require.NoError(t, err)
		b, err := io.ReadAll(rep)
		require.NoError(t, err)
		require.NoError(t, rep.Err())
		return strings.TrimSpace(string(b)), rep.CacheHit
	}

	body, hit := run("3")
	assert.Equal(t, "30", body)
	assert.False(t, hit, "first run is a miss")

	body, hit = run("3")
	assert.Equal(t, "30", body)
	assert.True(t, hit, "identical bindings serve from the cache")

	body, hit = run("4")
	assert.Equal(t, "40", body)
	assert.False(t, hit, "a changed binding must miss — the params fold in the key")
}

func TestExecOnPool_RejectsBadParamName(t *testing.T) {
	_, caller := newTestBroker(t)

	rep, err := ExecOnPool(context.Background(), caller, "scratchpad", ExecRequest{
		SQL:    "SELECT 1",
		Format: "TabSeparated",
		Params: map[string]string{"has-dash": "x"},
	})
	require.NoError(t, err, "transport succeeds; the broker replies with a structured error")
	_, _ = io.ReadAll(rep)
	require.Error(t, rep.Err())
	assert.Contains(t, rep.Err().Error(), "invalid parameter name")
}
