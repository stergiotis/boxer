package play

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlcomplete"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
)

// Every operation of the step, as the catalog declares it.
func TestCompletionStateGraphHistoryCatalogEntries(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	type want struct {
		class     app.OperationClassE
		effect    app.OperationEffectE
		untrusted bool
		reads     []string
		writes    []string
		gesture   bool
	}
	for name, w := range map[string]want{
		opCompleteSql:       {app.OperationClassQuery, app.OperationEffectNone, true, []string{opsResSql}, nil, false},
		opValidateSql:       {app.OperationClassExternalRead, app.OperationEffectNone, true, []string{opsResSql}, nil, false},
		opEndpointFunctions: {app.OperationClassExternalRead, app.OperationEffectNone, true, nil, nil, false},
		opGetState:          {app.OperationClassQuery, app.OperationEffectNone, true, []string{opsResSql, opsResParams, opsResSignals, opsResResult, opsResPanes, opsResBundle, opsResFollowed}, nil, false},
		opSetParam:          {app.OperationClassCommand, app.OperationEffectDocument, false, nil, []string{opsResParams}, true},
		opGetQueryGraph:     {app.OperationClassQuery, app.OperationEffectNone, true, []string{opsResSql, opsResResult, opsResPanes, opsResSignals}, nil, false},
		opObserveNode:       {app.OperationClassCommand, app.OperationEffectDocument, false, []string{opsResSql}, []string{opsResPanes}, true},
		opListHistory:       {app.OperationClassQuery, app.OperationEffectNone, true, []string{opsResResult}, nil, false},
	} {
		spec, ok := m.Operations.Lookup(name)
		require.True(t, ok, name)
		assert.Equal(t, w.class, spec.Class, name)
		assert.Equal(t, w.effect, spec.Effect, name)
		assert.Equal(t, w.untrusted, spec.Untrusted, name)
		assert.True(t, spec.Agents, name)
		assert.Equal(t, w.reads, spec.Reads, name)
		assert.Equal(t, w.writes, spec.Writes, name)
		assert.Equal(t, w.gesture, spec.Gesture != "", name)
	}
	for name, version := range map[string]int{opGetState: 2, opSetParam: 2, opValidateSql: 3} {
		spec, _ := m.Operations.Lookup(name)
		assert.Equal(t, version, int(spec.Version), name)
	}
}

// registerTestComponents does what a host's wiring does for the default
// registries the operations read: the components and the vocabulary.
func registerTestComponents(t *testing.T) {
	t.Helper()
	componentsql.Default.Reset()
	t.Cleanup(componentsql.Default.Reset)
	require.NoError(t, RegisterComponents(componentsql.Default))
	sqlvocab.Default.Reset()
	t.Cleanup(sqlvocab.Default.Reset)
	require.NoError(t, RegisterVocabulary(sqlvocab.Default))
}

// complete_sql answers an in-process domain at an offset of a draft, and
// marks an endpoint listing no pane has probed as not ready rather than
// starting it; once the pane's probe lands, the next snapshot answers.
func TestCompleteSqlAnswersAtAnOffsetAndNeverProbes(t *testing.T) {
	registerTestComponents(t)
	l, h := opsLauncher(t)

	out := queryOp[CompleteResult](t, h, opCompleteSql, CompleteArgs{Sql: "SELECT LW_COMPONENT('Sys"})
	assert.Contains(t, out.Domain, "kind")
	assert.Equal(t, "LW_COMPONENT", out.Callee)
	assert.Equal(t, int32(1), out.Argument)
	assert.Equal(t, "Sys", out.Typed)
	assert.Equal(t, "prefix", out.Match)
	texts := make([]string, 0, len(out.Candidates))
	for _, c := range out.Candidates {
		texts = append(texts, c.Text)
	}
	assert.Contains(t, texts, "SysMem")
	assert.Empty(t, out.NotReady)

	sql := "SELECT LW_COMPONENT('SysMem') AS m FROM t"
	off := int32(len("SELECT LW_COMPONENT('SysMem"))
	exact := queryOp[CompleteResult](t, h, opCompleteSql, CompleteArgs{Sql: sql, Offset: &off})
	assert.Equal(t, "exact", exact.Match)
	assert.Equal(t, "SysMem", exact.Exact)
	assert.Equal(t, off, exact.Offset)

	// The table position needs the endpoint's listing.
	tables := queryOp[CompleteResult](t, h, opCompleteSql, CompleteArgs{Sql: "SELECT a FROM "})
	assert.Empty(t, tables.Candidates)
	assert.Contains(t, tables.NotReady, "tables")
	assert.Nil(t, l.inner.completion.catalog, "the read starts no probe")

	l.inner.completion.catalog = newCatalogProbe(nil)
	l.inner.completion.catalog.memo["tables\x00"] = []sqlcomplete.Item{{Text: "events", Kind: sqlcomplete.ItemTable, Source: "system.tables"}}
	tables = queryOp[CompleteResult](t, h, opCompleteSql, CompleteArgs{Sql: "SELECT a FROM "})
	require.Len(t, tables.Candidates, 1)
	assert.Equal(t, "events", tables.Candidates[0].Text)
	assert.Empty(t, tables.NotReady)

	// The buffer is the default statement.
	l.inner.sql = "SELECT LW_COMPONENT('SysM"
	buf := queryOp[CompleteResult](t, h, opCompleteSql, CompleteArgs{})
	assert.Equal(t, "SysM", buf.Typed)

	bad := int32(999)
	_, err := h.Snapshot().Query(opCompleteSql, mustEncode(t, CompleteArgs{Sql: "SELECT 1", Offset: &bad}))
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)
}

// validate_sql reports the literals the editor underlines in red, with
// candidates, and needs no endpoint for them.
func TestValidateSqlReportsUnknownLiterals(t *testing.T) {
	registerTestComponents(t)
	_, h := opsLauncher(t)
	v := externalReadOp[ValidateResult](t, h, app.OperationCall{}, opValidateSql,
		ValidateArgs{Sql: "SELECT LW_COMPONENT('SysMem') AS a, LW_COMPONENT('Nonesuch') AS b"})
	require.Len(t, v.Literals, 1)
	assert.Contains(t, v.Literals[0], "'Nonesuch'")
	assert.Contains(t, v.Literals[0], "candidates:")
	assert.False(t, v.Valid)

	ok := externalReadOp[ValidateResult](t, h, app.OperationCall{}, opValidateSql, ValidateArgs{Sql: "SELECT LW_COMPONENT('SysMem') AS a"})
	assert.Empty(t, ok.Literals)
	assert.True(t, ok.Valid, ok.Error)
}

// endpoint_functions asks the endpoint under the grant, reads the surface
// marker and the extras, and leaves the pane's probe alone.
func TestEndpointFunctionsAsksTheEndpointUnderTheGrant(t *testing.T) {
	ep := newStubEndpoint(t, []byte(`{"name":"`+lwsqlsurface.VersionFunctionName+`","def":"CREATE FUNCTION `+lwsqlsurface.VersionFunctionName+` AS () -> 1"}
{"name":"myUdf","def":""}
`))
	registerTestComponents(t)
	l, h := opsLauncher(t)
	l.inner.client = NewClient(ClientConfig{URL: ep.srv.URL}, ep.srv.Client())

	agent := app.OperationCall{Writer: "task:t", OnBehalfOf: &app.OnBehalfOf{Task: "t", Epoch: 1}}
	_, _, err := h.Snapshot().ExternalRead(agent, opEndpointFunctions, nil)
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)
	assert.Equal(t, []string{ep.destination()}, refusal.Destinations)
	bodies, _ := ep.sent()
	assert.Empty(t, bodies)

	agent.OnBehalfOf.Destinations = []string{ep.destination()}
	out := externalReadOp[EndpointFunctions](t, h, agent, opEndpointFunctions, nil)
	assert.Equal(t, int32(2), out.UserDefined)
	assert.Equal(t, int32(1), out.SurfaceVersion)
	assert.NotEmpty(t, out.Skew)
	assert.Contains(t, out.Undeclared, "myUdf")
	assert.NotEmpty(t, out.Missing, "the build's server functions are not there")
	bodies, params := ep.sent()
	require.Len(t, bodies, 1)
	assert.Contains(t, bodies[0], "system.functions")
	assert.Contains(t, params[0], "param_v=")
	_, ready := l.inner.vocab.known()
	assert.False(t, ready, "the pane's probe is not touched")
}

// An endpoint with no user-defined functions breaks every client macro
// that expands into one.
func TestEndpointFunctionsOfAnEmptyEndpoint(t *testing.T) {
	registerTestComponents(t)
	out := endpointFunctionsOf("clickhouse:x", map[string]string{})
	assert.Zero(t, out.UserDefined)
	assert.Zero(t, out.SurfaceVersion)
	assert.Contains(t, out.Skew, "no surface marker")
	assert.Zero(t, out.Installed)
	assert.NotEmpty(t, out.Missing)
	for _, g := range out.MacrosBroken {
		assert.NotEmpty(t, g.Missing, g.Name)
	}
}
