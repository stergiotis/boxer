//go:build integration

package lading_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingdata"
	"github.com/stergiotis/boxer/public/fs/lading/ladingmeta"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/fs/lading/ladingvocab"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The mount this file writes under. It is an opaque uint64 on purpose: the
// store never claims a tag, mints an id, inspects a body or assumes a width
// (ADR-0198 §SD3), and a test that had to mint one would be testing the
// application's half of that contract rather than the store's.
//
// A distinctive value so the rows are recognisable in a shared server, and
// every test here removes its own at the end.
const testMount uint64 = 0xF5F5_0198_0000_0001

func liveExec(t *testing.T) recordstore.ExecutorI {
	t.Helper()
	cfg := chclient.ConfigFromEnv()
	client := chclient.New(cfg, nil)
	if err := client.Ping(context.Background()); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", cfg.URL, err)
	}
	exec, err := storeexec.New(client, nil)
	require.NoError(t, err)
	return exec
}

func queryTSV(t *testing.T, sql string) (rows [][]string) {
	t.Helper()
	client := chclient.New(chclient.ConfigFromEnv(), nil)
	body, err := client.Query(context.Background(), sql+" FORMAT TSV")
	require.NoError(t, err)
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if line != "" {
			rows = append(rows, strings.Split(line, "\t"))
		}
	}
	return
}

// purge removes this mount's rows. It is also the only exercise of the
// per-mount purge ADR-0198 §SD1 promises — a lightweight DELETE on a key
// prefix, no sweep, no reference counting.
func purge(t *testing.T, exec recordstore.ExecutorI) {
	t.Helper()
	ctx := context.Background()
	key, err := ladingschema.PhysicalPlainName("id")
	require.NoError(t, err)
	for _, tbl := range []string{
		ladingschema.TableNameMeta, ladingschema.TableNameData, ladingschema.TableNameSnap,
	} {
		require.NoError(t, exec.Exec(ctx, fmt.Sprintf("DELETE FROM %s.%s WHERE %s = %d",
			ladingschema.DatabaseName, tbl, key, testMount)))
	}
}

// TestProvisionIsIdempotentAndVerifies is M1's acceptance: the three tables,
// the ALTERs and the view come up from nothing, come up again unchanged, and
// the generated stores agree with what landed.
//
// The second Provision is the load-bearing half. Every statement is IF NOT
// EXISTS, so a store may run this at every start — and it must, because a
// store that starts against a half-provisioned table reads correctly and
// indexes nothing.
func TestProvisionIsIdempotentAndVerifies(t *testing.T) {
	exec := liveExec(t)
	ctx := context.Background()

	require.NoError(t, lading.Provision(ctx, exec, ladingschema.ProfileCorpus))
	require.NoError(t, lading.Verify(ctx, exec))
	require.NoError(t, lading.Provision(ctx, exec, ladingschema.ProfileCorpus),
		"provisioning must be repeatable — it runs at every start")
	require.NoError(t, lading.Verify(ctx, exec),
		"VerifySchema must pass with the MATERIALIZED tree columns present: they are absent from SELECT *, which is what the decode reads")

	// The clauses are the design's, not the generator's defaults.
	rows := queryTSV(t, fmt.Sprintf(
		"SELECT partition_key, sorting_key, engine FROM system.tables WHERE database = '%s' AND name = '%s'",
		ladingschema.DatabaseName, ladingschema.TableNameMeta))
	require.Len(t, rows, 1)
	assert.Contains(t, rows[0][0], "toYYYYMMDD", "partitioned by expiry day, never by mount")
	assert.Contains(t, rows[0][1], "naturalKey", "(mount, snapshot, path) is the key")

	// The four tree columns are there and are hidden from the decode.
	rows = queryTSV(t, fmt.Sprintf(
		"SELECT name FROM system.columns WHERE database = '%s' AND table = '%s' AND default_kind = 'MATERIALIZED' ORDER BY name",
		ladingschema.DatabaseName, ladingschema.TableNameMeta))
	got := make([]string, 0, len(rows))
	for _, r := range rows {
		got = append(got, r[0])
	}
	assert.Equal(t, []string{"depth", "dir", "ext", "name"}, got)

	// And the skip index behind ReadDir.
	rows = queryTSV(t, fmt.Sprintf(
		"SELECT name, type_full FROM system.data_skipping_indices WHERE database = '%s' AND table = '%s'",
		ladingschema.DatabaseName, ladingschema.TableNameMeta))
	require.Len(t, rows, 1)
	assert.Equal(t, "ix_dir", rows[0][0])
	assert.Contains(t, rows[0][1], "bloom_filter")
}

// TestManyRowsPerSnapshotRoundTrip writes a small tree the way the walker will
// and reads it back the way the adapter will.
//
// Two things it pins that no unit test can. First, many rows share one
// (id, ts) — one per path — which is not the shape a record store usually
// carries and is why the generated Ingest<Kind> verb cannot be used: it
// refuses a repeated key and would drop the envelope. Second, the root row
// carries two components at once, which is what makes it the commit record.
func TestManyRowsPerSnapshotRoundTrip(t *testing.T) {
	exec := liveExec(t)
	ctx := context.Background()
	require.NoError(t, lading.Provision(ctx, exec, ladingschema.ProfileCorpus))
	purge(t, exec)
	t.Cleanup(func() { purge(t, exec) })

	snap := time.Now().UTC().Truncate(time.Second)
	// Whole days only: an expiry inside a day leaves a partition partially
	// expired, and a partially expired part keeps its rows through every
	// background merge under ttl_only_drop_parts = 1.
	expiresAt := snap.UTC().Truncate(24 * time.Hour).Add(48 * time.Hour)

	meta := ladingmeta.NewMetaStore(exec, nil, ladingmeta.MetaStoreConfig{})
	defer meta.Close()

	tree := []struct {
		path string
		kind string
		size uint64
	}{
		{"a", "dir", 0},
		{"a/b.txt", "file", 12},
		{"a/c", "dir", 0},
		{"a/c/d.bin", "file", 4096},
		{"top.md", "file", 3},
	}
	for _, n := range tree {
		row := ladingmeta.LadingEntry{
			Kind: "entry", NodeKind: n.kind, Content: "blocks",
			Mode: 0o644, BlockSize: ladingschema.ProfileCorpus.BlockSize, Blocks: 1,
			Size: n.size, Mtime: snap, ContentHash: []byte("0123456789abcdef0123456789abcdef"),
			Text: n.kind == "file",
		}
		require.NoError(t, meta.Begin(testMount, snap, ladingmeta.MetaEnvelope{
			NaturalKey: []byte(n.path), ExpiresAt: expiresAt,
		}).AddLadingEntry(row).Commit())
	}
	n, err := meta.Flush(ctx)
	require.NoError(t, err)
	require.Equal(t, len(tree), n)

	// Before the root row lands, the snapshot does not exist.
	assert.Empty(t, queryTSV(t, fmt.Sprintf(
		"SELECT 1 FROM %s.%s WHERE %s = %d",
		ladingschema.DatabaseName, ladingschema.TableNameSnap,
		mustPlain(t, "id"), testMount)),
		"a walk with no root row must be invisible — that is the commit rule")

	// The root row: an entry like any other, plus the commit record.
	require.NoError(t, meta.Begin(testMount, snap, ladingmeta.MetaEnvelope{
		NaturalKey: []byte("."), ExpiresAt: expiresAt,
	}).AddLadingEntry(ladingmeta.LadingEntry{
		Kind: "entry", NodeKind: "dir", Content: "none", Mode: 0o755, Mtime: snap,
	}).AddLadingSnapshot(ladingmeta.LadingSnapshot{
		Kind: "snapshot", Entries: uint64(len(tree)) + 1, Bytes: 4111,
		TtlClass: "1d", TextRule: "newline", InlineMax: 1 << 20,
	}).Commit())
	_, err = meta.Flush(ctx)
	require.NoError(t, err)

	// Now it does, through the view, exactly once.
	rows := queryTSV(t, fmt.Sprintf(
		"SELECT count() FROM %s.%s WHERE %s = %d",
		ladingschema.DatabaseName, ladingschema.TableNameSnap, mustPlain(t, "id"), testMount))
	require.Len(t, rows, 1)
	assert.Equal(t, "1", rows[0][0], "the view copies one row per complete snapshot")

	// And it arrives with its commit record intact. The view's predicate is a
	// path equality — the cheapest thing it can be, since it runs on every
	// insert block — so what makes an fssnap row a snapshot is the component
	// the walker put on it, and that is what is checked rather than assumed.
	rows = queryTSV(t, fmt.Sprintf(
		"SELECT countIf(has(`tv:symbol:lr:lr:u64:1247:::0::data`, %d)) FROM %s.%s WHERE %s = %d",
		ladingvocab.MembKindSnapshot.GetId().Value(),
		ladingschema.DatabaseName, ladingschema.TableNameSnap, mustPlain(t, "id"), testMount))
	require.Len(t, rows, 1)
	assert.Equal(t, "1", rows[0][0],
		"every fssnap row must carry the snapshot component — the view copies whole rows, it does not synthesise them")

	// Read back through the generated Scan with a key predicate, the way the
	// adapter's ReadDir will.
	byPath := map[string]ladingmeta.LadingEntry{}
	for ent, serr := range meta.ScanLadingEntry(ctx, recordstore.ScanOpts{
		ExtraPredicate: fmt.Sprintf("%s = %d AND startsWith(%s, 'a/')",
			mustPlain(t, "id"), testMount, mustPlain(t, "naturalKey")),
	}) {
		require.NoError(t, serr)
		require.True(t, ent.LadingEntry.Has)
		byPath[string(ent.NaturalKey)] = ent.LadingEntry.Val
	}
	require.Len(t, byPath, 3, "a subtree is a startsWith range over the natural key")
	assert.Equal(t, uint64(4096), byPath["a/c/d.bin"].Size)
	assert.Equal(t, "dir", byPath["a/c"].NodeKind)
	assert.Equal(t, snap, byPath["a/b.txt"].Mtime.UTC())
	assert.Len(t, byPath["a/b.txt"].ContentHash, 32)

	// The commit record decodes as its own kind, on the row that also decodes
	// as an entry.
	commits := 0
	for ent, serr := range meta.ScanLadingSnapshot(ctx, recordstore.ScanOpts{
		ExtraPredicate: fmt.Sprintf("%s = %d", mustPlain(t, "id"), testMount),
	}) {
		require.NoError(t, serr)
		commits++
		require.True(t, ent.LadingSnapshot.Has)
		assert.Equal(t, ".", string(ent.NaturalKey))
		assert.Equal(t, uint64(len(tree))+1, ent.LadingSnapshot.Val.Entries)
		assert.Equal(t, "1d", ent.LadingSnapshot.Val.TtlClass)
		assert.True(t, ent.LadingEntry.Has, "the root row is an entry too — Stat('.') has to work")
	}
	assert.Equal(t, 1, commits)
}

// TestBlockOrdinalIsAContiguousRange pins the encoding SD11 chose: the ordinal
// rides the natural key as `path ‖ 0x00 ‖ be32(seq)`, so a file's blocks are
// one key range and come back in ordinal order under a prefix predicate.
func TestBlockOrdinalIsAContiguousRange(t *testing.T) {
	exec := liveExec(t)
	ctx := context.Background()
	require.NoError(t, lading.Provision(ctx, exec, ladingschema.ProfileCorpus))
	purge(t, exec)
	t.Cleanup(func() { purge(t, exec) })

	snap := time.Now().UTC().Truncate(time.Second)
	expiresAt := snap.Truncate(24 * time.Hour).Add(48 * time.Hour)
	data := ladingdata.NewDataStore(exec, nil, ladingdata.DataStoreConfig{})
	defer data.Close()

	const blocks = 5
	for seq := range blocks {
		key := append([]byte("a/c/d.bin"), 0)
		key = append(key, byte(seq>>24), byte(seq>>16), byte(seq>>8), byte(seq))
		require.NoError(t, data.Begin(testMount, snap, ladingdata.DataEnvelope{
			NaturalKey: key, ExpiresAt: expiresAt,
		}).AddLadingBlock(ladingdata.LadingBlock{
			Kind: "block",
			Data: []byte(fmt.Sprintf("block-%d", seq)),
			Hash: []byte("0123456789abcdef0123456789abcdef"),
			// 1-based, and one line per block in this fixture.
			Line0: uint32(seq + 1),
		}).Commit())
	}
	_, err := data.Flush(ctx)
	require.NoError(t, err)

	// A different file's blocks must not fall inside the range: 0x00 sorts
	// below every byte an io/fs path may carry, so "a/c/d.bin\0…" cannot
	// collide with "a/c/d.bin.bak\0…".
	require.NoError(t, data.Begin(testMount, snap, ladingdata.DataEnvelope{
		NaturalKey: append([]byte("a/c/d.bin.bak"), 0, 0, 0, 0, 0), ExpiresAt: expiresAt,
	}).AddLadingBlock(ladingdata.LadingBlock{Kind: "block", Data: []byte("other")}).Commit())
	_, err = data.Flush(ctx)
	require.NoError(t, err)

	seen := make([]string, 0, blocks)
	for ent, serr := range data.ScanLadingBlock(ctx, recordstore.ScanOpts{
		ExtraPredicate: fmt.Sprintf("%s = %d AND startsWith(%s, 'a/c/d.bin\\0')",
			mustPlain(t, "id"), testMount, mustPlain(t, "naturalKey")),
	}) {
		require.NoError(t, serr)
		require.True(t, ent.LadingBlock.Has)
		seen = append(seen, string(ent.LadingBlock.Val.Data))
	}
	assert.ElementsMatch(t,
		[]string{"block-0", "block-1", "block-2", "block-3", "block-4"}, seen,
		"the prefix selects exactly this file's blocks and nothing else")
}

func mustPlain(t *testing.T, plain string) string {
	t.Helper()
	q, err := ladingschema.PhysicalPlainName(plain)
	require.NoError(t, err)
	return q
}
