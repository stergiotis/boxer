package glosssql

import (
	"testing"

	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func expand(t *testing.T, sql string) string {
	t.Helper()
	out, err := Expand(sql, gloss.Default())
	require.NoError(t, err)
	return out
}

func expandErr(t *testing.T, sql string) error {
	t.Helper()
	_, err := Expand(sql, gloss.Default())
	require.Error(t, err)
	return err
}

// The goldens of ADR-0186 §Verification: label rules, typed parameter
// marshalling, and the untouched-SQL fast path.
func TestExpandGoldens(t *testing.T) {
	// A bare identifier contributes its own name.
	assert.Equal(t,
		`SELECT reading AS "reading@gloss/temperature;unit=K" FROM t`,
		expand(t, `SELECT gloss(reading, 'gloss/temperature', 'unit', 'K') FROM t`))

	// 'label' names the alias; the token may already carry parameters.
	assert.Equal(t,
		`SELECT a + b AS "span@gloss/length;unit=m" FROM t`,
		expand(t, `SELECT gloss(a + b, 'gloss/length;unit=m', 'label', 'span') FROM t`))

	// Any other expression contributes its source text, as ClickHouse would.
	assert.Equal(t,
		`SELECT round(x, 1) AS "round(x, 1)@gloss/bytes" FROM t`,
		expand(t, `SELECT gloss(round(x, 1), 'gloss/bytes') FROM t`))

	// A qualified identifier contributes its last part, decoded.
	assert.Equal(t,
		"SELECT t.`temp c` AS \"temp c@gloss/temperature;unit=C\" FROM t",
		expand(t, "SELECT gloss(t.`temp c`, 'gloss/temperature', 'unit', 'C') FROM t"))

	// The content family works the same way; several calls in one select.
	assert.Equal(t,
		`SELECT body AS "body@text/markdown", n AS "n@gloss/raw", 1 AS x FROM t`,
		expand(t, `SELECT gloss(body, 'text/markdown'), gloss(n, 'gloss/raw'), 1 AS x FROM t`))

	// Case- and quoting-insensitive name; parameter keys fold.
	assert.Equal(t,
		`SELECT reading AS "reading@gloss/temperature;unit=K" FROM t`,
		expand(t, `SELECT GLOSS(reading, 'gloss/temperature', 'UNIT', 'K') FROM t`))

	// No call, no parse: the SQL comes back untouched.
	plain := "SELECT 1 AS `t@gloss/temperature;unit=K`"
	assert.Equal(t, plain, expand(t, plain), "an alias spelling is not a call")
	assert.Equal(t, "SELECT 1", expand(t, "SELECT 1"))
}

// Typed parameter values: numbers keep a canonical spelling, strings are
// unescaped, bools spell true/false — and quoting per RFC 2045 when the
// value needs it.
func TestExpandTypedParameters(t *testing.T) {
	// A catalog with a gloss taking free-valued parameters, so the values
	// can be anything.
	cat := gloss.NewCatalog()
	cat.MustRegister(&freeGloss{})
	out, err := Expand(`SELECT gloss(v, 'gloss/free', 'digits', 1, 'ratio', 2.50, 'on', true, 'name', 'a b')`, cat)
	require.NoError(t, err)
	assert.Equal(t, `SELECT v AS "v@gloss/free;digits=1;name=""a b"";on=true;ratio=2.5"`, out,
		"sorted parameters, the number canonical, the space forcing an RFC 2045 quoted value (doubled inside the identifier)")
}

func TestExpandRejections(t *testing.T) {
	assert.ErrorContains(t, expandErr(t, `SELECT gloss(x, 'gloss/temperatur', 'unit', 'K')`), "unknown media type")
	assert.ErrorContains(t, expandErr(t, `SELECT gloss(x, 'gloss/temperature', 'unti', 'K')`), "unknown parameter")
	assert.ErrorContains(t, expandErr(t, `SELECT gloss(x, 'gloss/temperature')`), "requires unit=")
	assert.ErrorContains(t, expandErr(t, `SELECT gloss(x, 'gloss/temperature', 'unit', 'k')`), "not allowed")
	assert.ErrorContains(t, expandErr(t, `SELECT gloss(x, 'gloss/temperature;unit=K', 'unit', 'C')`), "in the media type and in a pair")
	assert.ErrorContains(t, expandErr(t, `SELECT gloss(x, 'gloss/raw', 'label')`), "one key has no value")
	assert.ErrorContains(t, expandErr(t, `SELECT gloss(x)`), "needs the expression and a media type")
	assert.ErrorContains(t, expandErr(t, `SELECT gloss(x, gloss_type)`), "must be a string literal")
	assert.ErrorContains(t, expandErr(t, `SELECT gloss(x, 'gloss/temperature', 'unit', unit_col)`), "must be a scalar literal")
	assert.ErrorContains(t, expandErr(t, `SELECT gloss(x, 'gloss/temperature', 'unit', [1, 2])`), "must be a scalar literal")
	assert.ErrorContains(t, expandErr(t, `SELECT gloss(x, 'gloss/raw', 'label', NULL)`), "NULL is not a parameter value")
	assert.ErrorContains(t, expandErr(t, `SELECT gloss(x, 'gloss/raw') AS y`), "mints its own alias")
	assert.ErrorContains(t, expandErr(t, `SELECT 1 FROM t WHERE gloss(x, 'gloss/raw') = 1`), "whole projection item")
	assert.ErrorContains(t, expandErr(t, `SELECT gloss(gloss(x, 'gloss/raw'), 'gloss/raw')`), "inside another")
	assert.ErrorContains(t, expandErr(t, `SELECT gloss(x, 'celsius')`), "no slash")

	// The rejection carries the call's source range.
	err := expandErr(t, `SELECT 1, gloss(x, 'gloss/nope')`)
	assert.ErrorContains(t, err, "unknown media type")
}

// Call is Expand's dual: what the Glosses tab drops at the caret expands to
// the alias with the same token (ADR-0186 §SD6).
func TestCallRoundTrips(t *testing.T) {
	call := Call("reading", gloss.MediaTypeTemperature, map[string]string{"unit": "K"})
	assert.Equal(t, `gloss(reading, 'gloss/temperature', 'unit', 'K')`, call)
	assert.Equal(t, `SELECT reading AS "reading@gloss/temperature;unit=K" FROM t`, expand(t, "SELECT "+call+" FROM t"))

	assert.Equal(t, `gloss(expr, 'gloss/raw')`, Call("expr", gloss.MediaTypeRaw, nil), "no parameters: the type alone; a placeholder expression goes in verbatim")

	// Parameters in name order, values quoted as ClickHouse literals.
	call = Call("x", "gloss/free", map[string]string{"name": "it's", "digits": "1"})
	assert.Equal(t, `gloss(x, 'gloss/free', 'digits', '1', 'name', 'it\'s')`, call)
	cat := gloss.NewCatalog()
	cat.MustRegister(&freeGloss{})
	out, err := Expand("SELECT "+call, cat)
	require.NoError(t, err)
	assert.Equal(t, `SELECT x AS "x@gloss/free;digits=1;name=it's"`, out, "the SQL escape is undone; RFC 2045 does not quote an apostrophe")
}

func TestHasMarker(t *testing.T) {
	assert.True(t, HasMarker("select GLOSS(x, 'gloss/raw')"))
	assert.True(t, HasMarker("select `x@gloss/raw`"), "a false positive costs one parse")
	assert.False(t, HasMarker("select 1"))
}

func TestFunctions(t *testing.T) {
	fns := Functions()
	require.Len(t, fns, 1)
	assert.Equal(t, FuncName, fns[0].Name)
}

// freeGloss accepts any parameter names with any values, for the typed
// parameter test.
type freeGloss struct{}

func (freeGloss) MediaType() string { return "gloss/free" }
func (freeGloss) Doc() string       { return "test" }
func (freeGloss) Params() []gloss.ParamSpec {
	return []gloss.ParamSpec{{Name: "digits"}, {Name: "ratio"}, {Name: "on"}, {Name: "name"}}
}
func (freeGloss) Affinities() []string { return nil }
func (inst freeGloss) Bind(params map[string]string) (gloss.InstanceI, error) {
	return freeInstance{params: params}, nil
}

type freeInstance struct{ params map[string]string }

func (inst freeInstance) Gloss() gloss.GlossI                     { return freeGloss{} }
func (inst freeInstance) Params() map[string]string               { return inst.params }
func (inst freeInstance) Accepts(gloss.ValueKindE) (bool, string) { return true, "" }
func (inst freeInstance) Inline(cell gloss.CellI) gloss.Inline {
	return gloss.Inline{Text: cell.Text()}
}
