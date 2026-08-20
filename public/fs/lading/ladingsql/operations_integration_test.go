//go:build integration

package ladingsql_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingdata"
	"github.com/stergiotis/boxer/public/fs/lading/ladingingest"
	"github.com/stergiotis/boxer/public/fs/lading/ladingmeta"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsql"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file is ADR-0198 §7 — the operations the store exists for beyond
// `io/fs` — run as SQL against a real server over a seeded mount.
//
// They are executed rather than quoted because each one leans on something the
// expansion promises and nothing else checks: that the logical column names
// exist, that a text block's `line0` is the real line number, that a diff's
// missing side fills with '' rather than NULL, that `BLAKE3(data)` in the
// engine agrees with the digest the walker wrote in Go.

// seeded is one mount with two snapshots, and the sources they were taken of.
type seeded struct {
	exec   recordstore.ExecutorI
	stores lading.Stores
	first  ladingingest.Result
	second ladingingest.Result
	src    fstest.MapFS
}

func corpus() fstest.MapFS {
	var notes bytes.Buffer
	for i := 1; i <= 30; i++ {
		if i == 12 {
			notes.WriteString("TODO: the twelfth line\n")
			continue
		}
		fmt.Fprintf(&notes, "note line %d\n", i)
	}
	return fstest.MapFS{
		"docs/notes.txt": {Data: notes.Bytes(), Mode: 0o644, ModTime: time.Unix(1_700_000_001, 0)},
		"docs/readme.md": {Data: []byte("# readme\nTODO: write it\n"), Mode: 0o644, ModTime: time.Unix(1_700_000_002, 0)},
		"docs/copy.md":   {Data: []byte("# readme\nTODO: write it\n"), Mode: 0o644, ModTime: time.Unix(1_700_000_003, 0)},
		"bin/tool":       {Data: bytes.Repeat([]byte{0x7f, 'E', 'L', 'F'}, 50), Mode: 0o755, ModTime: time.Unix(1_700_000_004, 0)},
		"bin/gone.tmp":   {Data: []byte("temporary\n"), Mode: 0o644, ModTime: time.Unix(1_700_000_005, 0)},
		"empty":          {Mode: fs.ModeDir | 0o755, ModTime: time.Unix(1_700_000_006, 0)},
	}
}

func seedCorpus(t *testing.T) *seeded {
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
	purgeMount(t, exec)
	t.Cleanup(func() {
		st.Meta.Close()
		st.Data.Close()
		purgeMount(t, exec)
	})

	pol := ladingingest.DefaultPolicy()
	pol.Ttl = ladingingest.TtlClass7d
	pol.Profile.BlockSize = 96 // small, so notes.txt spans blocks
	pol.Profile.PerBlockHash = true

	src := corpus()
	first, err := ladingingest.Snapshot(ctx, src, testMount, pol, st)
	require.NoError(t, err)

	// A second voyage: one file gone, one added, one changed.
	later := corpus()
	delete(later, "bin/gone.tmp")
	later["docs/new.md"] = &fstest.MapFile{Data: []byte("# new\n"), Mode: 0o644, ModTime: time.Unix(1_700_000_007, 0)}
	later["docs/readme.md"] = &fstest.MapFile{Data: []byte("# readme\nDONE\n"), Mode: 0o644, ModTime: time.Unix(1_700_000_008, 0)}
	second, err := ladingingest.Snapshot(ctx, later, testMount, pol, st)
	require.NoError(t, err)

	return &seeded{exec: exec, stores: st, first: first, second: second, src: src}
}

func purgeMount(t *testing.T, exec recordstore.ExecutorI) {
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

// run expands sql through the macros and executes it, returning TSV rows.
func run(t *testing.T, sql string) (rows [][]string) {
	t.Helper()
	expanded, err := ladingsql.Expand(openCfg(), sql)
	require.NoErrorf(t, err, "expand:\n%s", sql)

	client := chclient.New(chclient.ConfigFromEnv(), nil)
	body, err := client.Query(context.Background(), expanded+" FORMAT TSV")
	require.NoErrorf(t, err, "execute:\n%s", expanded)
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

func m() uint64 { return testMount.Value() }

// TestOpGrepWithLineNumbers is §7's first query, and the one that cashes in
// M2's text guarantee: a text file's blocks end at newlines, so a single-line
// match cannot straddle two blocks and `line0 + i - 1` is the real line number.
func TestOpGrepWithLineNumbers(t *testing.T) {
	seedCorpus(t)
	rows := run(t, fmt.Sprintf(`
		SELECT path, line0 + i - 1 AS lineno, line
		FROM fsdata(%d)
		ARRAY JOIN splitByChar('\n', data) AS line,
		           arrayEnumerate(splitByChar('\n', data)) AS i
		WHERE match(line, 'TODO')
		ORDER BY path, lineno`, m()))

	// §7 spells this with a PREWHERE, and that does not survive the macro: a
	// subquery is not a table, and ClickHouse allows PREWHERE only against a
	// table or a table function. WHERE alone is correct — what is lost is the
	// pre-filtering, and a caller who needs it reads the physical table.
	require.Len(t, rows, 2, "the TODO in notes.txt and the one in copy.md")
	got := map[string]string{}
	for _, r := range rows {
		got[r[0]] = r[1]
	}
	assert.Equal(t, "12", got["docs/notes.txt"],
		"the line number must be the file's, not the block's — which is what line0 is for")
	assert.Equal(t, "2", got["docs/copy.md"])
}

// TestOpHistory — a mount over time, and one path's versions.
//
// §7 spells the first half against `fs(m, '*') WHERE path = '.'`, which does
// not work: the totals are the commit record's, a different component on the
// root row, and the entry projection cannot carry them. `fssnap(m)` is that
// relation, and it reads the index rather than every path of every snapshot.
func TestOpHistory(t *testing.T) {
	s := seedCorpus(t)

	rows := run(t, fmt.Sprintf(
		`SELECT snap_entries, snap_bytes, ttl_class FROM fssnap(%d) ORDER BY snap`, m()))
	require.Len(t, rows, 2, "two voyages")
	assert.Equal(t, "7d", rows[0][2], "the policy as applied, recorded per snapshot")
	assert.NotEqual(t, "0", rows[0][0])

	// One path's versions: the same file, changed between the two.
	versions := run(t, fmt.Sprintf(
		`SELECT size, hex(content_hash) FROM fs(%d, '*') WHERE path = 'docs/readme.md' ORDER BY snap`, m()))
	require.Len(t, versions, 2)
	assert.NotEqual(t, versions[0][1], versions[1][1], "the content changed, so the hash must have")
	_ = s
}

// TestOpDiffBetweenTwoSnapshots is §7's diff, and it rests on a property of
// the join rather than of the store: with join_use_nulls = 0 the missing side
// fills with ”, which is never a valid io/fs path and is therefore a safe
// "absent" marker.
func TestOpDiffBetweenTwoSnapshots(t *testing.T) {
	s := seedCorpus(t)
	rows := run(t, fmt.Sprintf(`
		SELECT if(n.path != '', n.path, o.path) AS path,
		       multiIf(o.path = '', 'added', n.path = '', 'removed',
		               n.content_hash != o.content_hash OR n.mtime != o.mtime, 'modified', 'same') AS change
		FROM fs(%d, %d) AS n
		FULL OUTER JOIN fs(%d, %d) AS o ON n.path = o.path
		WHERE change != 'same'
		ORDER BY path`,
		m(), s.second.Snap.UnixNano(), m(), s.first.Snap.UnixNano()))

	got := map[string]string{}
	for _, r := range rows {
		got[r[0]] = r[1]
	}
	assert.Equal(t, map[string]string{
		"bin/gone.tmp":   "removed",
		"docs/new.md":    "added",
		"docs/readme.md": "modified",
	}, got)
}

// TestOpDu — every directory's recursive size in one pass.
func TestOpDu(t *testing.T) {
	seedCorpus(t)
	rows := run(t, fmt.Sprintf(`
		SELECT anc, sum(size) AS bytes, count() AS files
		FROM fs(%d)
		ARRAY JOIN arrayMap(k -> arrayStringConcat(arraySlice(splitByChar('/', path), 1, k), '/'), range(1, depth)) AS anc
		WHERE NOT is_dir
		GROUP BY anc
		ORDER BY anc`, m()))

	got := map[string]string{}
	for _, r := range rows {
		got[r[0]] = r[2]
	}
	assert.Equal(t, "1", got["bin"], "one file under bin in the newest snapshot")
	assert.Equal(t, "4", got["docs"], "notes, readme, copy and new")
}

// TestOpWhatIsWhere — the four one-liners of §7.
func TestOpWhatIsWhere(t *testing.T) {
	seedCorpus(t)

	biggest := run(t, fmt.Sprintf(
		`SELECT path, size FROM fs(%d) WHERE NOT is_dir ORDER BY size DESC LIMIT 2`, m()))
	require.Len(t, biggest, 2)
	assert.Equal(t, "docs/notes.txt", biggest[0][0], "thirty lines of notes outweigh the 200-byte blob")

	byExt := run(t, fmt.Sprintf(
		`SELECT ext, count(), sum(size) FROM fs(%d) WHERE NOT is_dir GROUP BY ext ORDER BY 2 DESC, 1`, m()))
	exts := map[string]string{}
	for _, r := range byExt {
		exts[r[0]] = r[1]
	}
	assert.Equal(t, "3", exts[".md"], "readme, copy and new")
	assert.Equal(t, "1", exts[".txt"])
	assert.Equal(t, "1", exts[""], "an extensionless file groups under the empty string")

	errs := run(t, fmt.Sprintf(`SELECT path, err FROM fs(%d) WHERE err != ''`, m()))
	assert.Empty(t, errs, "nothing in this corpus fails to read")
}

// TestOpIdenticalContent — dedup as a question, not as storage. It is the one
// §7 query that shows why the store hashes content it does not deduplicate.
func TestOpIdenticalContent(t *testing.T) {
	s := seedCorpus(t)
	// Against the FIRST snapshot: the second rewrites readme.md, so the pair
	// is identical only there — which is itself the point of asking the
	// question per snapshot rather than per mount.
	rows := run(t, fmt.Sprintf(`
		SELECT hex(content_hash) AS h, arraySort(groupArray(path)) AS paths
		FROM fs(%d, %d) WHERE content != 'none'
		GROUP BY content_hash HAVING count() > 1
		ORDER BY h`, m(), s.first.Snap.UnixNano()))

	require.Len(t, rows, 1, "exactly one pair of byte-identical files")
	assert.Contains(t, rows[0][1], "docs/copy.md")
	assert.Contains(t, rows[0][1], "docs/readme.md")
}

// TestOpAuditEveryBlockAgainstItsDigest is the query the per-block hash exists
// for, and it works only because those digests are standalone BLAKE3 of the
// block rather than subtree chaining values (ADR-0198 §SD5 as corrected).
func TestOpAuditEveryBlockAgainstItsDigest(t *testing.T) {
	seedCorpus(t)
	rows := run(t, fmt.Sprintf(
		`SELECT countIf(BLAKE3(data) != hash) AS bad, count() AS total FROM fsdata(%d, '*')`, m()))
	require.Len(t, rows, 1)
	assert.Equal(t, "0", rows[0][0], "every stored digest must be BLAKE3 of its block")
	assert.NotEqual(t, "0", rows[0][1], "the audit must have seen blocks, or it proves nothing")
}

// TestOpAcrossMounts — the store-wide question, which reads the index rather
// than any mount's rows.
func TestOpAcrossMounts(t *testing.T) {
	s := seedCorpus(t)
	key, err := ladingschema.PhysicalPlainName("id")
	require.NoError(t, err)
	ts, err := ladingschema.PhysicalPlainName("ts")
	require.NoError(t, err)
	exp, err := ladingschema.PhysicalPlainName("expiresAt")
	require.NoError(t, err)

	client := chclient.New(chclient.ConfigFromEnv(), nil)
	body, err := client.Query(context.Background(), fmt.Sprintf(
		"SELECT %s AS id, max(%s) AS latest FROM %s.%s WHERE %s > now64(9,'UTC') AND %s = %d GROUP BY id FORMAT TSV",
		key, ts, ladingschema.DatabaseName, ladingschema.TableNameSnap, exp, key, m()))
	require.NoError(t, err)
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NotEmpty(t, strings.TrimSpace(string(raw)))
	_ = s
}

// TestTheCutoffHidesAnExpiredSnapshot is the acceptance the plan states: an
// expired row is invisible through fs() while still present in the table.
//
// It is the whole reason the cutoff is in the expansion. `TTL` reclaims space
// at merge time, so a row routinely outlives its expiry on disk, and a surface
// that read the table directly would hand back rows whose siblings a merge has
// already taken.
func TestTheCutoffHidesAnExpiredSnapshot(t *testing.T) {
	s := seedCorpus(t)
	ctx := context.Background()

	// A wholly expired partition is dropped by the engine as soon as it is
	// written — M0 measured that, and it is the behaviour `ttl_only_drop_parts`
	// exists for. So the row has to be protected from the merge to show what
	// the cutoff is for: rows that ARE still on disk and must not be returned.
	require.NoError(t, s.exec.Exec(ctx, fmt.Sprintf("SYSTEM STOP TTL MERGES %s.%s",
		ladingschema.DatabaseName, ladingschema.TableNameMeta)))
	t.Cleanup(func() {
		_ = s.exec.Exec(context.Background(), fmt.Sprintf("SYSTEM START TTL MERGES %s.%s",
			ladingschema.DatabaseName, ladingschema.TableNameMeta))
	})

	// A complete snapshot by every structural test, expired an hour ago.
	past := time.Now().UTC().Add(-72 * time.Hour)
	require.NoError(t, s.stores.Meta.Begin(m(), past, ladingmeta.MetaEnvelope{
		NaturalKey: []byte("."), ExpiresAt: time.Now().UTC().Add(-time.Hour),
	}).AddLadingEntry(ladingmeta.LadingEntry{
		Kind: "entry", NodeKind: "dir", Content: "none", Mode: uint32(fs.ModeDir | 0o755),
	}).AddLadingSnapshot(ladingmeta.LadingSnapshot{
		Kind: "snapshot", Entries: 1, TtlClass: "7d", TextRule: "sniff", InlineMax: 1 << 20,
	}).Commit())
	_, err := s.stores.Meta.Flush(ctx)
	require.NoError(t, err)

	// Still on disk: the raw table sees it.
	key, err := ladingschema.PhysicalPlainName("id")
	require.NoError(t, err)
	nk, err := ladingschema.PhysicalPlainName("naturalKey")
	require.NoError(t, err)
	client := chclient.New(chclient.ConfigFromEnv(), nil)
	body, err := client.Query(ctx, fmt.Sprintf(
		"SELECT count() FROM %s.%s WHERE %s = %d AND %s = '.' FORMAT TSV",
		ladingschema.DatabaseName, ladingschema.TableNameMeta, key, m(), nk))
	require.NoError(t, err)
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	_ = body.Close()
	assert.Equal(t, "3", strings.TrimSpace(string(raw)),
		"two live root rows and the expired one are all still in the table")

	// Invisible through the macro: neither as a snapshot nor as a row.
	rows := run(t, fmt.Sprintf(`SELECT count() FROM fs(%d, '*') WHERE path = '.'`, m()))
	require.Len(t, rows, 1)
	assert.Equal(t, "2", rows[0][0], "the expired snapshot must not be one of them")
}
