//go:build integration

package jackstay

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
)

func findTable(plan *Plan, r datacatalog.TableRef) (pt *PlanTable) {
	for i := range plan.Tables {
		if plan.Tables[i].Source == r {
			return &plan.Tables[i]
		}
	}
	return nil
}

const (
	itFilterSource = "jackstay_it_filter_src"
	itFilterTarget = "jackstay_it_filter_dst"
	itPackTarget   = "jackstay_it_pack_dst"
)

// TestFilterAndPack_LiveServer runs the workflow of ADR-0271 against a real
// server: a filtered replace that must leave the rows outside its slice alone,
// and an export to a pack that a second plan syncs from with no source server
// in the loop.
func TestFilterAndPack_LiveServer(t *testing.T) {
	liveClient(t)
	client := chclient.New(chclient.ConfigFromEnv(), &http.Client{})
	ctx := context.Background()
	drop := func() {
		for _, db := range []string{itFilterSource, itFilterTarget, itPackTarget} {
			_ = client.Exec(context.Background(), "DROP DATABASE IF EXISTS "+db+" SYNC")
		}
	}
	drop()
	t.Cleanup(drop)
	exec := func(sqls ...string) {
		for _, sql := range sqls {
			require.NoError(t, client.Exec(ctx, sql), sql)
		}
	}
	count := func(sql string) uint64 {
		rows, err := queryRows[countRow](ctx, client, sql+jsonSettings)
		require.NoError(t, err, sql)
		return rows[0].N
	}
	s, d := itFilterSource+".", itFilterTarget+"."
	exec(
		"CREATE DATABASE "+itFilterSource,
		"CREATE DATABASE "+itFilterTarget,
		"CREATE TABLE "+s+"part (k UInt64, s String) ENGINE = MergeTree PARTITION BY k % 4 ORDER BY k",
		"INSERT INTO "+s+"part SELECT number, toString(number) FROM numbers(20000)",
		"CREATE TABLE "+d+"part AS "+s+"part",
		"INSERT INTO "+d+"part SELECT number, 'old' FROM numbers(20000)",
		"CREATE TABLE "+s+"rng (k UInt64, v Float64) ENGINE = MergeTree ORDER BY k",
		"INSERT INTO "+s+"rng SELECT number, number / 3 FROM numbers(6000)",
	)
	ep := SourceEndpoint()
	src := ServerSource(client)
	chunkOpts := DefaultChunkingOptions()
	chunkOpts.TargetChunkRows = 1000
	chunkOpts.TargetLeafRows = 256
	diffAll := func(src SourceI, plan *Plan, only ...datacatalog.TableRef) Plan {
		fresh, skipped, stale, err := DiffStep(ctx, src, client, plan, DiffOptionsAll{Chunking: chunkOpts, Diff: DefaultDiffOptions(), Only: only}, time.Now)
		require.NoError(t, err)
		require.Empty(t, stale)
		require.Empty(t, skipped)
		return fresh
	}
	sync := func(src SourceI, plan *Plan, ts TableSync, planPath string, only ...datacatalog.TableRef) SyncOutcome {
		prep, err := PrepareSyncStep(ctx, src, client, plan, SyncRequest{TableSync: ts, Chunking: chunkOpts, Only: only})
		require.NoError(t, err)
		require.Empty(t, prep.Stale)
		require.Empty(t, prep.Skipped)
		out, err := RunSync(ctx, src, client, &prep, planPath, true, DefaultSyncOptions(), time.Now)
		require.NoError(t, err)
		*plan = prep.Plan
		return out
	}

	t.Run("filtered replace leaves the rows outside the slice", func(t *testing.T) {
		sel := Selection{
			Databases:   []string{itFilterSource},
			DatabaseMap: map[string]string{itFilterSource: itFilterTarget},
			Filters:     map[string]string{itFilterSource + ".part": "k % 10 = 3"},
		}
		plan, err := PlanStructure(ctx, src, client, ep, ep, sel, nil, time.Now())
		require.NoError(t, err)
		// rng has no target table; only part is synced here.
		part := ref(itFilterSource, "part")
		pt := findTable(&plan, part)
		require.NotNil(t, pt)
		require.Equal(t, "k % 10 = 3", pt.Filter)

		planPath := filepath.Join(t.TempDir(), "plan.json")
		plan = diffAll(src, &plan, part)
		pt = findTable(&plan, part)
		assert.False(t, pt.Diff.IsIdentical(), "the target's slice holds 'old' values")
		assert.Equal(t, uint64(2000), pt.Diff.SrcRows, "the diff counts the slice only")
		assert.Equal(t, uint64(2000), pt.Diff.DstRows)

		out := sync(src, &plan, TableSync{Mode: SyncModeFull, Existing: ExistingPolicyReplace}, planPath, part)
		assert.Equal(t, 0, out.Failed, findTable(&plan, part).SyncReport.Problems)

		assert.Equal(t, uint64(18000), count("SELECT count() AS n FROM "+d+"part WHERE NOT (k % 10 = 3) AND s = 'old'"),
			"every row outside the filter is untouched")
		assert.Equal(t, uint64(2000), count("SELECT count() AS n FROM "+d+"part WHERE k % 10 = 3 AND s = toString(k)"))
		assert.Equal(t, uint64(20000), count("SELECT count() AS n FROM "+d+"part"))
		plan = diffAll(src, &plan, part)
		assert.True(t, findTable(&plan, part).Diff.IsIdentical())
	})

	t.Run("export, then sync from the pack", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "pack")
		req := ExportRequest{
			Selection:   Selection{Databases: []string{itFilterSource}, Filters: map[string]string{itFilterSource + ".rng": "k < 5000"}},
			SampleNum:   1,
			SampleDen:   2,
			Compression: "zstd",
			Chunking:    chunkOpts,
		}
		out, err := Export(ctx, client, ep, dir, req, time.Now)
		require.NoError(t, err)
		require.Zero(t, out.Failed)
		require.True(t, out.Manifest.Complete)
		require.Len(t, out.Manifest.Tables, 2)
		for _, pt := range out.Manifest.Tables {
			assert.Greater(t, len(pt.Chunks), 1, "%s: more than one chunk", pt.Source)
		}
		packRows := map[string]uint64{}
		for i := range out.Manifest.Tables {
			packRows[out.Manifest.Tables[i].Source.Name] = out.Manifest.Tables[i].Rows()
		}
		sampled := count("SELECT count() AS n FROM " + s + "rng WHERE k < 5000 AND cityHash64('jackstay-sample', tuple(k)) % 2 < 1")
		assert.Equal(t, sampled, packRows["rng"])

		// A second run resumes: nothing moved on the source, nothing is read again.
		copied := 0
		req.Progress = func(r ChunkResult) {
			if r.Status == ChunkStatusCopied {
				copied++
			}
		}
		again, err := Export(ctx, client, ep, dir, req, time.Now)
		require.NoError(t, err)
		assert.True(t, again.Resumed)
		assert.Zero(t, copied)
		req.SampleDen = 3
		_, err = Export(ctx, client, ep, dir, req, time.Now)
		assert.ErrorContains(t, err, "another sample")

		// The second host: the pack is the source.
		pack, err := OpenPack(dir)
		require.NoError(t, err)
		dstEp := ep
		plan, err := PlanStructure(ctx, pack, client, pack.Endpoint(), dstEp,
			Selection{Databases: []string{itFilterSource}, DatabaseMap: map[string]string{itFilterSource: itPackTarget}}, nil, time.Now())
		require.NoError(t, err)
		require.Len(t, plan.Tables, 2)
		for _, pt := range plan.Tables {
			assert.Equal(t, VerdictCreate, pt.Verdict)
			assert.NotEmpty(t, pt.Filter, "the pack's slice is the plan's")
		}
		guards, err := DDLGuardSettings(ctx, client)
		require.NoError(t, err)
		ddl := chclient.New(DDLClientConfig(chclient.ConfigFromEnv(), guards), nil)
		_, plan, stale, err := ApplyDDLStep(ctx, pack, client, ddl, &plan, time.Now())
		require.NoError(t, err)
		require.Empty(t, stale)

		planPath := filepath.Join(t.TempDir(), "plan.json")
		res := sync(pack, &plan, TableSync{Mode: SyncModeFull, Existing: ExistingPolicyRefuse}, planPath)
		assert.Equal(t, 0, res.Failed)
		assert.Equal(t, packRows["part"], count("SELECT count() AS n FROM "+itPackTarget+".part"))
		assert.Equal(t, packRows["rng"], count("SELECT count() AS n FROM "+itPackTarget+".rng"))
		plan = diffAll(pack, &plan)
		for _, pt := range plan.Tables {
			assert.True(t, pt.Diff.IsIdentical(), "%s: %+v", pt.Source, pt.Diff)
		}

		// A damaged chunk file fails before the target chunk is cleared.
		pt := findTable(&plan, ref(itFilterSource, "rng"))
		require.NotNil(t, pt)
		mt := pack.Manifest().Table(pt.Source)
		file := filepath.Join(dir, mt.Chunks[0].File)
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		data[len(data)/2] ^= 0xff
		require.NoError(t, os.WriteFile(file, data, 0o644))
		damaged, err := OpenPack(dir)
		require.NoError(t, err)
		before := count("SELECT count() AS n FROM " + itPackTarget + ".rng")
		prep, err := PrepareSyncStep(ctx, damaged, client, &plan, SyncRequest{TableSync: TableSync{Mode: SyncModeFull, Existing: ExistingPolicyReplace}, Only: []datacatalog.TableRef{pt.Source}, Chunking: chunkOpts})
		require.NoError(t, err)
		res, err = RunSync(ctx, damaged, client, &prep, planPath, true, DefaultSyncOptions(), time.Now)
		require.NoError(t, err)
		assert.Equal(t, 1, res.Failed)
		assert.Contains(t, findTable(&prep.Plan, pt.Source).SyncReport.Problems[0], "damaged")
		assert.Equal(t, before, count("SELECT count() AS n FROM "+itPackTarget+".rng"), "the damaged chunk's target rows are still there")
	})
}
