package play

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

func externalReadOp[T any](t *testing.T, h app.OperationsHandlerI, call app.OperationCall, name string, args any) (out T) {
	t.Helper()
	var raw []byte
	if args != nil {
		raw = mustEncode(t, args)
	}
	res, err := h.Snapshot().ExternalRead(call, name, raw)
	require.NoError(t, err)
	out, err = buscodec.Decode[T](res)
	require.NoError(t, err)
	return
}

func TestTheSchemaReadsAreExternalReads(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	require.NoError(t, m.Operations.Validate())
	for _, name := range []string{opValidateSql, opListTables, opDescribeTable} {
		spec, ok := m.Operations.Lookup(name)
		require.True(t, ok, name)
		assert.True(t, spec.Agents, name)
		assert.Equal(t, app.OperationClassExternalRead, spec.Class, name)
	}
}

// A catalog read reaches the endpoint, so an agent's needs it among the
// grant's destinations, and the refusal names it.
func TestACatalogReadNeedsTheEndpointDestination(t *testing.T) {
	l, h := opsLauncher(t)
	l.inner.client = NewClient(ClientConfig{URL: "http://ch.example:8123/"}, nil)
	call := app.OperationCall{Writer: "task:t", OnBehalfOf: &app.OnBehalfOf{Task: "t", Epoch: 1}}
	for _, name := range []string{opListTables, opDescribeTable} {
		_, err := h.Snapshot().ExternalRead(call, name, mustEncode(t, DescribeArgs{Table: "facts"}))
		var refusal *app.OperationRefusal
		require.ErrorAs(t, err, &refusal, name)
		assert.Equal(t, []string{"clickhouse:ch.example:8123"}, refusal.Destinations, name)
	}
}

// validate_sql checks the grammar without reaching anything; the rewrite,
// which reads the endpoint's catalog, waits for the destination, and a run's
// verdict comes with it.
func TestValidateSqlChecksWithoutRunning(t *testing.T) {
	l, h := opsLauncher(t)
	l.inner.client = NewClient(ClientConfig{URL: "http://ch.example:8123/"}, nil)
	bare := app.OperationCall{Writer: "task:t", OnBehalfOf: &app.OnBehalfOf{Task: "t", Epoch: 1}}

	bad := externalReadOp[ValidateResult](t, h, bare, opValidateSql, ValidateArgs{Sql: "SELEC 1 FRM t"})
	assert.False(t, bad.Valid)
	assert.NotEmpty(t, bad.Error)

	noDest := externalReadOp[ValidateResult](t, h, bare, opValidateSql, ValidateArgs{Sql: "SELECT 1"})
	assert.True(t, noDest.Valid)
	assert.False(t, noDest.Expanded)
	assert.Equal(t, "read", noDest.Kind)
	assert.Equal(t, []string{"clickhouse:ch.example:8123"}, noDest.Needs)

	granted := app.OperationCall{Writer: "task:t", OnBehalfOf: &app.OnBehalfOf{Task: "t", Epoch: 1,
		Destinations: []string{"clickhouse:ch.example:8123"}}}
	ok := externalReadOp[ValidateResult](t, h, granted, opValidateSql, nil) // the buffer: SELECT 1
	assert.True(t, ok.Valid, ok.Error)
	assert.True(t, ok.Expanded)
	assert.NotEmpty(t, ok.Sent)
	assert.Equal(t, "allowed", ok.Run)
	assert.Empty(t, ok.Needs)

	write := externalReadOp[ValidateResult](t, h, granted, opValidateSql, ValidateArgs{Sql: "INSERT INTO t VALUES (1)"})
	assert.NotEqual(t, "read", write.Kind)
	assert.NotEqual(t, "allowed", write.Run, "an agent's run is a plain read")
	assert.Equal(t, "SELECT 1", l.inner.sql, "the buffer is unchanged")
}

// trace_rewrite reports every step of the execute path in order, play's own
// included, with the body that would ship and the parameters lifted out of
// the prelude; it reaches the endpoint's catalog, so it needs the
// destination.
func TestTraceRewriteReportsEveryStep(t *testing.T) {
	l, h := opsLauncher(t)
	l.inner.client = NewClient(ClientConfig{URL: "http://ch.example:8123/"}, nil)
	bare := app.OperationCall{Writer: "task:t", OnBehalfOf: &app.OnBehalfOf{Task: "t", Epoch: 1}}
	_, err := h.Snapshot().ExternalRead(bare, opTraceRewrite, nil)
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)
	assert.Equal(t, []string{"clickhouse:ch.example:8123"}, refusal.Destinations)

	granted := app.OperationCall{Writer: "task:t", OnBehalfOf: &app.OnBehalfOf{Task: "t", Epoch: 1,
		Destinations: []string{"clickhouse:ch.example:8123"}}}
	tr := externalReadOp[RewriteTrace](t, h, granted, opTraceRewrite,
		TraceArgs{Sql: "SET param_n = 3; SELECT number FROM numbers({n:UInt8})"})
	assert.Empty(t, tr.ParseError)
	require.NotEmpty(t, tr.Steps)
	assert.Equal(t, rewriteStepExtractParams, tr.Steps[0].Name)
	assert.Equal(t, "play", tr.Steps[0].Kind)
	assert.True(t, tr.Steps[0].Changed, "the prelude is lifted out")
	assert.Equal(t, rewriteStepSetFormat, tr.Steps[len(tr.Steps)-1].Name)
	assert.Contains(t, tr.Body, "FORMAT ArrowStream")
	assert.NotContains(t, tr.Body, "SET param_n")
	assert.Equal(t, []TraceParam{{Name: "n", Value: "3"}}, tr.Params)
	assert.Equal(t, "ch.example:8123", tr.Target)
	assert.NotEmpty(t, tr.Summary)

	broken := externalReadOp[RewriteTrace](t, h, granted, opTraceRewrite, TraceArgs{Sql: "SELEC 1"})
	assert.NotEmpty(t, broken.ParseError)
}
