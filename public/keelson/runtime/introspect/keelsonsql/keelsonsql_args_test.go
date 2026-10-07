package keelsonsql

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// seqProvider takes one required argument n and an optional label.
type seqProvider struct{}

func (seqProvider) Name() string                         { return "seq" }
func (seqProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (seqProvider) Schema() *arrow.Schema {
	return introspect.NewTable().Uint64("v", func(int) uint64 { return 0 }).Schema()
}
func (seqProvider) Snapshot(introspect.Projection) (arrow.RecordBatch, error) {
	return nil, assert.AnError
}
func (seqProvider) Args() []introspect.ArgSpec {
	return []introspect.ArgSpec{
		{Name: "n", Type: introspect.ArgUInt64, Required: true},
		{Name: "label", Type: introspect.ArgString, Default: "x"},
	}
}
func (seqProvider) SnapshotArgs(proj introspect.Projection, args introspect.Args) (arrow.RecordBatch, error) {
	n := int(args.UInt64("n"))
	return introspect.NewTable().Uint64("v", func(i int) uint64 { return uint64(i + 1) }).Build(proj, n), nil
}

func argsReg(t *testing.T) *introspect.Registry {
	t.Helper()
	r := testReg(t)
	require.NoError(t, r.Register(seqProvider{}))
	return r
}

func TestExpandWithArgs_LiteralsAndSlots(t *testing.T) {
	r := argsReg(t)
	got, calls, err := ExpandWithArgs(r, "",
		"SELECT * FROM keelson('seq', n = 3) AS a JOIN keelson('seq', n = {k:UInt64}, label = 'it''s') AS b ON 1 JOIN keelson('env') AS e ON 1",
		map[string]string{"k": "5"})
	require.NoError(t, err)
	require.Len(t, calls, 2)
	assert.Equal(t, "seq", calls[0].Table)
	assert.Equal(t, map[string]string{"n": "3"}, calls[0].Raw)
	assert.Equal(t, map[string]string{"n": "5", "label": "it's"}, calls[1].Raw)
	assert.NotEqual(t, calls[0].Temp, calls[1].Temp, "different values, different tables")
	assert.True(t, introspect.ValidTableName(calls[0].Temp))
	assert.Equal(t, "SELECT * FROM "+calls[0].Temp+" AS a JOIN "+calls[1].Temp+" AS b ON 1 JOIN env AS e ON 1", got)
}

func TestExpandWithArgs_SameValuesShareATable(t *testing.T) {
	r := argsReg(t)
	got, calls, err := ExpandWithArgs(r, "",
		"SELECT * FROM keelson('seq', n = 3) UNION ALL SELECT * FROM keelson('seq', label = 'x', n = {k:UInt64})",
		map[string]string{"k": "3"})
	require.NoError(t, err)
	require.Len(t, calls, 1, "n=3 with the default label twice is one table")
	assert.Equal(t, 2, strings.Count(got, calls[0].Temp))
}

func TestExpandWithArgs_NegativeAndStringLiterals(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, r.Register(signedProvider{}))
	_, calls, err := ExpandWithArgs(r, "", `SELECT * FROM keelson('signed', i = -4, s = 'a\'b')`, nil)
	require.NoError(t, err)
	require.Len(t, calls, 1)
	assert.Equal(t, map[string]string{"i": "-4", "s": "a'b"}, calls[0].Raw)
}

func TestExpandWithArgs_Refusals(t *testing.T) {
	r := argsReg(t)
	for name, sql := range map[string]string{
		"unbound slot":         "SELECT * FROM keelson('seq', n = {k:UInt64})",
		"positional":           "SELECT * FROM keelson('seq', 3)",
		"twice":                "SELECT * FROM keelson('seq', n = 1, n = 2)",
		"expression value":     "SELECT * FROM keelson('seq', n = 1 + 2)",
		"null value":           "SELECT * FROM keelson('seq', n = NULL)",
		"undeclared":           "SELECT * FROM keelson('seq', n = 1, m = 2)",
		"missing required":     "SELECT * FROM keelson('seq', label = 'y')",
		"ill-typed":            "SELECT * FROM keelson('seq', n = 'many')",
		"plain table and args": "SELECT * FROM keelson('env', n = 1)",
		"comparison":           "SELECT * FROM keelson('seq', n > 1)",
	} {
		_, _, err := ExpandWithArgs(r, "", sql, nil)
		assert.Error(t, err, name)
	}
}

func TestPassesWithoutParamsRefuseArguments(t *testing.T) {
	r := argsReg(t)
	const in = "SELECT * FROM keelson('seq', n = 3)"
	_, err := RewriteToBare(r, in)
	assert.Error(t, err)
	_, err = RewriteToURL(r, "http://127.0.0.1:1", in)
	assert.Error(t, err)
}

func TestReferencesSeesCallsWithArguments(t *testing.T) {
	assert.Equal(t, []string{"seq", "env"}, References("SELECT * FROM keelson('seq', n = {k:UInt64}) JOIN keelson('env') ON 1"))
}

func TestRewriteAliasesKeepsArguments(t *testing.T) {
	got := RewriteAliases("SELECT * FROM keelson('items', n = {k:UInt64})", map[string]string{"items": "adhoc_deadbeef01234567"})
	assert.Equal(t, "SELECT * FROM keelson('adhoc_deadbeef01234567', n = {k:UInt64})", got)
}

// signedProvider takes a signed integer and a string.
type signedProvider struct{}

func (signedProvider) Name() string                         { return "signed" }
func (signedProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (signedProvider) Schema() *arrow.Schema {
	return introspect.NewTable().Int64("i", func(int) int64 { return 0 }).Schema()
}
func (signedProvider) Snapshot(introspect.Projection) (arrow.RecordBatch, error) {
	return nil, assert.AnError
}
func (signedProvider) Args() []introspect.ArgSpec {
	return []introspect.ArgSpec{
		{Name: "i", Type: introspect.ArgInt64, Required: true},
		{Name: "s", Type: introspect.ArgString, Required: true},
	}
}
func (signedProvider) SnapshotArgs(proj introspect.Projection, args introspect.Args) (arrow.RecordBatch, error) {
	return introspect.NewTable().Int64("i", func(int) int64 { return args.Int64("i") }).Build(proj, 1), nil
}
