package play

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/analytics/timeseries/adscore"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	passregdefaults "github.com/stergiotis/boxer/public/keelson/data/passreg/defaults"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// A bound dataset reaches the residual as its handle, and the grant names it
// by its alias: the limits read one as the other, and a refusal asks for the
// alias, the only name the person knows.
func TestAgentLimitsReadABoundDatasetByItsAlias(t *testing.T) {
	c := NewClient(ClientConfig{URL: "http://ch.example:8123/"}, nil)
	c.bindDataset("chat_turns", "ds_7f3a")
	sql := "SELECT * FROM keelson('chat_turns')"
	residual, _ := c.buildResidual(sql)
	require.Contains(t, residual, "ds_7f3a")

	err := checkAgentLimits(residual, c.previewDispatch(residual, ""), &app.OnBehalfOf{}, c.datasetAliasOf())
	var limit *AgentLimitError
	require.ErrorAs(t, err, &limit)
	assert.Equal(t, "keelson:chat_turns", limit.Destination)

	require.NoError(t, checkAgentLimits(residual, c.previewDispatch(residual, ""),
		&app.OnBehalfOf{Destinations: []string{"keelson:chat_turns", "clickhouse:ch.example:8123"}}, c.datasetAliasOf()))
}

// offlineClient carries the standard pass set and the leeway resolver, so
// its late-bound steps are the ones a grant without the endpoint leaves out.
func offlineClient(t *testing.T) (c *Client) {
	t.Helper()
	reg := passreg.NewRegistry()
	require.NoError(t, passregdefaults.RegisterStandard(reg))
	c = NewClient(ClientConfig{URL: "http://ch.example:8123/"}, nil)
	c.passes = reg
	installLeewayNameResolution(c)
	return
}

// Without the endpoint in the grant, validate_sql and trace_rewrite make the
// rewrite without the steps that read its catalog — LW_ID_* still expands —
// and name the late-bound ones and the destination the rest needs.
func TestTheRewriteWithoutTheEndpointLeavesOutOnlyTheCatalogSteps(t *testing.T) {
	c := offlineClient(t)
	obo := &app.OnBehalfOf{Task: "t", Epoch: 1}
	stmt := "SELECT LW_ID_BODY(toUInt64(7)) AS b"

	v, err := validateStatement(c, obo, stmt)
	require.NoError(t, err)
	assert.True(t, v.Valid, v.Error)
	assert.False(t, v.Expanded)
	assert.NotContains(t, v.Sent, "LW_ID_BODY", "an unbound macro still expands")
	assert.Contains(t, v.Declined, "LwExtractExpand")
	assert.Contains(t, v.Declined, "ResolveColumnNames")
	assert.Equal(t, []string{"clickhouse:ch.example:8123"}, v.Needs)

	tr := traceRewrite(c, stmt, false, false)
	assert.False(t, tr.Expanded)
	assert.Equal(t, []string{"clickhouse:ch.example:8123"}, tr.Needs)
	outcomes := map[string]string{}
	for _, s := range tr.Steps {
		outcomes[s.Name] = s.Outcome
	}
	assert.Equal(t, "declined", outcomes["LwExtractExpand"])
	assert.Equal(t, "applied", outcomes["ExpandLwIdMacros"])

	// Under Auto, a statement the introspection plane serves needs no
	// endpoint at all; what it needs is the table.
	c.SetResolver(keelsonResolver{localEndpoint: func() string { return "http://127.0.0.1:1/query" }})
	k, err := validateStatement(c, obo, "SELECT name FROM keelson('env')")
	require.NoError(t, err)
	assert.Equal(t, []string{"keelson:env"}, k.Needs)
}

// list_datasets over the real plane: the alias, the destination, and — for
// a sealed dataset, asked for by alias — its columns, labelled confined.
func TestListDatasetsEndToEnd(t *testing.T) {
	svc, _ := adhocReadPlane(t)
	// The host installs the sealed-name predicate beside the plane it starts
	// (introspecthost); every ad-hoc dataset answers yes (ADR-0240).
	introspect.SetLocalSealedPredicate(func(name string) bool { return strings.HasPrefix(name, "adhoc_") })
	t.Cleanup(func() { introspect.SetLocalSealedPredicate(nil) })
	fixture, err := generateFixture(fixtureSpec{kind: adscore.AllAnomalyKinds[0], seed: 7})
	require.NoError(t, err)
	ipc, err := encodeFixtureSeries(fixture, memory.NewGoAllocator())
	require.NoError(t, err)
	pub, err := svc.Publish(adhocdata.PublishInput{Alias: fixtureSeriesAlias, ArrowIPCStream: ipc})
	require.NoError(t, err)
	// Pinned elsewhere, under Auto: keelson() goes to the plane, as the
	// endpoint switcher's Auto routes it.
	c := NewClient(ClientConfig{URL: "http://ch.example:8123/"}, nil)
	c.SetResolver(autoResolver)
	c.bindDataset(fixtureSeriesAlias, pub.Handle)

	bare := &app.OnBehalfOf{Task: "t", Epoch: 1}
	all, err := listDatasets(c, bare, "")
	require.NoError(t, err)
	require.Len(t, all.Datasets, 1)
	d := all.Datasets[0]
	assert.Equal(t, fixtureSeriesAlias, d.Alias)
	assert.Equal(t, "keelson:"+fixtureSeriesAlias, d.Destination)
	assert.False(t, d.Granted)
	assert.Empty(t, d.Columns)
	assert.False(t, all.ResultConfined())

	granted := &app.OnBehalfOf{Task: "t", Epoch: 1, Destinations: []string{d.Destination}}
	one, err := listDatasets(c, granted, fixtureSeriesAlias)
	require.NoError(t, err)
	require.Len(t, one.Datasets, 1)
	got := one.Datasets[0]
	require.Empty(t, got.Error)
	assert.True(t, got.Granted)
	names := []string{}
	for _, col := range got.Columns {
		names = append(names, col.Name)
	}
	assert.Contains(t, names, "t")
	assert.Contains(t, names, "v")
	assert.True(t, got.Confined, "an ad-hoc dataset is sealed (ADR-0240)")
	assert.True(t, one.ResultConfined(), "a sealed dataset's columns label the result")
	assert.True(t, d.Confined)
	assert.Empty(t, d.Columns, "the listing never carries a sealed dataset's columns")

	_, err = listDatasets(c, granted, "nope")
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)
}
