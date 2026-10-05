//go:build integration

package play

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	passregdefaults "github.com/stergiotis/boxer/public/keelson/data/passreg/defaults"
)

// The catalog reads against a live endpoint: system.tables and
// system.columns answer in the shape the operations decode, and a leeway
// table is described by its sections and handles rather than its physical
// names. The leeway half needs a leeway table on the endpoint; it skips
// without one.
func TestLiveSchemaReads(t *testing.T) {
	c := NewClient(ClientConfig{URL: liveClickHouseURL(t)}, nil)
	installLeewayNameResolution(c)
	ctx := context.Background()

	list, err := listTables(ctx, c, TablesArgs{Database: "system", Search: "columns"})
	require.NoError(t, err)
	require.NotEmpty(t, list.Tables)
	assert.Equal(t, "system", list.Database)

	plain, err := describeTable(ctx, c, "system.one")
	require.NoError(t, err)
	assert.False(t, plain.Leeway)
	require.Len(t, plain.Columns, 1)
	assert.Equal(t, "dummy", plain.Columns[0].Name)

	_, err = describeTable(ctx, c, "system.no_such_table")
	require.Error(t, err)

	var leeway TableInfo
	var db string
	all, err := listTables(ctx, c, TablesArgs{})
	require.NoError(t, err)
	for _, d := range append([]string{all.Database}, all.Databases...) {
		l, lerr := listTables(ctx, c, TablesArgs{Database: d})
		require.NoError(t, lerr)
		for _, tb := range l.Tables {
			if tb.Leeway {
				leeway, db = tb, d
				break
			}
		}
		if db != "" {
			break
		}
	}
	if db == "" {
		t.Skip("no leeway table on the endpoint")
	}
	desc, err := describeTable(ctx, c, db+"."+leeway.Name)
	require.NoError(t, err)
	require.True(t, desc.Leeway, db+"."+leeway.Name)
	require.NotEmpty(t, desc.Sections)
	assert.Positive(t, desc.Physical)
	for _, s := range desc.Sections {
		for _, v := range s.Values {
			assert.True(t, strings.HasPrefix(v.Handle, s.Name+":"), v.Handle)
			assert.NotEmpty(t, v.Type, v.Handle)
		}
	}
	for _, col := range desc.Columns {
		assert.NotContains(t, col.Name, ":", "a physical name is left out")
	}
}

// validate_sql over a live leeway table, with the standard pass set the host
// wires: a handle resolves into the physical name it ships as, and a
// misspelt one is reported with candidates rather than shipped silently.
func TestLiveValidateResolvesHandles(t *testing.T) {
	reg := passreg.NewRegistry()
	require.NoError(t, passregdefaults.RegisterStandard(reg))
	c := NewClient(ClientConfig{URL: liveClickHouseURL(t)}, nil)
	c.passes = reg
	installLeewayNameResolution(c)
	desc, err := describeTable(context.Background(), c, "anchor.facts")
	if err != nil || !desc.Leeway {
		t.Skip("no leeway table anchor.facts on the endpoint")
	}
	ok, err := validateStatement(c, nil, "SELECT `id:id` FROM anchor.facts LIMIT 3")
	require.NoError(t, err)
	assert.True(t, ok.Valid, ok.Error)
	assert.NotContains(t, ok.Sent, "`id:id`", "the handle ships as its physical name")

	bad, err := validateStatement(c, nil, "SELECT `geoPoint:lat` FROM anchor.facts")
	require.NoError(t, err)
	assert.False(t, bad.Valid)
	require.Len(t, bad.Handles, 1)
	assert.Contains(t, bad.Handles[0], "geoPoint:lat")
}

// trace_rewrite over a live leeway table with the standard pass set: LW_GET
// and the handles are late-bound steps that rewrote the statement, each with
// a time, and the breakdown names the passes inside a step.
func TestLiveTraceRewrite(t *testing.T) {
	reg := passreg.NewRegistry()
	require.NoError(t, passregdefaults.RegisterStandard(reg))
	c := NewClient(ClientConfig{URL: liveClickHouseURL(t)}, nil)
	c.passes = reg
	installLeewayNameResolution(c)
	desc, err := describeTable(context.Background(), c, "anchor.facts")
	if err != nil || !desc.Leeway {
		t.Skip("no leeway table anchor.facts on the endpoint")
	}
	tr := traceRewrite(c, nil, "SELECT `id:id`, LW_GET('symbol', 22, 'chan:low-card-ref') AS x FROM anchor.facts LIMIT 3", true)
	byName := map[string]TraceStep{}
	for _, s := range tr.Steps {
		byName[s.Name] = s
	}
	for _, name := range []string{"LwExtractExpand", "ResolveColumnNames"} {
		s, ok := byName[name]
		require.True(t, ok, name)
		assert.Equal(t, "late-bound", s.Kind, name)
		assert.Equal(t, "applied", s.Outcome, name)
		assert.True(t, s.Changed, name)
		assert.NotEmpty(t, s.Description, name)
		assert.NotEmpty(t, s.Costs, name)
	}
	assert.Contains(t, tr.Body, "LW_VALUE_BY_TAG_EQUAL")
	assert.NotContains(t, tr.Body, "`id:id`")
	assert.Positive(t, tr.Micros)
}
