//go:build integration

package jackstay

import (
	"context"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
)

const (
	itMonSource = "jackstay_it_mon_src"
	itMonTarget = "jackstay_it_mon_dst"
)

// TestMonitor_LiveServer checks the disk readout, the live row counter under
// each relay compression, and a floor wait that gives up on cancel
// (ADR-0259 §SD6).
func TestMonitor_LiveServer(t *testing.T) {
	liveClient(t)
	client := chclient.New(chclient.ConfigFromEnv(), &http.Client{})
	ctx := context.Background()
	drop := func() {
		_ = client.Exec(context.Background(), "DROP DATABASE IF EXISTS "+itMonSource+" SYNC")
		_ = client.Exec(context.Background(), "DROP DATABASE IF EXISTS "+itMonTarget+" SYNC")
	}
	drop()
	t.Cleanup(drop)
	for _, sql := range []string{
		"CREATE DATABASE " + itMonSource,
		"CREATE DATABASE " + itMonTarget,
		"CREATE TABLE " + itMonSource + ".t (k UInt64, s String) ENGINE = MergeTree PARTITION BY k % 3 ORDER BY k",
		"INSERT INTO " + itMonSource + ".t SELECT number, repeat(toString(number), 5) FROM numbers(300000)",
		"CREATE TABLE " + itMonTarget + ".t AS " + itMonSource + ".t",
	} {
		require.NoError(t, client.Exec(ctx, sql), sql)
	}

	rep, err := ReadDisks(ctx, client, []datacatalog.TableRef{ref(itMonSource, "t"), ref(itMonTarget, "t")})
	require.NoError(t, err)
	require.NotEmpty(t, rep.Disks)
	src, has := rep.Table(ref(itMonSource, "t"))
	require.True(t, has)
	assert.Positive(t, src.BytesOnDisk)
	assert.NotEmpty(t, src.Disks)
	for _, d := range src.Disks {
		_, known := rep.Disk(d)
		assert.True(t, known, d)
	}

	ops := newOps(t)
	ep := SourceEndpoint()
	inv, err := Discover(ctx, client)
	require.NoError(t, err)
	plan, err := BuildPlan(ops, ep, ep, &inv, &inv, Selection{Databases: []string{itMonSource}, DatabaseMap: map[string]string{itMonSource: itMonTarget}}, time.Now())
	require.NoError(t, err)
	require.Len(t, plan.Tables, 1)
	pt := &plan.Tables[0]
	c, err := DeriveChunking(ctx, client, pt.Source, pt.SortingKey, pt.PartitionKey, pt.Rows, DefaultChunkingOptions())
	require.NoError(t, err)
	pt.Chunking = &c

	for i, compression := range []string{"zstd", "gzip", ""} {
		require.NoError(t, client.Exec(ctx, "TRUNCATE TABLE "+itMonTarget+".t"))
		pt.Sync = &TableSync{Mode: SyncModeFull}
		j, jerr := OpenJournal(filepath.Join(t.TempDir(), "j"), "run-"+compression)
		require.NoError(t, jerr)
		opts := DefaultSyncOptions()
		opts.Compression = compression
		var rows, bytes atomic.Int64
		opts.Rows, opts.Bytes = &rows, &bytes
		opts.PollPeriod = 100 * time.Millisecond
		calls := 0
		opts.BeforeChunk = func(context.Context, *PlanTable) error { calls++; return nil }
		sr, serr := SyncTable(ctx, client, client, pt, j, opts, time.Now)
		require.NoError(t, serr, compression)
		require.NoError(t, j.Close())
		assert.Equal(t, 3, sr.Copied, compression)
		assert.Equal(t, int64(300000), rows.Load(), "rows counted exactly (%q)", compression)
		assert.Equal(t, 3, calls)
		if i == 2 {
			assert.Greater(t, bytes.Load(), int64(3_000_000), "uncompressed Native")
		} else {
			assert.Less(t, bytes.Load(), int64(3_000_000), "compressed on the wire (%q)", compression)
		}
	}

	floor := FreeFloor{MinFreeBytes: ^uint64(0) >> 1, Poll: time.Second}
	cctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	notified := 0
	err = floor.WaitForFree(cctx, client, ref(itMonTarget, "t"), func(low []DiskInfo) { notified++ })
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Positive(t, notified)
}
