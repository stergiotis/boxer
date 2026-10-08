package introspectengine

import (
	"context"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// seqProvider is a table defined by its argument: n rows, v = 1..n.
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
	return []introspect.ArgSpec{{Name: "n", Type: introspect.ArgTypeUInt64, Required: true}}
}
func (seqProvider) SnapshotArgs(proj introspect.Projection, args introspect.Args) (arrow.RecordBatch, error) {
	n := int(args.UInt64("n"))
	return introspect.NewTable().Uint64("v", func(i int) uint64 { return uint64(i + 1) }).Build(proj, n), nil
}

// One statement reads the same table twice with different arguments — one a
// literal, one bound from a query parameter — and each call sees its own rows
// (ADR-0290 §SD2).
func TestQuery_NamedArgumentsAreTwoTables(t *testing.T) {
	e := newEngineWithBroker(t)
	require.NoError(t, e.reg.Register(seqProvider{}))
	body, _, err := e.QueryParams(context.Background(),
		"SELECT (SELECT sum(v) FROM keelson('seq', n = 3)), (SELECT sum(v) FROM keelson('seq', n = {k:UInt64}))",
		"TabSeparated", map[string]string{"k": "5"})
	require.NoError(t, err)
	assert.Equal(t, "6\t15", strings.TrimSpace(string(body)))
}

func TestQuery_NamedArgumentRefusalNamesIt(t *testing.T) {
	e := newEngineWithBroker(t)
	require.NoError(t, e.reg.Register(seqProvider{}))
	_, _, err := e.QueryParams(context.Background(), "SELECT * FROM keelson('seq', n = {k:UInt64})", "TabSeparated", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not bound")
}
