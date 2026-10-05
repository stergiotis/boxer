//go:build integration

package jackstay

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
)

// The scratch databases this lane owns, named for the package so a concurrent
// member of the lane cannot collide with them on the shared server. Source and
// target are one server; the database map keeps them apart.
const (
	itSource = "jackstay_it_src"
	itTarget = "jackstay_it_dst"
)

func liveClient(t *testing.T) *chclient.Client {
	t.Helper()
	cfg := chclient.ConfigFromEnv()
	client := chclient.New(cfg, nil)
	if err := client.Ping(context.Background()); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", cfg.URL, err)
	}
	return client
}

// TestStructure_LiveServer runs discover → structure → apply → discover against
// a real server. It covers a table the target lacks, one it has with a column
// missing, a leeway table whose DDL needs a guard setting, and a view.
func TestStructure_LiveServer(t *testing.T) {
	client := liveClient(t)
	ctx := context.Background()
	drop := func() {
		_ = client.Exec(context.Background(), "DROP DATABASE IF EXISTS "+itSource+" SYNC")
		_ = client.Exec(context.Background(), "DROP DATABASE IF EXISTS "+itTarget+" SYNC")
	}
	drop()
	t.Cleanup(drop)

	guards, err := DDLGuardSettings(ctx, client)
	require.NoError(t, err)
	ddl := chclient.New(DDLClientConfig(chclient.ConfigFromEnv(), guards), nil)
	for _, sql := range []string{
		"CREATE DATABASE " + itSource,
		"CREATE TABLE " + itSource + ".plain (k UInt64, s String, n UInt32 CODEC(ZSTD(1))) ENGINE = MergeTree PARTITION BY k % 4 ORDER BY k",
		"CREATE TABLE " + itSource + ".grown (k UInt64, s String, added Float64 DEFAULT 1.5) ENGINE = MergeTree ORDER BY k",
		"CREATE TABLE " + itSource + ".silver (`id:id:u64:47::0:` UInt64, `id:naturalKey:y:4::0:` String, " +
			"`tv:symbol:value:val:s:124::I:0::data` Array(LowCardinality(String)), `tv:symbol:lr:lr:u64:1247:::0::data` Array(LowCardinality(UInt64)), " +
			"`tv:symbol:lrcard:lrcard:u64:4E:::0::data` Array(UInt64)) ENGINE = MergeTree ORDER BY `id:id:u64:47::0:`",
		"CREATE VIEW " + itSource + ".v AS SELECT k FROM " + itSource + ".plain",
		"CREATE DATABASE " + itTarget,
		"CREATE TABLE " + itTarget + ".grown (k UInt64, s String) ENGINE = MergeTree ORDER BY k",
	} {
		require.NoError(t, ddl.Exec(ctx, sql), sql)
	}

	ops := newOps(t)
	ep := SourceEndpoint()
	sel := Selection{Databases: []string{itSource}, DatabaseMap: map[string]string{itSource: itTarget}}
	discoverBoth := func() (src Inventory, dst Inventory) {
		var err error
		src, err = Discover(ctx, client)
		require.NoError(t, err)
		dst = src
		return
	}
	src, dst := discoverBoth()
	assert.NotEmpty(t, src.Server.Version)
	plan, err := BuildPlan(ops, ep, ep, &src, &dst, sel, time.Now())
	require.NoError(t, err)

	byName := func(p *Plan) map[string]*PlanTable {
		m := make(map[string]*PlanTable, len(p.Tables))
		for i := range p.Tables {
			m[p.Tables[i].Source.Name] = &p.Tables[i]
		}
		return m
	}
	before := byName(&plan)
	require.Len(t, before, 4)
	assert.Equal(t, VerdictCreate, before["plain"].Verdict)
	assert.Equal(t, VerdictExtend, before["grown"].Verdict)
	assert.Equal(t, []string{"k", "s", "added"}, before["grown"].CopyColumns)
	assert.Equal(t, VerdictCreate, before["silver"].Verdict)
	assert.True(t, before["silver"].Leeway)
	assert.Equal(t, VerdictUnsupported, before["v"].Verdict)
	assert.Empty(t, plan.DatabaseDDL, "the target database exists")

	src, dst = discoverBoth()
	pending, stale, err := Restate(ops, &plan, &src, &dst, time.Now())
	require.NoError(t, err)
	require.Empty(t, stale)
	_, err = ApplyStructure(ctx, ddl, &pending)
	require.NoError(t, err)

	src, dst = discoverBoth()
	after, err := BuildPlan(ops, ep, ep, &src, &dst, sel, time.Now())
	require.NoError(t, err)
	got := byName(&after)
	assert.Equal(t, VerdictIdentical, got["plain"].Verdict, "%v", got["plain"].Reasons)
	assert.Equal(t, VerdictIdentical, got["grown"].Verdict, "%v", got["grown"].Reasons)
	assert.Equal(t, VerdictIdentical, got["silver"].Verdict, "%v", got["silver"].Reasons)
	assert.Equal(t, "equal", got["silver"].LeewayRelation)
	assert.False(t, after.HasPendingDDL())

	// Applying the same plan again is progress, not staleness, and runs nothing.
	_, stale, err = Restate(ops, &plan, &src, &dst, time.Now())
	require.NoError(t, err)
	assert.Empty(t, stale)
}
