//go:build integration

package ladingadapter_test

import (
	"context"
	"fmt"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingadapter"
	"github.com/stergiotis/boxer/public/fs/lading/ladingdata"
	"github.com/stergiotis/boxer/public/fs/lading/ladingingest"
	"github.com/stergiotis/boxer/public/fs/lading/ladingmeta"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedLive is [seedFrom] against a real server. The difference that matters is
// `fssnap`: it is filled by a materialised view on insert, so [Snapshots] and
// [Latest] can only be exercised where an engine is running one.
func seedLive(t *testing.T, src fstest.MapFS) *harness {
	t.Helper()
	cfg := chclient.ConfigFromEnv()
	client := chclient.New(cfg, nil)
	if err := client.Ping(context.Background()); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", cfg.URL, err)
	}
	exec, err := storeexec.New(client, nil)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, lading.Provision(ctx, exec, ladingschema.ProfileCorpus))

	st := lading.Stores{
		Meta: ladingmeta.NewMetaStore(exec, nil, ladingmeta.MetaStoreConfig{}),
		Data: ladingdata.NewDataStore(exec, nil, ladingdata.DataStoreConfig{}),
	}
	purge(t, exec)
	t.Cleanup(func() {
		st.Meta.Close()
		st.Data.Close()
		purge(t, exec)
	})

	res, err := ladingingest.Snapshot(ctx, src, testMount, policy(), st)
	require.NoError(t, err)
	return &harness{exec: exec, stores: st, res: res, src: src}
}

func purge(t *testing.T, exec recordstore.ExecutorI) {
	t.Helper()
	ctx := context.Background()
	key, err := ladingschema.PhysicalPlainName("id")
	require.NoError(t, err)
	for _, tbl := range []string{
		ladingschema.TableNameMeta, ladingschema.TableNameData, ladingschema.TableNameSnap,
	} {
		require.NoError(t, exec.Exec(ctx, fmt.Sprintf("DELETE FROM %s.%s WHERE %s = %d",
			ladingschema.DatabaseName, tbl, key, testMount.Value())))
	}
}

// TestFstestConformanceLive is the M3 acceptance as the plan states it: the
// standard library's conformance test against a snapshot on a real server.
func TestFstestConformanceLive(t *testing.T) {
	h := seedLive(t, readableSource())
	fsys := h.open(t)
	require.NoError(t, fstest.TestFS(fsys,
		"a", "a/b.txt", "a/c", "a/c/d.bin", "a/c/tiny", "top.md", "empty"))
}

// TestSnapshotsAndLatestReadTheIndex. Both come off `fssnap`, which the
// materialised view fills from root rows — so what they list is exactly the
// set of complete snapshots, and an interrupted walk cannot appear.
func TestSnapshotsAndLatestReadTheIndex(t *testing.T) {
	h := seedLive(t, source())
	ctx := context.Background()

	all, err := ladingadapter.Snapshots(ctx, h.exec, testMount)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.True(t, all[0].Snap.Equal(h.res.Snap), "the index names the snapshot the walk returned")
	assert.Equal(t, h.res.Entries, all[0].Entries, "the totals are the walk's own")
	assert.Equal(t, "7d", all[0].TtlClass, "the policy as applied")
	assert.True(t, all[0].ExpiresAt.Equal(h.res.ExpiresAt))

	// A second voyage, and the newest is the newest.
	second := source()
	second["late.txt"] = &fstest.MapFile{Data: []byte("later\n"), Mode: 0o644}
	r2, err := ladingingest.Snapshot(ctx, second, testMount, policy(), h.stores)
	require.NoError(t, err)

	all, err = ladingadapter.Snapshots(ctx, h.exec, testMount)
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.True(t, all[0].Snap.After(all[1].Snap), "newest first")

	latest, found, err := ladingadapter.Latest(ctx, h.exec, testMount)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, latest.Snap.Equal(r2.Snap))

	fsys, found, err := ladingadapter.OpenLatest(ctx, h.exec, h.stores, testMount)
	require.NoError(t, err)
	require.True(t, found)
	_, err = fsys.Stat("late.txt")
	assert.NoError(t, err, "OpenLatest pins the newer one")

	// A mount nothing has written has no snapshots, and that is not an error.
	_, found, err = ladingadapter.Latest(ctx, h.exec, testMount+1)
	require.NoError(t, err)
	assert.False(t, found)
}

// TestAPinnedViewDoesNotShift is the property the whole read side rests on: a
// snapshot written while another is being read changes nothing about the one
// in hand.
//
// It is not a locking claim. Every query the adapter issues carries `ts =
// <snap>`, and a walk writes a different `ts`, so there is nothing for a
// concurrent writer to shift — which is why the adapter's caches need no
// invalidation.
func TestAPinnedViewDoesNotShift(t *testing.T) {
	h := seedLive(t, source())
	ctx := context.Background()
	fsys := h.open(t)

	before, err := fs.ReadDir(fsys, ".")
	require.NoError(t, err)
	beforeContent, err := fs.ReadFile(fsys, "top.md")
	require.NoError(t, err)

	// A whole second snapshot lands underneath the open view: a file added, a
	// file removed, and one file's content changed.
	changed := source()
	delete(changed, "a/c/tiny")
	changed["brand.new"] = &fstest.MapFile{Data: []byte("new\n"), Mode: 0o644}
	changed["top.md"] = &fstest.MapFile{Data: []byte("# completely different\n"), Mode: 0o644}
	r2, err := ladingingest.Snapshot(ctx, changed, testMount, policy(), h.stores)
	require.NoError(t, err)
	require.NotEqual(t, h.res.Snap, r2.Snap)

	after, err := fs.ReadDir(fsys, ".")
	require.NoError(t, err)
	assert.Equal(t, names(before), names(after), "the pinned listing is unchanged")

	afterContent, err := fs.ReadFile(fsys, "top.md")
	require.NoError(t, err)
	assert.Equal(t, beforeContent, afterContent, "and so is the pinned content")

	_, err = fsys.Stat("brand.new")
	assert.ErrorIs(t, err, fs.ErrNotExist, "a file added after the pin is not in it")
	_, err = fsys.Stat("a/c/tiny")
	assert.NoError(t, err, "and a file removed after the pin is still in it")

	// The new snapshot, opened on its own, has all of it.
	newer, err := ladingadapter.Open(h.stores, testMount, r2.Snap)
	require.NoError(t, err)
	_, err = newer.Stat("brand.new")
	assert.NoError(t, err)
	_, err = newer.Stat("a/c/tiny")
	assert.ErrorIs(t, err, fs.ErrNotExist)
	newest, err := fs.ReadFile(newer, "top.md")
	require.NoError(t, err)
	assert.Equal(t, []byte("# completely different\n"), newest)
}

// TestExpiredSnapshotsAreNotListed. TTL reclaims space at merge time, so a row
// can outlive its expiry on disk; a reader that ignored that would hand back a
// snapshot whose entries may already be gone.
func TestExpiredSnapshotsAreNotListed(t *testing.T) {
	h := seedLive(t, source())
	ctx := context.Background()

	all, err := ladingadapter.Snapshots(ctx, h.exec, testMount)
	require.NoError(t, err)
	require.Len(t, all, 1)

	// Write a root row directly, expired an hour ago. It is a complete
	// snapshot by every structural test; only the cutoff hides it.
	past := time.Now().UTC().Add(-48 * time.Hour)
	require.NoError(t, h.stores.Meta.Begin(testMount.Value(), past, ladingmeta.MetaEnvelope{
		NaturalKey: []byte("."), ExpiresAt: time.Now().UTC().Add(-time.Hour),
	}).AddLadingEntry(ladingmeta.LadingEntry{
		Kind: "entry", NodeKind: "dir", Content: "none", Mode: uint32(fs.ModeDir | 0o755),
	}).AddLadingSnapshot(ladingmeta.LadingSnapshot{
		Kind: "snapshot", Entries: 1, TtlClass: "7d", TextRule: "sniff", InlineMax: 1024,
	}).Commit())
	_, err = h.stores.Meta.Flush(ctx)
	require.NoError(t, err)

	all, err = ladingadapter.Snapshots(ctx, h.exec, testMount)
	require.NoError(t, err)
	assert.Len(t, all, 1, "an expired snapshot is still on disk and must still be invisible")
}

func names(in []fs.DirEntry) (out []string) {
	for _, d := range in {
		out = append(out, d.Name())
	}
	return
}
