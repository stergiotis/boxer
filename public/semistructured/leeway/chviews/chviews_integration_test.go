//go:build integration

package chviews_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/chpack"
	"github.com/stergiotis/boxer/public/semistructured/leeway/chviews"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
)

// The views are installed here from chpack and chviews directly rather than
// through lwsqlsurface: that package imports this one, so a test in this
// package's own tree cannot reach it. The install ORDER this lane would
// otherwise cover — views dropped before functions are reconciled — is
// exercised where the installer lives.

func liveClient(t *testing.T) (client *chclient.Client, ctx context.Context) {
	t.Helper()
	if clickhouseenv.Endpoint.Get() == "" && clickhouseenv.URL.Get() == "" {
		t.Skip("no ClickHouse endpoint configured (CLICKHOUSE_ENDPOINT / CLICKHOUSE_URL); skipping")
	}
	client = chclient.New(chclient.ConfigFromEnv(), nil)
	c, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	require.NoError(t, client.Ping(c))
	return client, c
}

func query(t *testing.T, client *chclient.Client, ctx context.Context, sql string) (out string) {
	t.Helper()
	body, err := client.Query(ctx, sql)
	require.NoError(t, err, "query: %s", sql)
	defer func() { _ = body.Close() }()
	b, err := io.ReadAll(body)
	require.NoError(t, err)
	return strings.TrimRight(string(b), "\n")
}

// fixtureTable composes a leeway schema's physical names and creates a table
// carrying exactly those columns. The column types are irrelevant — the views
// decode names — so the table is built from the names directly rather than
// through the DDL generator, keeping the assertion about the decode.
func fixtureTable(t *testing.T, client *chclient.Client, ctx context.Context, database string, table string) (names []string, conv *ddl.HumanReadableNamingConvention) {
	t.Helper()
	manip, err := common.GetSystemTableColumnsManipulator()
	require.NoError(t, err)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "observedAt", ctabb.U64)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)

	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&td, clickhouse.NewTechnologySpecificCodeGenerator()))
	conv, err = ddl.NewHumanReadableNamingConvention(ddl.DefaultSeparator)
	require.NoError(t, err)

	phys := make([]common.PhysicalColumnDesc, 0, 64)
	for cc, cp := range ir.IterateColumnProps() {
		phys, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, phys, common.SystemTableColumnsTableRowConfig)
		require.NoError(t, err)
	}
	cols := make([]string, 0, len(phys))
	names = make([]string, 0, len(phys))
	for _, p := range phys {
		names = append(names, p.String())
		cols = append(cols, "`"+p.String()+"` String")
	}
	// One column this convention did not compose, so the foreign layout is a
	// row the assertions reach rather than a branch nothing exercises.
	cols = append(cols, "`hand_added` UInt8")

	require.NoError(t, client.Exec(ctx, "CREATE DATABASE IF NOT EXISTS "+database))
	require.NoError(t, client.Exec(ctx, "DROP TABLE IF EXISTS "+database+"."+table))
	require.NoError(t, client.Exec(ctx, "CREATE TABLE "+database+"."+table+" ("+
		strings.Join(cols, ", ")+") ENGINE = MergeTree ORDER BY tuple()"))
	t.Cleanup(func() {
		_ = client.Exec(context.Background(), "DROP TABLE IF EXISTS "+database+"."+table)
	})
	return names, conv
}

// TestIntegrationColumnsDecode is the claim the whole package rests on: the
// SQL decode of a physical name agrees, field by field, with what the Go
// convention extracts from the same name. It runs on a live server because
// the generated bodies are only SQL here — a segment index that is wrong in
// the same way on both sides of a Go-only test would still be wrong.
func TestIntegrationColumnsDecode(t *testing.T) {
	client, ctx := liveClient(t)
	target := chviews.TargetDatabase("leeway_it")

	for _, stmt := range chpack.Statements() {
		require.NoError(t, client.Exec(ctx, stmt))
	}
	for _, stmt := range chviews.Statements(target, chviews.Stamp("integration")) {
		require.NoError(t, client.Exec(ctx, stmt))
	}

	const database = "leeway_it_fixture"
	const table = "decode"
	names, conv := fixtureTable(t, client, ctx, database, table)
	phys, err := conv.ParseColumns(names)
	require.NoError(t, err)
	labels := lwsql.BuildLabels(names)

	// Tab-separated scalars only: an Array(String) renders as a quoted list
	// in TSV, and joining server-side keeps the comparison about the decode
	// rather than about a parser written for this test.
	rows := query(t, client, ctx, "SELECT name, layout, section, leeway_column, role, lane_kind, "+
		"canonical_type, handle, table_row_config, streaming_group, aspects_decodable, "+
		"arrayStringConcat(encoding_hints, ','), arrayStringConcat(value_semantics, ',') "+
		"FROM "+target.Qualified(chviews.ViewColumns)+
		" WHERE database = '"+database+"' AND table = '"+table+"' ORDER BY position")

	decoded := make(map[string][]string, len(names)+1)
	for _, line := range strings.Split(rows, "\n") {
		f := strings.Split(line, "\t")
		require.Len(t, f, 13, "row: %s", line)
		decoded[f[0]] = f
	}
	require.Len(t, decoded, len(names)+1, "one row per column, the hand-added one included")

	require.Equal(t, "foreign", decoded["hand_added"][1])
	require.Empty(t, decoded["hand_added"][7], "a foreign column has no handle")

	for i, name := range names {
		row, ok := decoded[name]
		require.True(t, ok, "no row for %s", name)
		require.Equal(t, "1", row[10], "aspect segments of %s did not decode", name)

		pit, e := conv.ExtractPlainItemType(phys[i])
		require.NoError(t, e)
		if pit == common.PlainItemTypeNone {
			require.Equal(t, "tagged", row[1], "layout of %s", name)
			section, e := conv.ExtractSectionName(phys[i])
			require.NoError(t, e)
			require.Equal(t, string(section), row[2], "section of %s", name)
			role, e := conv.ExtractColumnRole(phys[i])
			require.NoError(t, e)
			require.Equal(t, string(role), row[4], "role of %s", name)
			kind, _ := lwsql.ClassifyLaneRole(role)
			require.Equal(t, kind.String(), row[5], "lane kind of %s", name)
		} else {
			require.Equal(t, "plain", row[1], "layout of %s", name)
			require.Equal(t, ddl.PlainSectionName(pit), row[2], "section of %s", name)
			require.Empty(t, row[4], "a plain column carries no role: %s", name)
			require.Equal(t, lwsql.LaneKindValue.String(), row[5], "lane kind of %s", name)
		}

		column, e := conv.ExtractLeewayColumnName(phys[i])
		require.NoError(t, e)
		require.Equal(t, string(column), row[3], "column of %s", name)

		ct, e := conv.ExtractCanonicalType(phys[i])
		require.NoError(t, e)
		require.Equal(t, ct.String(), row[6], "canonical type of %s", name)

		require.Equal(t, labels[name], row[7], "handle of %s", name)

		cfg, e := conv.ExtractTableRowConfig(phys[i])
		require.NoError(t, e)
		require.Equal(t, cfg.String(), row[8], "row config of %s", name)

		sg, e := conv.ExtractStreamingGroup(phys[i])
		require.NoError(t, e)
		require.Equal(t, string(sg), row[9], "streaming group of %s", name)
	}
}

// TestIntegrationAggregates: the two aggregate views must select without a
// cyclic-alias or ambiguity error and must agree with the decode they group.
func TestIntegrationAggregates(t *testing.T) {
	client, ctx := liveClient(t)
	target := chviews.TargetDatabase("leeway_it")

	for _, stmt := range chpack.Statements() {
		require.NoError(t, client.Exec(ctx, stmt))
	}
	for _, stmt := range chviews.Statements(target, chviews.Stamp("integration")) {
		require.NoError(t, client.Exec(ctx, stmt))
	}

	const database = "leeway_it_fixture"
	const table = "aggregates"
	fixtureTable(t, client, ctx, database, table)
	where := " WHERE database = '" + database + "' AND name = '" + table + "'"

	shape := query(t, client, ctx, "SELECT name_shape, n_foreign_columns, n_sections, table_row_config, n_table_row_configs FROM "+
		target.Qualified(chviews.ViewTables)+where)
	f := strings.Split(shape, "\t")
	require.Len(t, f, 5)
	require.Equal(t, "mixed", f[0], "the fixture carries one hand-added column")
	require.Equal(t, "1", f[1])
	require.Equal(t, "1", f[4], "one row config across the table")

	sections := query(t, client, ctx, "SELECT count(), sum(n_columns) FROM "+
		target.Qualified(chviews.ViewSections)+
		" WHERE database = '"+database+"' AND table = '"+table+"'")
	g := strings.Split(sections, "\t")
	require.Len(t, g, 2)
	require.Equal(t, f[2], g[0], "leeway.tables and leeway.sections disagree on the section count")

	total := query(t, client, ctx, "SELECT countIf(layout != 'foreign') FROM "+
		target.Qualified(chviews.ViewColumns)+
		" WHERE database = '"+database+"' AND table = '"+table+"'")
	require.Equal(t, total, g[1], "the sections view drops leeway columns")
}

// TestIntegrationViewsInlineFunctionBodies pins the ClickHouse behaviour the
// stamp exists for (ADR-0226 §SD4): a SQL UDF is expanded INTO the view's
// stored query at CREATE time, so a view is a snapshot of the vocabulary it
// was created with and no amount of re-installing functions refreshes it.
//
// If a future ClickHouse resolved UDFs at read time instead, this test goes
// red and the stamp — and the rule that views are created last — stop being
// load-bearing. That is worth being told about.
func TestIntegrationViewsInlineFunctionBodies(t *testing.T) {
	client, ctx := liveClient(t)
	target := chviews.TargetDatabase("leeway_it")

	for _, stmt := range chpack.Statements() {
		require.NoError(t, client.Exec(ctx, stmt))
	}
	for _, stmt := range chviews.Statements(target, chviews.Stamp("integration")) {
		require.NoError(t, client.Exec(ctx, stmt))
	}

	stored := query(t, client, ctx, "SELECT create_table_query FROM system.tables WHERE database = '"+
		target.Name()+"' AND name = '"+chviews.ViewColumns+"'")
	require.NotContains(t, stored, "LW_ASPECT_NAMES_ENC",
		"the view kept the call; UDFs are no longer expanded at CREATE time")
	require.Contains(t, stored, "delta-encoding",
		"the view does not carry the inlined aspect vocabulary")
}
