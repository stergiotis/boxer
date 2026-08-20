package ladingingest_test

import (
	"bytes"
	"context"
	"fmt"
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
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"lukechampine.com/blake3"
)

// The mount every test here writes under. Opaque on purpose: the store never
// claims a tag, mints an id or inspects a body (ADR-0198 §SD3).
const testMount identifier.TaggedId = 0xF5F5_0198_0002_0001

// blockSize is small so a fixture file can span blocks without being large.
const blockSize = 64

// textLines is 40 short lines. At a 64-byte block that is several blocks with
// several lines each, which is what makes the line0 sequence worth checking.
func textLines() []byte {
	var b bytes.Buffer
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.Bytes()
}

// binary carries a NUL, so no text rule may call it text however it is cut.
func binaryBlob() []byte {
	b := make([]byte, 200)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

// longLine is one line longer than a block. It is the case the newline cut
// cannot serve, and the file must therefore come back Text = false.
func longLine() []byte {
	return append(bytes.Repeat([]byte("x"), blockSize*2), '\n')
}

// tree is the fixture the M2 acceptance names: files, directories, symlinks, a
// text file spanning blocks, a binary file, an empty directory — plus a file
// over the inline threshold and a line too long to cut on.
func tree() fstest.MapFS {
	return fstest.MapFS{
		"a/b.txt":      {Data: textLines(), Mode: 0o644, ModTime: time.Unix(1_700_000_001, 0)},
		"a/c/d.bin":    {Data: binaryBlob(), Mode: 0o600, ModTime: time.Unix(1_700_000_002, 0)},
		"a/c/long.txt": {Data: longLine(), Mode: 0o644, ModTime: time.Unix(1_700_000_003, 0)},
		"top.md":       {Data: []byte("# hi\n"), Mode: 0o644, ModTime: time.Unix(1_700_000_004, 0)},
		"big.bin":      {Data: bytes.Repeat([]byte("B"), 4096), Mode: 0o644, ModTime: time.Unix(1_700_000_005, 0)},
		"empty":        {Mode: fs.ModeDir | 0o755, ModTime: time.Unix(1_700_000_006, 0)},
		"link.txt":     {Data: []byte("a/b.txt"), Mode: fs.ModeSymlink | 0o777, ModTime: time.Unix(1_700_000_007, 0)},
	}
}

func testPolicy() ladingingest.Policy {
	p := ladingingest.DefaultPolicy()
	p.Ttl = ladingingest.TtlClass7d
	p.InlineMax = 1024 // big.bin (4096) lands above it
	p.Profile.BlockSize = blockSize
	p.Profile.PerBlockHash = true
	return p
}

// harness provisions the store into a throwaway clickhouse-local directory and
// hands back the pair a walk writes through.
//
// clickhouse-local rather than a recording executor: the acceptance is about
// the rows, and a recorder would only prove what the walker handed to Arrow,
// not what survives the encode, the insert and the decode. This is the same
// lane `recordstore/example` uses, and it skips itself when the binary is not
// there.
type harness struct {
	exec recordstore.ExecutorI
	meta *ladingmeta.MetaStore
	data *ladingdata.DataStore
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	ctx := context.Background()
	require.NoError(t, lading.Provision(ctx, exec, ladingschema.ProfileCorpus))
	h := &harness{
		exec: exec,
		meta: ladingmeta.NewMetaStore(exec, nil, ladingmeta.MetaStoreConfig{}),
		data: ladingdata.NewDataStore(exec, nil, ladingdata.DataStoreConfig{}),
	}
	t.Cleanup(func() { h.meta.Close(); h.data.Close() })
	return h
}

func (inst *harness) stores() lading.Stores {
	return lading.Stores{Meta: inst.meta, Data: inst.data}
}

// entries reads every entry row of ONE snapshot back, keyed by path.
//
// Pinned to res.Snap, not just to the mount: several tests write more than one
// snapshot of the same mount, and a helper that read them all would resolve a
// path to whichever snapshot's row the scan returned — a pass or a fail that
// moves with Go's map ordering rather than with the code.
func (inst *harness) entries(t *testing.T, res ladingingest.Result) map[string]ladingmeta.LadingEntry {
	t.Helper()
	out := map[string]ladingmeta.LadingEntry{}
	for ent, err := range inst.meta.ScanLadingEntry(context.Background(), recordstore.ScanOpts{
		ExtraPredicate: fmt.Sprintf("%s = %d AND %s = %s",
			plain(t, "id"), testMount.Value(), plain(t, "ts"), tsLiteral(res.Snap)),
	}) {
		require.NoError(t, err)
		require.True(t, ent.LadingEntry.Has)
		out[string(ent.NaturalKey)] = ent.LadingEntry.Val
	}
	return out
}

// blocksOf reads one file's blocks back, in ordinal order, from one snapshot.
// Pinned to snap for the same reason [harness.entries] is.
func (inst *harness) blocksOf(t *testing.T, snap time.Time, path string) []ladingdata.LadingBlock {
	t.Helper()
	type keyed struct {
		seq uint32
		row ladingdata.LadingBlock
	}
	var got []keyed
	for ent, err := range inst.data.ScanLadingBlock(context.Background(), recordstore.ScanOpts{
		ExtraPredicate: fmt.Sprintf("%s = %d AND %s = %s AND startsWith(%s, '%s\\0')",
			plain(t, "id"), testMount.Value(), plain(t, "ts"), tsLiteral(snap),
			plain(t, "naturalKey"), path),
	}) {
		require.NoError(t, err)
		require.True(t, ent.LadingBlock.Has)
		k := ent.NaturalKey
		require.Greater(t, len(k), 4)
		seq := uint32(k[len(k)-4])<<24 | uint32(k[len(k)-3])<<16 | uint32(k[len(k)-2])<<8 | uint32(k[len(k)-1])
		got = append(got, keyed{seq, ent.LadingBlock.Val})
	}
	out := make([]ladingdata.LadingBlock, len(got))
	for _, g := range got {
		require.Less(t, int(g.seq), len(out), "block ordinals must be dense from 0")
		out[g.seq] = g.row
	}
	return out
}

func plain(t *testing.T, name string) string {
	t.Helper()
	q, err := ladingschema.PhysicalPlainName(name)
	require.NoError(t, err)
	return q
}

// TestSnapshotWritesTheExpectedRows is the M2 acceptance: one walk of the
// fixture tree, read back row for row.
func TestSnapshotWritesTheExpectedRows(t *testing.T) {
	h := newHarness(t)
	fsys := tree()

	res, err := ladingingest.Snapshot(context.Background(), fsys, testMount, testPolicy(), h.stores())
	require.NoError(t, err)

	got := h.entries(t, res)

	// Every node of the tree, the root and the synthesised parents included.
	assert.ElementsMatch(t,
		[]string{".", "a", "a/b.txt", "a/c", "a/c/d.bin", "a/c/long.txt", "top.md", "big.bin", "empty", "link.txt"},
		keys(got))
	assert.EqualValues(t, len(got), res.Entries, "Result and the rows must agree on the count")

	// Directories and the empty one among them.
	assert.Equal(t, "dir", got["a"].NodeKind)
	assert.Equal(t, "dir", got["empty"].NodeKind)
	assert.Equal(t, "none", got["empty"].Content, "a directory has no content")

	// A symlink Lstats as a link and keeps its target verbatim, unresolved.
	assert.Equal(t, "symlink", got["link.txt"].NodeKind)
	assert.Equal(t, "a/b.txt", got["link.txt"].LinkTarget)
	assert.Equal(t, "none", got["link.txt"].Content,
		"the store records the link, never what it points at")

	// Content modes partition the regular files.
	assert.Equal(t, "blocks", got["a/b.txt"].Content)
	assert.Equal(t, "blocks", got["a/c/d.bin"].Content)
	assert.Equal(t, "blocks", got["top.md"].Content)
	assert.Equal(t, "ref", got["big.bin"].Content, "4096 bytes is over the 1024-byte threshold")
	assert.EqualValues(t, 4, res.Stored, "a/b.txt, a/c/d.bin, a/c/long.txt, top.md")
	assert.EqualValues(t, 1, res.Referenced, "big.bin")

	// Hashes are of the source bytes, checked against blake3 directly rather
	// than against the walker's own arithmetic.
	for _, path := range []string{"a/b.txt", "a/c/d.bin", "top.md", "big.bin"} {
		want := blake3.Sum256(fsys[path].Data)
		assert.Equalf(t, want[:], got[path].ContentHash, "content hash of %s", path)
	}
	assert.Empty(t, got["empty"].ContentHash, "a directory has no content hash")

	// Text classification, and what it promises.
	assert.True(t, got["a/b.txt"].Text, "40 short lines of UTF-8 is text")
	assert.False(t, got["a/c/d.bin"].Text, "a NUL byte is not text")
	assert.False(t, got["a/c/long.txt"].Text,
		"a line longer than a block cannot be newline-cut, so the flag must not claim it was")

	// Sizes and block counts describe the content that was stored.
	assert.EqualValues(t, len(fsys["a/b.txt"].Data), got["a/b.txt"].Size)
	assert.EqualValues(t, blockSize, got["a/b.txt"].BlockSize)
	assert.Greater(t, got["a/b.txt"].Blocks, uint32(1), "the fixture must span blocks or it proves nothing")
}

// TestTextBlocksEndAtNewlinesAndCarryLineNumbers is the invariant the grep
// query in ADR-0198 §7 rests on. If it fails, a single-line match can straddle
// two blocks and a line-numbered query is silently wrong.
func TestTextBlocksEndAtNewlinesAndCarryLineNumbers(t *testing.T) {
	h := newHarness(t)
	fsys := tree()
	res, err := ladingingest.Snapshot(context.Background(), fsys, testMount, testPolicy(), h.stores())
	require.NoError(t, err)

	blocks := h.blocksOf(t, res.Snap, "a/b.txt")
	require.NotEmpty(t, blocks)

	var rebuilt []byte
	wantLine := uint32(1)
	for i, b := range blocks {
		assert.Equalf(t, byte('\n'), b.Data[len(b.Data)-1], "text block %d must end at a newline", i)
		assert.LessOrEqualf(t, len(b.Data), blockSize, "block %d exceeds the block size", i)
		assert.Equalf(t, wantLine, b.Line0, "block %d starts at the wrong line", i)
		wantLine += uint32(bytes.Count(b.Data, []byte{'\n'}))
		rebuilt = append(rebuilt, b.Data...)

		// The per-block digest is a standalone BLAKE3 of the block, which is
		// what makes `BLAKE3(data) != hash` a valid audit in SQL.
		want := blake3.Sum256(b.Data)
		assert.Equalf(t, want[:], b.Hash, "block %d digest", i)
	}
	assert.Equal(t, fsys["a/b.txt"].Data, rebuilt, "the blocks must be the file")
	assert.EqualValues(t, 41, wantLine, "40 lines, so the line after the last is 41")

	// A non-text file is cut at exact offsets and claims no line numbers.
	bin := h.blocksOf(t, res.Snap, "a/c/d.bin")
	require.Len(t, bin, (200+blockSize-1)/blockSize)
	for i, b := range bin {
		assert.Zerof(t, b.Line0, "block %d of a binary file must not claim a line number", i)
		if i < len(bin)-1 {
			assert.Lenf(t, b.Data, blockSize, "block %d of a fixed cut", i)
		}
	}
	assert.Equal(t, fsys["a/c/d.bin"].Data, bytes.Join(dataOf(bin), nil))
}

// TestWalkErrorsBecomeRowsAndDoNotStopTheWalk. A tree with an unreadable node
// is still a snapshot, and the failure is queryable rather than lost.
func TestWalkErrorsBecomeRowsAndDoNotStopTheWalk(t *testing.T) {
	h := newHarness(t)
	fsys := tree()
	broken := failingFS{MapFS: fsys, fail: "a/c/d.bin"}

	res, err := ladingingest.Snapshot(context.Background(), broken, testMount, testPolicy(), h.stores())
	require.NoError(t, err, "one unreadable file must not fail the walk")
	assert.EqualValues(t, 1, res.Errors)

	got := h.entries(t, res)
	require.Contains(t, got, "a/c/d.bin")
	assert.Contains(t, got["a/c/d.bin"].Err, "broken by the test")
	assert.Equal(t, "none", got["a/c/d.bin"].Content, "an unread file stores no content")
	assert.Empty(t, got["a/c/d.bin"].ContentHash)

	// Everything past it is still there.
	assert.Contains(t, got, "top.md")
	assert.Contains(t, got, ".")
	assert.Equal(t, "blocks", got["top.md"].Content)
}

// TestRootRowIsLastAndCarriesTheCommitRecord. The commit rule: a snapshot is
// complete exactly when its root row exists, so the walker must write it after
// everything else and it must carry both components.
func TestRootRowIsLastAndCarriesTheCommitRecord(t *testing.T) {
	h := newHarness(t)
	res, err := ladingingest.Snapshot(context.Background(), tree(), testMount, testPolicy(), h.stores())
	require.NoError(t, err)

	commits := 0
	for ent, serr := range h.meta.ScanLadingSnapshot(context.Background(), recordstore.ScanOpts{
		ExtraPredicate: fmt.Sprintf("%s = %d", plain(t, "id"), testMount.Value()),
	}) {
		require.NoError(t, serr)
		commits++
		assert.Equal(t, ".", string(ent.NaturalKey), "only the root row is the commit record")
		require.True(t, ent.LadingSnapshot.Has)
		assert.Equal(t, res.Entries, ent.LadingSnapshot.Val.Entries)
		assert.Equal(t, res.Bytes, ent.LadingSnapshot.Val.Bytes)
		assert.Equal(t, "7d", ent.LadingSnapshot.Val.TtlClass, "the policy as applied, not as declared")
		assert.Equal(t, "sniff", ent.LadingSnapshot.Val.TextRule)
		assert.EqualValues(t, 1024, ent.LadingSnapshot.Val.InlineMax)
		assert.True(t, ent.LadingEntry.Has, "the root is a node too — Stat('.') has to work")
	}
	assert.Equal(t, 1, commits)
}

// TestAbortedWalkLeavesNoCompleteSnapshot. The reason there is no cleanup path
// anywhere in this package: an interrupted walk's rows are unreachable by
// construction, and TTL removes them.
func TestAbortedWalkLeavesNoCompleteSnapshot(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ladingingest.Snapshot(ctx, tree(), testMount, testPolicy(), h.stores())
	require.ErrorIs(t, err, context.Canceled)

	commits := 0
	for range h.meta.ScanLadingSnapshot(context.Background(), recordstore.ScanOpts{
		ExtraPredicate: fmt.Sprintf("%s = %d", plain(t, "id"), testMount.Value()),
	}) {
		commits++
	}
	assert.Zero(t, commits, "no root row, so no snapshot — whatever else was written")
}

// TestExpiryIsAWholeDayBoundary. Retention classes are whole days so that
// every row of a partition expires at the same instant; a partially expired
// part keeps its rows through every background merge.
func TestExpiryIsAWholeDayBoundary(t *testing.T) {
	h := newHarness(t)
	res, err := ladingingest.Snapshot(context.Background(), tree(), testMount, testPolicy(), h.stores())
	require.NoError(t, err)

	assert.Equal(t, time.UTC, res.ExpiresAt.Location())
	hh, mm, ss := res.ExpiresAt.Clock()
	assert.Equal(t, [3]int{0, 0, 0}, [3]int{hh, mm, ss}, "expiry must land on midnight UTC")
	assert.Zero(t, res.ExpiresAt.Nanosecond())

	y, m, d := res.Snap.UTC().Date()
	want := time.Date(y, m, d, 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1+7)
	assert.Equal(t, want, res.ExpiresAt, "end of the snapshot's day plus the class")
}

// TestMetaOnlyPolicyWritesNoBlocks. A stat-only mount still answers
// find-shaped questions and costs one row per node.
func TestMetaOnlyPolicyWritesNoBlocks(t *testing.T) {
	h := newHarness(t)
	p := testPolicy()
	p.MetaOnly = true

	res, err := ladingingest.Snapshot(context.Background(), tree(), testMount, p,
		lading.Stores{Meta: h.meta})
	require.NoError(t, err)
	assert.Zero(t, res.Blocks)
	assert.Zero(t, res.Stored)

	got := h.entries(t, res)
	for path, e := range got {
		assert.Equalf(t, "none", e.Content, "%s", path)
		assert.Emptyf(t, e.ContentHash, "%s", path)
	}
}

// TestPolicyRefusesWhatTheTablesCannotHold. Each of these fails late — at
// merge time or at read time — if it is not caught at the call.
func TestPolicyRefusesWhatTheTablesCannotHold(t *testing.T) {
	h := newHarness(t)
	for name, mutate := range map[string]func(*ladingingest.Policy){
		"no retention class": func(p *ladingingest.Policy) { p.Ttl = 0 },
		"no block size":      func(p *ladingingest.Policy) { p.Profile.BlockSize = 0 },
		"stores nothing":     func(p *ladingingest.Policy) { p.InlineMax = 0 },
	} {
		p := testPolicy()
		mutate(&p)
		_, err := ladingingest.Snapshot(context.Background(), tree(), testMount, p, h.stores())
		assert.Errorf(t, err, "%s must be refused", name)
	}

	// And a policy that produces blocks with nowhere to put them.
	_, err := ladingingest.Snapshot(context.Background(), tree(), testMount, testPolicy(),
		lading.Stores{Meta: h.meta})
	assert.Error(t, err, "a content policy without a block store must be refused")
}

// TestBlockCuttingIsExact covers the cut in isolation, at the boundaries the
// round-trip tests cannot reach cheaply.
func TestBlockCuttingIsExact(t *testing.T) {
	h := newHarness(t)
	p := testPolicy()

	for name, tc := range map[string]struct {
		data      []byte
		wantText  bool
		wantCount int
	}{
		// One block has no interior boundary, so the newline promise holds
		// vacuously and the flag is honest.
		"exactly one block":   {bytes.Repeat([]byte("a"), blockSize), true, 1},
		"one byte over":       {bytes.Repeat([]byte("a"), blockSize+1), false, 2},
		"empty file":          {nil, false, 0},
		"newline at the edge": {append(bytes.Repeat([]byte("a"), blockSize-1), '\n'), true, 1},
	} {
		fsys := fstest.MapFS{"f": {Data: tc.data, Mode: 0o644}}
		res, err := ladingingest.Snapshot(context.Background(), fsys, testMount, p, h.stores())
		require.NoErrorf(t, err, "%s", name)
		got := h.entries(t, res)
		assert.EqualValuesf(t, tc.wantCount, got["f"].Blocks, "%s: block count", name)
		assert.Equalf(t, tc.wantText, got["f"].Text, "%s: text", name)
		if tc.wantCount > 0 {
			assert.Equalf(t, tc.data, bytes.Join(dataOf(h.blocksOf(t, res.Snap, "f")), nil), "%s: round trip", name)
		}
		// Each case is its own snapshot; drop the rows so the next one reads
		// only its own.
		require.NoError(t, h.exec.Exec(context.Background(),
			fmt.Sprintf("DELETE FROM %s WHERE 1", ladingmeta.MetaTableName)))
		require.NoError(t, h.exec.Exec(context.Background(),
			fmt.Sprintf("DELETE FROM %s WHERE 1", ladingdata.DataTableName)))
	}
}

// --- helpers.

func keys(m map[string]ladingmeta.LadingEntry) (out []string) {
	for k := range m {
		out = append(out, k)
	}
	return
}

func dataOf(bs []ladingdata.LadingBlock) (out [][]byte) {
	for _, b := range bs {
		out = append(out, b.Data)
	}
	return
}

// failingFS makes one path unreadable, which fstest.MapFS has no way to
// express on its own.
//
// Both Open and ReadFile are overridden: fs.ReadFile takes the ReadFileFS
// fast path when the source has one, and MapFS does, so overriding Open alone
// leaves the walker reading the file successfully.
type failingFS struct {
	fstest.MapFS
	fail string
}

func (inst failingFS) Open(name string) (fs.File, error) {
	if name == inst.fail {
		return nil, inst.err(name)
	}
	return inst.MapFS.Open(name)
}

func (inst failingFS) ReadFile(name string) ([]byte, error) {
	if name == inst.fail {
		return nil, inst.err(name)
	}
	return inst.MapFS.ReadFile(name)
}

func (inst failingFS) err(name string) error {
	return &fs.PathError{Op: "open", Path: name, Err: fmt.Errorf("broken by the test")}
}

var _ fs.ReadLinkFS = failingFS{}
var _ = strings.TrimSpace

// tsLiteral renders an instant the way the store's key column holds it.
//
// fromUnixTimestamp64Nano, never toDateTime64: a plain number handed to
// toDateTime64 is read as seconds whatever the scale says, so a nanosecond
// value saturates to the year 2262 and the predicate matches nothing, with no
// error anywhere.
func tsLiteral(t time.Time) string {
	return fmt.Sprintf("fromUnixTimestamp64Nano(%d, 'UTC')", t.UTC().UnixNano())
}
