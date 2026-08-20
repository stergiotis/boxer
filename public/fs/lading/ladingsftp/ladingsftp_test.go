package ladingsftp_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"sort"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pkg/sftp"

	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingdata"
	"github.com/stergiotis/boxer/public/fs/lading/ladingingest"
	"github.com/stergiotis/boxer/public/fs/lading/ladingmeta"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsftp"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsql"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testMount identifier.TaggedId = 0xF5F5_0198_0005_0001

const blockSize = 64

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
		"a/b.txt":   {Data: text.Bytes(), Mode: 0o644, ModTime: time.Unix(1_700_000_001, 0)},
		"a/c/d.bin": {Data: bin, Mode: 0o600, ModTime: time.Unix(1_700_000_002, 0)},
		"top.md":    {Data: []byte("# hi\n"), Mode: 0o644, ModTime: time.Unix(1_700_000_003, 0)},
		"empty":     {Mode: fs.ModeDir | 0o755, ModTime: time.Unix(1_700_000_004, 0)},
		"link.txt":  {Data: []byte("a/b.txt"), Mode: fs.ModeSymlink | 0o777, ModTime: time.Unix(1_700_000_005, 0)},
	}
}

type rig struct {
	client *sftp.Client
	src    fstest.MapFS
	snaps  []time.Time
}

// serve seeds a store, stands a head over an in-memory pipe and hands back a
// real pkg/sftp client speaking to it.
//
// A socket pair rather than io.Pipe: the request server reads and writes
// concurrently, and a pair of half-duplex pipes deadlocks the moment it does
// both at once. This is also the shape the real head has under rclone — one
// bidirectional stream, no framing of our own.
func serve(t *testing.T, snapshots int) *rig {
	t.Helper()
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	ctx := context.Background()
	require.NoError(t, lading.Provision(ctx, exec, ladingschema.ProfileCorpus))

	st := lading.Stores{
		Meta: ladingmeta.NewMetaStore(exec, nil, ladingmeta.MetaStoreConfig{}),
		Data: ladingdata.NewDataStore(exec, nil, ladingdata.DataStoreConfig{}),
	}
	t.Cleanup(func() { st.Meta.Close(); st.Data.Close() })

	pol := ladingingest.DefaultPolicy()
	pol.Ttl = ladingingest.TtlClass7d
	pol.Profile.BlockSize = blockSize
	pol.Profile.PerBlockHash = true

	src := source()
	r := &rig{src: src}
	for i := range snapshots {
		if i > 0 {
			src = source()
			src[fmt.Sprintf("gen%d.txt", i)] = &fstest.MapFile{Data: []byte("later\n"), Mode: 0o644}
		}
		res, serr := ladingingest.Snapshot(ctx, src, testMount, pol, st)
		require.NoError(t, serr)
		r.snaps = append(r.snaps, res.Snap)
	}

	head, err := ladingsftp.New(ladingsftp.Config{
		Exec: exec, Stores: st, Visibility: ladingsql.VisibleAll{},
	})
	require.NoError(t, err)

	ours, theirs := net.Pipe()
	go func() { _ = head.Serve(ours) }()

	cl, err := sftp.NewClientPipe(theirs, theirs)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	r.client = cl
	return r
}

func names(t *testing.T, infos []fs.FileInfo) (out []string) {
	t.Helper()
	for _, i := range infos {
		out = append(out, i.Name())
	}
	sort.Strings(out)
	return
}

func snapName(ts time.Time) string { return ts.UTC().Format("20060102T150405.000000000Z") }

func mountDir() string { return fmt.Sprintf("%016x", testMount.Value()) }

// TestTheTreeHasThreeLevels — the shape §SD9 specifies, walked by a real SFTP
// client rather than asserted against the head's own idea of itself.
func TestTheTreeHasThreeLevels(t *testing.T) {
	r := serve(t, 2)

	root, err := r.client.ReadDir("/")
	require.NoError(t, err)
	assert.Equal(t, []string{mountDir()}, names(t, root), "the root lists mounts")
	assert.True(t, root[0].IsDir())

	mount, err := r.client.ReadDir("/" + mountDir())
	require.NoError(t, err)
	want := []string{"latest", snapName(r.snaps[0]), snapName(r.snaps[1])}
	sort.Strings(want)
	assert.Equal(t, want, names(t, mount), "a mount lists its snapshots and the latest link")

	tree, err := r.client.ReadDir("/" + mountDir() + "/" + snapName(r.snaps[0]))
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "empty", "link.txt", "top.md"}, names(t, tree))
}

// TestLatestIsASymlink — the only mutable name in the tree, which is what
// makes a long-lived VFS cache of a snapshot path safe.
func TestLatestIsASymlink(t *testing.T) {
	r := serve(t, 2)
	p := "/" + mountDir() + "/latest"

	li, err := r.client.Lstat(p)
	require.NoError(t, err)
	assert.NotZero(t, li.Mode()&fs.ModeSymlink, "Lstat must see the link")

	target, err := r.client.ReadLink(p)
	require.NoError(t, err)
	assert.Equal(t, snapName(r.snaps[1]), target, "it points at the newest snapshot")

	si, err := r.client.Stat(p)
	require.NoError(t, err)
	assert.True(t, si.IsDir(), "Stat follows it to a directory")

	// A path *through* the link resolves, the way it does on any file system:
	// walking a symlink for a path operation is the server's job, not
	// something a client should have to re-issue.
	viaLink, err := r.client.Stat(p + "/top.md")
	require.NoError(t, err)
	byName, err := r.client.Stat("/" + mountDir() + "/" + snapName(r.snaps[1]) + "/top.md")
	require.NoError(t, err)
	assert.Equal(t, byName.Size(), viaLink.Size())

	// And a snapshot's own symlinks read through unchanged.
	inner, err := r.client.ReadLink("/" + mountDir() + "/" + snapName(r.snaps[0]) + "/link.txt")
	require.NoError(t, err)
	assert.Equal(t, "a/b.txt", inner, "a link the walk recorded is served verbatim")
}

// TestReadingAFileOverThePipe — the bytes out are the bytes in, for both the
// newline-cut and the fixed-cut path.
func TestReadingAFileOverThePipe(t *testing.T) {
	r := serve(t, 1)
	base := "/" + mountDir() + "/" + snapName(r.snaps[0])

	for _, name := range []string{"a/b.txt", "a/c/d.bin", "top.md"} {
		f, err := r.client.Open(base + "/" + name)
		require.NoErrorf(t, err, "open %s", name)
		got, err := io.ReadAll(f)
		require.NoErrorf(t, err, "read %s", name)
		require.NoError(t, f.Close())
		assert.Equalf(t, r.src[name].Data, got, "content of %s", name)
	}

	// Stat over the wire agrees with the source.
	fi, err := r.client.Stat(base + "/a/c/d.bin")
	require.NoError(t, err)
	assert.EqualValues(t, len(r.src["a/c/d.bin"].Data), fi.Size())
	assert.True(t, r.src["a/c/d.bin"].ModTime.UTC().Equal(fi.ModTime()), "mtime survives the wire")
}

// TestChunkedReadsCrossBlockBoundaries. rclone reads in chunks unrelated to the
// store's blocks; this is the case where wrong offset arithmetic returns
// plausible bytes from the wrong place.
func TestChunkedReadsCrossBlockBoundaries(t *testing.T) {
	r := serve(t, 1)
	base := "/" + mountDir() + "/" + snapName(r.snaps[0])
	want := r.src["a/c/d.bin"].Data

	f, err := r.client.Open(base + "/a/c/d.bin")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	for _, off := range []int64{0, 1, blockSize - 1, blockSize, 2*blockSize + 3} {
		for _, n := range []int{1, blockSize - 1, blockSize + 5, 3 * blockSize} {
			p := make([]byte, n)
			got, rerr := f.ReadAt(p, off)
			if rerr != nil && rerr != io.EOF {
				require.NoErrorf(t, rerr, "ReadAt(%d @ %d)", n, off)
			}
			end := min(off+int64(n), int64(len(want)))
			assert.Equalf(t, want[off:end], p[:got], "ReadAt(%d bytes @ %d)", n, off)
		}
	}
}

// TestEveryWriteIsRefused. The store has no update path (§SD1), so a head that
// accepted a write would have to invent one.
func TestEveryWriteIsRefused(t *testing.T) {
	r := serve(t, 1)
	base := "/" + mountDir() + "/" + snapName(r.snaps[0])

	_, err := r.client.Create(base + "/new.txt")
	assert.Error(t, err, "create")
	assert.Error(t, r.client.Remove(base+"/top.md"), "remove")
	assert.Error(t, r.client.Mkdir(base+"/newdir"), "mkdir")
	assert.Error(t, r.client.Rename(base+"/top.md", base+"/other.md"), "rename")
	assert.Error(t, r.client.Symlink("top.md", base+"/newlink"), "symlink")
	assert.Error(t, r.client.Chmod(base+"/top.md", 0o777), "chmod")

	// And nothing changed.
	fi, err := r.client.Stat(base + "/top.md")
	require.NoError(t, err)
	assert.EqualValues(t, len(r.src["top.md"].Data), fi.Size())
}

// TestAnInvisibleMountIsAbsent, not forbidden — a tree that answered
// "permission denied" for one name and "no such file" for another would let a
// client enumerate what it cannot read.
func TestAnInvisibleMountIsAbsent(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	ctx := context.Background()
	require.NoError(t, lading.Provision(ctx, exec, ladingschema.ProfileCorpus))
	st := lading.Stores{
		Meta: ladingmeta.NewMetaStore(exec, nil, ladingmeta.MetaStoreConfig{}),
		Data: ladingdata.NewDataStore(exec, nil, ladingdata.DataStoreConfig{}),
	}
	defer st.Meta.Close()
	defer st.Data.Close()

	pol := ladingingest.DefaultPolicy()
	pol.Ttl = ladingingest.TtlClass7d
	pol.Profile.BlockSize = blockSize
	res, err := ladingingest.Snapshot(ctx, source(), testMount, pol, st)
	require.NoError(t, err)

	// A head that may see nothing.
	head, err := ladingsftp.New(ladingsftp.Config{
		Exec: exec, Stores: st, Visibility: ladingsql.VisibleSet{},
	})
	require.NoError(t, err)
	ours, theirs := net.Pipe()
	go func() { _ = head.Serve(ours) }()
	cl, err := sftp.NewClientPipe(theirs, theirs)
	require.NoError(t, err)
	defer func() { _ = cl.Close() }()

	root, err := cl.ReadDir("/")
	require.NoError(t, err)
	assert.Empty(t, root, "an invisible mount is not listed")

	_, err = cl.ReadDir("/" + mountDir())
	assert.Error(t, err, "and not enterable")
	_, err = cl.Stat("/" + mountDir() + "/" + snapName(res.Snap) + "/top.md")
	assert.Error(t, err, "nor readable by guessing a path inside it")
}

// TestPathsThatEscapeTheRootAreRefused. SFTP paths arrive client-normalised,
// but that is the client's word for it.
func TestPathsThatEscapeTheRootAreRefused(t *testing.T) {
	r := serve(t, 1)
	for _, p := range []string{
		"/../etc/passwd",
		"/" + mountDir() + "/../../etc/passwd",
		"/nosuchmount",
		"/" + mountDir() + "/notasnapshot",
		"/" + mountDir() + "/latest/nosuchfile",
	} {
		_, err := r.client.Stat(p)
		assert.Errorf(t, err, "%s must not resolve", p)
	}
}

// TestConcurrentReadsAreSerialised. The request server answers packets
// concurrently and the store underneath is single-goroutine; without the
// head's lock this races, and a race here corrupts a decode rather than
// failing cleanly.
func TestConcurrentReadsAreSerialised(t *testing.T) {
	r := serve(t, 1)
	base := "/" + mountDir() + "/" + snapName(r.snaps[0])

	const readers = 8
	errs := make(chan error, readers)
	for range readers {
		go func() {
			f, err := r.client.Open(base + "/a/b.txt")
			if err != nil {
				errs <- err
				return
			}
			defer func() { _ = f.Close() }()
			got, rerr := io.ReadAll(f)
			if rerr != nil {
				errs <- rerr
				return
			}
			if !bytes.Equal(got, r.src["a/b.txt"].Data) {
				errs <- fmt.Errorf("content mismatch: %d bytes", len(got))
				return
			}
			errs <- nil
		}()
	}
	for range readers {
		require.NoError(t, <-errs)
	}
}
