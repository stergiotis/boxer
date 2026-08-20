//go:build integration

package ladingingest_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingdata"
	"github.com/stergiotis/boxer/public/fs/lading/ladingingest"
	"github.com/stergiotis/boxer/public/fs/lading/ladingmeta"
	"github.com/stergiotis/boxer/public/fs/lading/ladingpolicy"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/fs/lading/ladingvocab"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func liveHarness(t *testing.T) *harness {
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

	h := &harness{
		exec: exec,
		meta: ladingmeta.NewMetaStore(exec, nil, ladingmeta.MetaStoreConfig{}),
		data: ladingdata.NewDataStore(exec, nil, ladingdata.DataStoreConfig{}),
	}
	purgeLive(t, exec)
	t.Cleanup(func() {
		h.meta.Close()
		h.data.Close()
		purgeLive(t, exec)
	})
	return h
}

// purgeLive removes this mount's rows from the shared server — the per-mount
// purge ADR-0198 §SD1 promises, used here as the test's own cleanup.
func purgeLive(t *testing.T, exec recordstore.ExecutorI) {
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
	require.NoError(t, exec.Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE %s = %d",
		ladingpolicy.PolicyTableName, key, testMount.Value())))
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

func snapCount(t *testing.T) string {
	t.Helper()
	key, err := ladingschema.PhysicalPlainName("id")
	require.NoError(t, err)
	rows := queryTSV(t, fmt.Sprintf("SELECT count() FROM %s.%s WHERE %s = %d",
		ladingschema.DatabaseName, ladingschema.TableNameSnap, key, testMount.Value()))
	require.Len(t, rows, 1)
	return rows[0][0]
}

// TestSnapshotAgainstALiveServer is M2's integration acceptance: the same
// fixture tree through a real ClickHouse, with the snapshot index appearing
// only once the root row lands.
//
// The default lane already checks the rows through clickhouse-local. What only
// a server can show is the materialised view — `fssnap` is filled by an MV on
// insert, so the commit rule is enforced by the engine here rather than by the
// walker's ordering alone.
func TestSnapshotAgainstALiveServer(t *testing.T) {
	h := liveHarness(t)
	ctx := context.Background()
	fsys := tree()

	assert.Equal(t, "0", snapCount(t), "nothing written yet")

	res, err := ladingingest.Snapshot(ctx, fsys, testMount, testPolicy(), h.stores())
	require.NoError(t, err)

	assert.Equal(t, "1", snapCount(t), "the root row landed, so the snapshot exists — exactly once")

	got := h.entries(t, res)
	assert.Len(t, got, 10)
	assert.Equal(t, "blocks", got["a/b.txt"].Content)
	assert.Equal(t, "ref", got["big.bin"].Content)
	assert.Equal(t, "a/b.txt", got["link.txt"].LinkTarget)

	// The block audit the per-block digest exists for, run as SQL against the
	// real engine — the query ADR-0198 §7 states, and it works only because
	// the digests are standalone BLAKE3 of the block rather than subtree
	// chaining values.
	bad, total := auditBlocks(t)
	assert.Equal(t, "0", bad, "every block's stored digest must be BLAKE3 of its bytes")
	assert.NotEqual(t, "0", total, "the audit must have seen blocks, or it proves nothing")
}

// auditBlocks runs `BLAKE3(data) != hash` over this mount's blocks.
//
// The two attributes sit in one blobArray section, so the value lane holds
// both per row — data at one position and hash at the other, told apart by
// their membership ids. Positions rather than names because the read surface's
// sugar is a nanopass expansion and this test talks to the server directly.
func auditBlocks(t *testing.T) (bad string, total string) {
	t.Helper()
	key, err := ladingschema.PhysicalPlainName("id")
	require.NoError(t, err)
	const (
		val = `"tv:blobArray:value:val:yh:4:::0::data"`
		lr  = `"tv:blobArray:lr:lr:u64:1247:::0::data"`
	)
	rows := queryTSV(t, fmt.Sprintf(`
		SELECT countIf(BLAKE3(%[1]s[indexOf(%[2]s, %[3]d)]) != %[1]s[indexOf(%[2]s, %[4]d)]) AS bad,
		       count() AS total
		FROM %[5]s.%[6]s
		WHERE %[7]s = %[8]d AND has(%[2]s, %[4]d)`,
		val, lr,
		ladingvocab.MembData.GetId().Value(), ladingvocab.MembBlockHash.GetId().Value(),
		ladingschema.DatabaseName, ladingschema.TableNameData,
		key, testMount.Value()))
	require.Len(t, rows, 1)
	return rows[0][0], rows[0][1]
}

// TestRecordPolicyIsSeparateFromASnapshot. The declared policy is runtime
// state on `boxer.facts`; the applied policy rides the snapshot's root row.
// Taking a snapshot must not append to the registry.
func TestRecordPolicyIsSeparateFromASnapshot(t *testing.T) {
	h := liveHarness(t)
	ctx := context.Background()
	ps := ladingpolicy.NewPolicyStore(h.exec, nil, ladingpolicy.PolicyStoreConfig{})
	defer ps.Close()

	_, err := ladingingest.Snapshot(ctx, tree(), testMount, testPolicy(), h.stores())
	require.NoError(t, err)

	key, err := ladingschema.PhysicalPlainName("id")
	require.NoError(t, err)
	countPolicy := func() string {
		rows := queryTSV(t, fmt.Sprintf("SELECT count() FROM %s WHERE %s = %d",
			ladingpolicy.PolicyTableName, key, testMount.Value()))
		require.Len(t, rows, 1)
		return rows[0][0]
	}
	assert.Equal(t, "0", countPolicy(), "a snapshot must not touch the mount registry")

	require.NoError(t, ladingingest.RecordPolicy(ctx, ps, testMount, testPolicy(), "the-fixture", "corpus"))
	assert.Equal(t, "1", countPolicy())

	found := 0
	for ent, serr := range ps.ScanLadingMount(ctx, recordstore.ScanOpts{
		ExtraPredicate: fmt.Sprintf("%s = %d", key, testMount.Value()),
	}) {
		require.NoError(t, serr)
		require.True(t, ent.LadingMount.Has)
		found++
		assert.Equal(t, "the-fixture", ent.LadingMount.Val.Name)
		assert.Equal(t, "corpus", ent.LadingMount.Val.Store)
		assert.Equal(t, "7d", ent.LadingMount.Val.TtlClass)
		assert.EqualValues(t, 1024, ent.LadingMount.Val.InlineMax)
	}
	assert.Equal(t, 1, found)
}

// TestTwoSnapshotsOfOneMountAreTwoVoyages. Time travel and diff come from
// nothing more than a second walk: same key prefix, different `ts`.
func TestTwoSnapshotsOfOneMountAreTwoVoyages(t *testing.T) {
	h := liveHarness(t)
	ctx := context.Background()

	first := tree()
	r1, err := ladingingest.Snapshot(ctx, first, testMount, testPolicy(), h.stores())
	require.NoError(t, err)

	second := tree()
	delete(second, "top.md")
	second["added.txt"] = first["a/b.txt"]
	r2, err := ladingingest.Snapshot(ctx, second, testMount, testPolicy(), h.stores())
	require.NoError(t, err)
	require.NotEqual(t, r1.Snap, r2.Snap, "two walks are two snapshots")

	assert.Equal(t, "2", snapCount(t))

	key, err := ladingschema.PhysicalPlainName("id")
	require.NoError(t, err)
	nk, err := ladingschema.PhysicalPlainName("naturalKey")
	require.NoError(t, err)
	ts, err := ladingschema.PhysicalPlainName("ts")
	require.NoError(t, err)

	// The diff idiom of ADR-0198 §7, over the two snapshots this test just
	// wrote. '' is never a valid io/fs path, so it is a safe absent marker —
	// which holds only while join_use_nulls is 0.
	//
	// fromUnixTimestamp64Nano, not toDateTime64: a plain number handed to
	// toDateTime64 is read as *seconds* whatever the scale says, so nanosecond
	// input saturates to the year 2262 and every predicate over it silently
	// matches nothing.
	rows := queryTSV(t, fmt.Sprintf(`
		SELECT if(n.path != '', n.path, o.path) AS path,
		       multiIf(o.path = '', 'added', n.path = '', 'removed', 'same') AS change
		FROM (SELECT %[1]s AS path FROM %[2]s.%[3]s WHERE %[4]s = %[5]d AND %[6]s = fromUnixTimestamp64Nano(toInt64(%[7]d), 'UTC')) AS n
		FULL OUTER JOIN
		     (SELECT %[1]s AS path FROM %[2]s.%[3]s WHERE %[4]s = %[5]d AND %[6]s = fromUnixTimestamp64Nano(toInt64(%[8]d), 'UTC')) AS o
		ON n.path = o.path
		WHERE change != 'same'
		ORDER BY path`,
		nk, ladingschema.DatabaseName, ladingschema.TableNameMeta, key, testMount.Value(), ts,
		r2.Snap.UnixNano(), r1.Snap.UnixNano()))

	got := map[string]string{}
	for _, r := range rows {
		got[r[0]] = r[1]
	}
	assert.Equal(t, map[string]string{"added.txt": "added", "top.md": "removed"}, got)
}
