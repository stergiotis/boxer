package ladingadapter_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"path"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingadapter"
	"github.com/stergiotis/boxer/public/fs/lading/ladingdata"
	"github.com/stergiotis/boxer/public/fs/lading/ladingingest"
	"github.com/stergiotis/boxer/public/fs/lading/ladingmeta"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testMount identifier.TaggedId = 0xF5F5_0198_0003_0001

// blockSize is small so the fixture spans blocks without being large. It is
// what makes the block-boundary reads worth checking at all.
const blockSize = 64

// source is the tree most tests here snapshot and then read back.
//
// It carries one of each thing the adapter has to get right: a text file over
// several blocks, a binary file cut at fixed offsets, a file whose content the
// policy leaves referenced, an empty directory, a symlink to a file, a symlink
// to a directory, a relative symlink one level down, and a broken one.
func source() fstest.MapFS {
	var text bytes.Buffer
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&text, "line %d\n", i)
	}
	bin := make([]byte, 500)
	for i := range bin {
		bin[i] = byte(i * 7)
	}
	return fstest.MapFS{
		"a/b.txt":     {Data: text.Bytes(), Mode: 0o644, ModTime: time.Unix(1_700_000_001, 0)},
		"a/c/d.bin":   {Data: bin, Mode: 0o600, ModTime: time.Unix(1_700_000_002, 0)},
		"a/c/tiny":    {Data: []byte("x"), Mode: 0o644, ModTime: time.Unix(1_700_000_003, 0)},
		"top.md":      {Data: []byte("# hi\n"), Mode: 0o644, ModTime: time.Unix(1_700_000_004, 0)},
		"big.bin":     {Data: bytes.Repeat([]byte("B"), 4096), Mode: 0o644, ModTime: time.Unix(1_700_000_005, 0)},
		"empty":       {Mode: fs.ModeDir | 0o755, ModTime: time.Unix(1_700_000_006, 0)},
		"link.txt":    {Data: []byte("a/b.txt"), Mode: fs.ModeSymlink | 0o777, ModTime: time.Unix(1_700_000_007, 0)},
		"linkdir":     {Data: []byte("a/c"), Mode: fs.ModeSymlink | 0o777, ModTime: time.Unix(1_700_000_008, 0)},
		"a/up.txt":    {Data: []byte("b.txt"), Mode: fs.ModeSymlink | 0o777, ModTime: time.Unix(1_700_000_009, 0)},
		"broken.link": {Data: []byte("nowhere"), Mode: fs.ModeSymlink | 0o777, ModTime: time.Unix(1_700_000_010, 0)},
	}
}

// readableSource is [source] minus the two nodes no file system can hand back
// bytes for: the entry whose content is only referenced, and the symlink that
// points at nothing.
//
// They are dropped for the conformance test alone, and the reason is
// fstest.TestFS's contract rather than a limitation here: it opens and reads
// everything it can reach, so a tree where some node deliberately cannot be
// read is a tree it must report. A real snapshot has both — which is why they
// keep their own tests below.
func readableSource() fstest.MapFS {
	src := source()
	delete(src, "big.bin")
	delete(src, "broken.link")
	return src
}

func policy() ladingingest.Policy {
	p := ladingingest.DefaultPolicy()
	p.Ttl = ladingingest.TtlClass7d
	p.InlineMax = 1024 // big.bin lands above it
	p.Profile.BlockSize = blockSize
	p.Profile.PerBlockHash = true
	return p
}

type harness struct {
	exec   recordstore.ExecutorI
	stores lading.Stores
	res    ladingingest.Result
	src    fstest.MapFS
}

// seed provisions a throwaway store, walks the fixture into it and hands back
// a reader. clickhouse-local rather than a fake: the point of this milestone is
// that the rows the walker wrote read back as a file system, and a fake store
// would prove neither half.
func seed(t *testing.T) *harness { return seedFrom(t, source()) }

func seedFrom(t *testing.T, src fstest.MapFS) *harness {
	t.Helper()
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	require.NoError(t, lading.Provision(ctx, exec, ladingschema.ProfileCorpus))

	st := lading.Stores{
		Meta: ladingmeta.NewMetaStore(exec, nil, ladingmeta.MetaStoreConfig{}),
		Data: ladingdata.NewDataStore(exec, nil, ladingdata.DataStoreConfig{}),
	}
	t.Cleanup(func() { st.Meta.Close(); st.Data.Close() })

	res, err := ladingingest.Snapshot(ctx, src, testMount, policy(), st)
	require.NoError(t, err)
	return &harness{exec: exec, stores: st, res: res, src: src}
}

func (inst *harness) open(t *testing.T, opts ...ladingadapter.Option) *ladingadapter.FS {
	t.Helper()
	fsys, err := ladingadapter.Open(inst.stores, testMount, inst.res.Snap, opts...)
	require.NoError(t, err)
	return fsys
}

// TestFstestConformance is M3's acceptance: the snapshot the walker wrote is a
// well-behaved io/fs, judged by the standard library's own conformance test.
//
// Over [readableSource] rather than the full fixture — see there. The links it
// does keep are resolvable, and TestFS follows them like any other io/fs
// caller would.
func TestFstestConformance(t *testing.T) {
	h := seedFrom(t, readableSource())
	fsys := h.open(t)
	require.NoError(t, fstest.TestFS(fsys,
		"a", "a/b.txt", "a/c", "a/c/d.bin", "a/c/tiny", "top.md", "empty"))
}

// TestStatMatchesTheSource, field for field, for every node the walk stored.
func TestStatMatchesTheSource(t *testing.T) {
	h := seed(t)
	fsys := h.open(t)

	for _, name := range []string{"a/b.txt", "a/c/d.bin", "a/c/tiny", "top.md", "big.bin"} {
		got, err := fsys.Stat(name)
		require.NoErrorf(t, err, "stat %s", name)
		want := h.src[name]
		assert.Equalf(t, int64(len(want.Data)), got.Size(), "size of %s", name)
		assert.Equalf(t, want.Mode, got.Mode(), "mode of %s", name)
		assert.Truef(t, want.ModTime.UTC().Equal(got.ModTime()), "mtime of %s: %v vs %v", name, want.ModTime, got.ModTime())
		assert.Falsef(t, got.IsDir(), "%s is not a directory", name)
	}

	dir, err := fsys.Stat("a/c")
	require.NoError(t, err)
	assert.True(t, dir.IsDir())
	assert.Equal(t, "c", dir.Name(), "FileInfo.Name is the base name, not the path")

	root, err := fsys.Stat(".")
	require.NoError(t, err)
	assert.True(t, root.IsDir(), "the root row stats as a directory")
}

// TestReadFileRoundTripsEveryStoredFile — the bytes out are the bytes in,
// through cutting, storage, decode and reassembly.
func TestReadFileRoundTripsEveryStoredFile(t *testing.T) {
	h := seed(t)
	fsys := h.open(t)
	for _, name := range []string{"a/b.txt", "a/c/d.bin", "a/c/tiny", "top.md"} {
		got, err := fs.ReadFile(fsys, name)
		require.NoErrorf(t, err, "read %s", name)
		assert.Equalf(t, h.src[name].Data, got, "content of %s", name)
	}
}

// TestReadAtAcrossBlockBoundaries. Every offset and length combination over a
// fixed-cut file that spans blocks, checked against the source — this is where
// the ordinal arithmetic either holds or quietly returns the wrong bytes.
func TestReadAtAcrossBlockBoundaries(t *testing.T) {
	h := seed(t)
	fsys := h.open(t)
	want := h.src["a/c/d.bin"].Data
	require.Greater(t, len(want), 3*blockSize, "the fixture must span several blocks")

	f, err := fsys.Open("a/c/d.bin")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	ra, ok := f.(io.ReaderAt)
	require.True(t, ok, "a File must satisfy io.ReaderAt")

	for _, off := range []int64{0, 1, blockSize - 1, blockSize, blockSize + 1, 2*blockSize - 1, int64(len(want)) - 1} {
		for _, n := range []int{1, 2, blockSize - 1, blockSize, blockSize + 1, 3 * blockSize} {
			p := make([]byte, n)
			got, rerr := ra.ReadAt(p, off)
			end := min(off+int64(n), int64(len(want)))
			expect := want[off:end]
			assert.Equalf(t, len(expect), got, "ReadAt(%d bytes @ %d): length", n, off)
			assert.Equalf(t, expect, p[:got], "ReadAt(%d bytes @ %d): bytes", n, off)
			if int64(got) < int64(n) {
				assert.ErrorIsf(t, rerr, io.EOF, "a short ReadAt must report EOF (%d @ %d)", n, off)
			} else {
				assert.NoErrorf(t, rerr, "ReadAt(%d bytes @ %d)", n, off)
			}
		}
	}
}

// TestReadAtWorksOnNewlineCutFilesToo. A text file's blocks end where its
// newlines were, so an offset says nothing about an ordinal — the adapter
// materialises it instead. The caller must not be able to tell.
func TestReadAtWorksOnNewlineCutFilesToo(t *testing.T) {
	h := seed(t)
	fsys := h.open(t)
	want := h.src["a/b.txt"].Data

	f, err := fsys.Open("a/b.txt")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	ra := f.(io.ReaderAt)

	for _, off := range []int64{0, 7, blockSize + 3, int64(len(want)) - 5} {
		p := make([]byte, 40)
		n, _ := ra.ReadAt(p, off)
		end := min(off+40, int64(len(want)))
		assert.Equalf(t, want[off:end], p[:n], "ReadAt @ %d over a newline-cut file", off)
	}
}

// TestSequentialReadAndSeek — Read advances, Seek moves, and the two agree.
func TestSequentialReadAndSeek(t *testing.T) {
	h := seed(t)
	fsys := h.open(t)
	want := h.src["a/c/d.bin"].Data

	f, err := fsys.Open("a/c/d.bin")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Read the whole file in chunks that do not align with the blocks.
	var got []byte
	buf := make([]byte, 37)
	for {
		n, rerr := f.Read(buf)
		got = append(got, buf[:n]...)
		if rerr == io.EOF {
			break
		}
		require.NoError(t, rerr)
	}
	assert.Equal(t, want, got, "a chunked sequential read must reassemble the file")

	sk := f.(io.Seeker)
	pos, err := sk.Seek(int64(blockSize)+5, io.SeekStart)
	require.NoError(t, err)
	assert.EqualValues(t, blockSize+5, pos)
	p := make([]byte, 10)
	n, _ := f.Read(p)
	assert.Equal(t, want[blockSize+5:blockSize+5+10], p[:n])

	pos, err = sk.Seek(-4, io.SeekEnd)
	require.NoError(t, err)
	assert.EqualValues(t, len(want)-4, pos)
	n, _ = f.Read(p)
	assert.Equal(t, want[len(want)-4:], p[:n])
}

// TestReadDirIsSortedAndComplete. fs.ReadDirFS requires filename order, and
// the store's own key orders by the whole path — a different question.
func TestReadDirIsSortedAndComplete(t *testing.T) {
	h := seed(t)
	fsys := h.open(t)

	got, err := fs.ReadDir(fsys, "a")
	require.NoError(t, err)
	names := make([]string, 0, len(got))
	for _, d := range got {
		names = append(names, d.Name())
	}
	assert.Equal(t, []string{"b.txt", "c", "up.txt"}, names,
		"bytewise by name, and every child exactly once")

	root, err := fs.ReadDir(fsys, ".")
	require.NoError(t, err)
	rootNames := make([]string, 0, len(root))
	for _, d := range root {
		rootNames = append(rootNames, d.Name())
	}
	assert.Equal(t, []string{"a", "big.bin", "broken.link", "empty", "link.txt", "linkdir", "top.md"}, rootNames)

	empty, err := fs.ReadDir(fsys, "empty")
	require.NoError(t, err)
	assert.Empty(t, empty, "an empty directory lists as empty, not as missing")
}

// TestSymlinksAreRecordedAndResolved. Lstat and ReadLink serve the link the
// walker recorded; Stat and Open follow it, which is what io/fs expects of a
// ReadLinkFS.
func TestSymlinksAreRecordedAndResolved(t *testing.T) {
	h := seed(t)
	fsys := h.open(t)

	li, err := fsys.Lstat("link.txt")
	require.NoError(t, err)
	assert.NotZero(t, li.Mode()&fs.ModeSymlink, "Lstat must not follow")

	target, err := fsys.ReadLink("link.txt")
	require.NoError(t, err)
	assert.Equal(t, "a/b.txt", target, "the target verbatim, unresolved")

	si, err := fsys.Stat("link.txt")
	require.NoError(t, err)
	assert.Zero(t, si.Mode()&fs.ModeSymlink, "Stat follows")
	assert.EqualValues(t, len(h.src["a/b.txt"].Data), si.Size())

	got, err := fs.ReadFile(fsys, "link.txt")
	require.NoError(t, err)
	assert.Equal(t, h.src["a/b.txt"].Data, got, "opening a link opens its target")

	// A relative target resolves against the link's own directory, not the root.
	rel, err := fs.ReadFile(fsys, "a/up.txt")
	require.NoError(t, err)
	assert.Equal(t, h.src["a/b.txt"].Data, rel)

	// A link to a directory lists as that directory.
	kids, err := fs.ReadDir(fsys, "linkdir")
	require.NoError(t, err)
	names := make([]string, 0, len(kids))
	for _, d := range kids {
		names = append(names, d.Name())
	}
	assert.Equal(t, []string{"d.bin", "tiny"}, names)

	// A broken link Lstats and ReadLinks, and fails to resolve.
	_, err = fsys.Lstat("broken.link")
	assert.NoError(t, err, "a broken link is still a node")
	_, err = fsys.Stat("broken.link")
	assert.ErrorIs(t, err, fs.ErrNotExist, "resolving it must fail like any missing path")

	// And ReadLink on something that is not a link is an error, not a guess.
	_, err = fsys.ReadLink("top.md")
	assert.Error(t, err)
}

// TestSubIsTheSameSnapshot.
func TestSubIsTheSameSnapshot(t *testing.T) {
	h := seed(t)
	fsys := h.open(t)

	sub, err := fs.Sub(fsys, "a")
	require.NoError(t, err)
	got, err := fs.ReadFile(sub, "b.txt")
	require.NoError(t, err)
	assert.Equal(t, h.src["a/b.txt"].Data, got)

	names, err := fs.ReadDir(sub, "c")
	require.NoError(t, err)
	assert.Len(t, names, 2)

	// A Sub of a Sub composes.
	deep, err := fs.Sub(sub, "c")
	require.NoError(t, err)
	tiny, err := fs.ReadFile(deep, "tiny")
	require.NoError(t, err)
	assert.Equal(t, []byte("x"), tiny)

	// And it cannot reach out of its subtree.
	_, err = fs.ReadFile(sub, "../top.md")
	assert.Error(t, err, "'..' is not a valid io/fs name")
	_, err = fs.ReadFile(sub, "top.md")
	assert.ErrorIs(t, err, fs.ErrNotExist, "a name outside the subtree is simply absent")
}

// TestUnstoredContentFailsTypedRatherThanEmpty. The distinction the store
// keeps: an entry with no bytes is not an entry with zero bytes.
func TestUnstoredContentFailsTypedRatherThanEmpty(t *testing.T) {
	h := seed(t)
	fsys := h.open(t)

	_, err := fs.ReadFile(fsys, "big.bin")
	assert.ErrorIs(t, err, ladingadapter.ErrReferenced,
		"a file over the inline threshold has a size, an mtime and a hash — but no bytes here")

	_, err = fs.ReadFile(fsys, "empty")
	assert.Error(t, err, "a directory has no content to read")

	// With a fetcher, the same entry serves.
	served := h.open(t, ladingadapter.WithSourceFetcher(fakeFetcher{h.src["big.bin"].Data}))
	got, err := fs.ReadFile(served, "big.bin")
	require.NoError(t, err)
	assert.Equal(t, h.src["big.bin"].Data, got)
}

// TestGlob.
func TestGlob(t *testing.T) {
	h := seed(t)
	fsys := h.open(t)

	got, err := fs.Glob(fsys, "a/*")
	require.NoError(t, err)
	assert.Equal(t, []string{"a/b.txt", "a/c", "a/up.txt"}, got)

	got, err = fs.Glob(fsys, "*.md")
	require.NoError(t, err)
	assert.Equal(t, []string{"top.md"}, got)

	// A pattern with no meta characters is one lookup, and misses cleanly.
	got, err = fs.Glob(fsys, "top.md")
	require.NoError(t, err)
	assert.Equal(t, []string{"top.md"}, got)
	got, err = fs.Glob(fsys, "nope")
	require.NoError(t, err)
	assert.Empty(t, got)

	_, err = fs.Glob(fsys, "[")
	assert.ErrorIs(t, err, path.ErrBadPattern)
}

// TestMissingAndInvalidNames.
func TestMissingAndInvalidNames(t *testing.T) {
	h := seed(t)
	fsys := h.open(t)

	_, err := fsys.Stat("nope.txt")
	assert.ErrorIs(t, err, fs.ErrNotExist)
	_, err = fsys.Open("a/nope/deeper.txt")
	assert.ErrorIs(t, err, fs.ErrNotExist)

	for _, bad := range []string{"", "/abs", "a/../b", "./a", "a/"} {
		_, err = fsys.Stat(bad)
		assert.ErrorIsf(t, err, fs.ErrInvalid, "%q is not a valid io/fs name", bad)
	}
}

// TestAnEmptySnapshotIsNotAView. Opening an instant no walk ever committed
// gives a file system with nothing in it — not an error at Open, because Open
// reads nothing, and not a panic later.
func TestAnEmptySnapshotIsNotAView(t *testing.T) {
	h := seed(t)
	fsys, err := ladingadapter.Open(h.stores, testMount, h.res.Snap.Add(-time.Hour))
	require.NoError(t, err)
	_, err = fsys.Stat(".")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

type fakeFetcher struct{ data []byte }

func (inst fakeFetcher) FetchContent(_ context.Context, _ identifier.TaggedId, _ time.Time, _ string, _ []byte) ([]byte, error) {
	return inst.data, nil
}

// countingExec wraps an executor and counts the SELECTs that reach each table,
// so a test can assert on how a read decomposes rather than only on its bytes.
type countingExec struct {
	recordstore.ExecutorI
	meta, data int
}

func (inst *countingExec) QueryArrow(ctx context.Context, sql string) iter.Seq2[arrow.RecordBatch, error] {
	switch {
	case strings.Contains(sql, ladingschema.TableNameData):
		inst.data++
	case strings.Contains(sql, ladingschema.TableNameMeta):
		inst.meta++
	}
	return inst.ExecutorI.QueryArrow(ctx, sql)
}

// TestAChunkedReadIsOneQueryPerBlockRange. The property the SFTP head will
// depend on: rclone and friends read in chunks far smaller than a block, and a
// query per chunk would be a query per 32 KiB of a large file.
//
// Counted rather than reasoned about, because the failure mode is invisible —
// a per-chunk implementation returns exactly the same bytes.
func TestAChunkedReadIsOneQueryPerBlockRange(t *testing.T) {
	h := seed(t)
	counted := &countingExec{ExecutorI: h.exec}
	st := lading.Stores{
		Meta: ladingmeta.NewMetaStore(counted, nil, ladingmeta.MetaStoreConfig{}),
		Data: ladingdata.NewDataStore(counted, nil, ladingdata.DataStoreConfig{}),
	}
	defer st.Meta.Close()
	defer st.Data.Close()

	fsys, err := ladingadapter.Open(st, testMount, h.res.Snap)
	require.NoError(t, err)
	f, err := fsys.Open("a/c/d.bin")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	want := h.src["a/c/d.bin"].Data
	before := counted.data

	// Read the whole file in chunks a quarter of a block wide.
	var got []byte
	buf := make([]byte, blockSize/4)
	for {
		n, rerr := f.Read(buf)
		got = append(got, buf[:n]...)
		if rerr == io.EOF {
			break
		}
		require.NoError(t, rerr)
	}
	assert.Equal(t, want, got)

	queries := counted.data - before
	blocks := (len(want) + blockSize - 1) / blockSize
	chunks := (len(want) + blockSize/4 - 1) / (blockSize / 4)
	assert.LessOrEqual(t, queries, blocks,
		"at most one block query per block; got %d for %d blocks read in %d chunks", queries, blocks, chunks)
	assert.Less(t, queries, chunks, "a query per chunk is the failure this test exists for")
}
