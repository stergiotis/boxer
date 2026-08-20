//go:build integration

package ladingremote_test

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/observability/eh"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingadapter"
	"github.com/stergiotis/boxer/public/fs/lading/ladingdata"
	"github.com/stergiotis/boxer/public/fs/lading/ladingingest"
	"github.com/stergiotis/boxer/public/fs/lading/ladingmeta"
	"github.com/stergiotis/boxer/public/fs/lading/ladingremote"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The two mounts this file uses: one snapshotted straight off the disk, one
// through rclone. Comparing them is the milestone's acceptance.
const (
	mountDirect identifier.TaggedId = 0xF5F5_0198_0006_0001
	mountRemote identifier.TaggedId = 0xF5F5_0198_0006_0002
)

// corpusDir writes the tree both paths will snapshot.
//
// No symlink: `rclone serve sftp` exposes one only under `--links`, so a tree
// with one would differ between the two paths for a reason that says nothing
// about either. TestRemoteSymlinksSurviveUnderLinks covers that on purpose.
func corpusDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "a", "c"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "empty"), 0o755))

	var text bytes.Buffer
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&text, "line %d\n", i)
	}
	bin := make([]byte, 4096)
	for i := range bin {
		bin[i] = byte(i * 7)
	}
	// Whole seconds: SFTP carries no more, so a source with sub-second times
	// would make the comparison fail on the transport's resolution rather than
	// on anything either side did.
	mt := time.Unix(1_700_000_000, 0)
	write := func(rel string, data []byte, mode os.FileMode, off time.Duration) {
		p := filepath.Join(dir, rel)
		require.NoError(t, os.WriteFile(p, data, mode))
		require.NoError(t, os.Chmod(p, mode))
		require.NoError(t, os.Chtimes(p, mt.Add(off), mt.Add(off)))
	}
	write("a/b.txt", text.Bytes(), 0o644, 0)
	write("a/c/d.bin", bin, 0o600, time.Minute)
	write("top.md", []byte("# hi\n"), 0o644, 2*time.Minute)
	write("a/tiny", []byte("x"), 0o644, 3*time.Minute)
	return dir
}

type harness struct {
	exec   recordstore.ExecutorI
	stores lading.Stores
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	cfg := chclient.ConfigFromEnv()
	client := chclient.New(cfg, nil)
	if err := client.Ping(context.Background()); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", cfg.URL, err)
	}
	ex, err := storeexec.New(client, nil)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, lading.Provision(ctx, ex, ladingschema.ProfileCorpus))

	h := &harness{exec: ex, stores: lading.Stores{
		Meta: ladingmeta.NewMetaStore(ex, nil, ladingmeta.MetaStoreConfig{}),
		Data: ladingdata.NewDataStore(ex, nil, ladingdata.DataStoreConfig{}),
	}}
	h.purge(t)
	t.Cleanup(func() {
		h.stores.Meta.Close()
		h.stores.Data.Close()
		h.purge(t)
	})
	return h
}

func (inst *harness) purge(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	key, err := ladingschema.PhysicalPlainName("id")
	require.NoError(t, err)
	for _, m := range []identifier.TaggedId{mountDirect, mountRemote} {
		for _, tbl := range []string{
			ladingschema.TableNameMeta, ladingschema.TableNameData, ladingschema.TableNameSnap,
		} {
			require.NoError(t, inst.exec.Exec(ctx, fmt.Sprintf("DELETE FROM %s.%s WHERE %s = %d",
				ladingschema.DatabaseName, tbl, key, m.Value())))
		}
	}
}

func policy() ladingingest.Policy {
	p := ladingingest.DefaultPolicy()
	p.Ttl = ladingingest.TtlClass7d
	p.Profile.BlockSize = 512
	p.Profile.PerBlockHash = true
	return p
}

// openRemote spawns rclone over dir and returns the fs.FS it serves.
func openRemote(t *testing.T, dir string, opts ...ladingremote.Option) *ladingremote.Remote {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	src, err := ladingremote.Serve(ctx, dir, opts...)
	if err != nil {
		t.Skipf("rclone unavailable: %v", err)
	}
	t.Cleanup(func() { _ = src.Close() })
	return src
}

// TestARemoteWalkEqualsALocalWalk is M6's acceptance: the same directory
// snapshotted twice — once straight off the disk, once through
// `rclone serve sftp --stdio` — agrees, up to what SFTP can carry.
func TestARemoteWalkEqualsALocalWalk(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	dir := corpusDir(t)

	direct, err := ladingingest.Snapshot(ctx, os.DirFS(dir), mountDirect, policy(), h.stores)
	require.NoError(t, err)

	src := openRemote(t, dir)
	remote, err := ladingingest.Snapshot(ctx, src, mountRemote, policy(), h.stores)
	require.NoError(t, err)

	assert.Equal(t, direct.Entries, remote.Entries, "the same tree is the same number of nodes")
	assert.Equal(t, direct.Blocks, remote.Blocks)
	assert.Equal(t, direct.Stored, remote.Stored)
	assert.Zero(t, remote.Errors, "nothing in this tree should fail to read over the wire: %+v", remote)
	// Result.Bytes is NOT compared: it sums every entry's size, and a
	// directory's size is a local filesystem's own number that SFTP does not
	// carry. The files' bytes are compared node by node below, which is the
	// half that means anything.

	// Node for node, through the adapter — the reader both paths end at.
	dv, err := ladingadapter.Open(h.stores, mountDirect, direct.Snap)
	require.NoError(t, err)
	rv, err := ladingadapter.Open(h.stores, mountRemote, remote.Snap)
	require.NoError(t, err)

	var walked int
	require.NoError(t, fs.WalkDir(dv, ".", func(p string, d fs.DirEntry, werr error) error {
		require.NoError(t, werr)
		walked++
		di, err := dv.Stat(p)
		require.NoErrorf(t, err, "direct stat %s", p)
		ri, err := rv.Stat(p)
		require.NoErrorf(t, err, "remote stat %s — the two walks must see the same tree", p)

		assert.Equalf(t, di.IsDir(), ri.IsDir(), "kind of %s", p)
		// SFTP's attribute width is whole seconds, so this is the one field
		// that cannot be equal — it can only be equal to the second.
		assert.WithinDurationf(t, di.ModTime(), ri.ModTime(), time.Second, "mtime of %s", p)

		if d.IsDir() {
			// A directory's size is the local filesystem's own bookkeeping;
			// nothing carries it and nothing should read it.
			return nil
		}
		assert.Equalf(t, di.Size(), ri.Size(), "size of %s", p)
		// Modes do NOT survive: rclone's sftp server reports 0644 for every
		// regular file whatever the source's permissions are — the ingress
		// mirror of the egress finding that `--metadata` carries no mode.
		// Asserted rather than skipped, so a future rclone that starts
		// carrying them is a test failure someone reads rather than a silent
		// improvement nobody notices.
		assert.EqualValuesf(t, 0o644, ri.Mode().Perm(),
			"rclone normalises the mode of %s; the source's own is %v", p, di.Mode().Perm())

		want, rerr := fs.ReadFile(dv, p)
		require.NoError(t, rerr)
		got, rerr := fs.ReadFile(rv, p)
		require.NoErrorf(t, rerr, "remote read %s", p)
		assert.Equalf(t, want, got, "content of %s", p)
		return nil
	}))
	assert.Greater(t, walked, 5, "the fixture must have walked something")
}

// TestASnapshotOfARemoteIsAWellBehavedFS — the standard library's conformance
// test over a snapshot that came in through rclone.
func TestASnapshotOfARemoteIsAWellBehavedFS(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	dir := corpusDir(t)

	src := openRemote(t, dir)
	res, err := ladingingest.Snapshot(ctx, src, mountRemote, policy(), h.stores)
	require.NoError(t, err)

	view, err := ladingadapter.Open(h.stores, mountRemote, res.Snap)
	require.NoError(t, err)
	require.NoError(t, fstest.TestFS(view,
		"a", "a/b.txt", "a/c", "a/c/d.bin", "a/tiny", "top.md", "empty"))
}

// TestFiltersRunAtTheSource. A mount's content policy for a remote is rclone's
// filter language, and what it excludes never reaches this process at all —
// which is the difference between filtering and not storing.
func TestFiltersRunAtTheSource(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	dir := corpusDir(t)

	src := openRemote(t, dir, ladingremote.WithFilters("--exclude", "*.bin"))
	res, err := ladingingest.Snapshot(ctx, src, mountRemote, policy(), h.stores)
	require.NoError(t, err)

	view, err := ladingadapter.Open(h.stores, mountRemote, res.Snap)
	require.NoError(t, err)
	_, err = view.Stat("a/b.txt")
	assert.NoError(t, err, "an unfiltered file is there")
	_, err = view.Stat("a/c/d.bin")
	assert.ErrorIs(t, err, fs.ErrNotExist, "a filtered file never arrived")
}

// TestSubdirRootsTheWalk.
func TestSubdirRootsTheWalk(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	dir := corpusDir(t)

	src := openRemote(t, dir, ladingremote.WithSubdir("a"))
	res, err := ladingingest.Snapshot(ctx, src, mountRemote, policy(), h.stores)
	require.NoError(t, err)

	view, err := ladingadapter.Open(h.stores, mountRemote, res.Snap)
	require.NoError(t, err)
	_, err = view.Stat("b.txt")
	assert.NoError(t, err, "the subtree's own paths are the snapshot's paths")
	_, err = view.Stat("a/b.txt")
	assert.ErrorIs(t, err, fs.ErrNotExist, "and nothing above it is reachable")
	_, err = view.Stat("top.md")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

// TestRemoteSymlinksSurviveUnderLinks pins what §SD9 recorded as a limit of
// this path, and it turns out to be a smaller one than the prose assumed.
//
// Without `--links`, `rclone serve sftp` does not show a symlink at all — it
// is absent from the listing, so a snapshot simply does not have it. *With*
// `--links`, the node arrives over the wire as a symlink, target and all, and
// the walker records it as one: `node_kind = symlink`, `link_target` set,
// no content. So symlink fidelity through rclone ingress is a flag away rather
// than unavailable.
//
// (rclone's own `ls` renders such a node as a small regular file whose bytes
// are the target — that is its client-side `.rclonelink` convention showing
// through, and it is a different layer from what SFTP carries.)
func TestRemoteSymlinksSurviveUnderLinks(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	dir := corpusDir(t)
	require.NoError(t, os.Symlink("b.txt", filepath.Join(dir, "a", "alias.txt")))

	// Without --links the link is simply absent.
	plain := openRemote(t, dir)
	res, err := ladingingest.Snapshot(ctx, plain, mountRemote, policy(), h.stores)
	require.NoError(t, err)
	view, err := ladingadapter.Open(h.stores, mountRemote, res.Snap)
	require.NoError(t, err)
	_, err = view.Lstat("a/alias.txt")
	assert.ErrorIs(t, err, fs.ErrNotExist, "without --links rclone does not show a symlink at all")

	h.purge(t)

	// With it, the link is a link.
	linked := openRemote(t, dir, ladingremote.WithArgs("--links"))
	res, err = ladingingest.Snapshot(ctx, linked, mountRemote, policy(), h.stores)
	require.NoError(t, err)
	view, err = ladingadapter.Open(h.stores, mountRemote, res.Snap)
	require.NoError(t, err)

	info, err := view.Lstat("a/alias.txt")
	require.NoError(t, err, "with --links the link arrives")
	assert.NotZero(t, info.Mode()&fs.ModeSymlink, "and it arrives as a symlink")

	target, err := view.ReadLink("a/alias.txt")
	require.NoError(t, err)
	assert.Equal(t, "b.txt", target, "with the target the source had")

	// And the adapter resolves it inside the snapshot, like any other link.
	got, err := fs.ReadFile(view, "a/alias.txt")
	require.NoError(t, err)
	want, err := os.ReadFile(filepath.Join(dir, "a", "b.txt"))
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// TestCloseReapsTheProcess. A Remote that is dropped leaves an rclone running
// until its stdin closes; Close is what makes that deterministic.
func TestCloseReapsTheProcess(t *testing.T) {
	dir := corpusDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	src, err := ladingremote.Serve(ctx, dir)
	if err != nil {
		t.Skipf("rclone unavailable: %v", err)
	}
	entries, err := src.ReadDir(".")
	require.NoError(t, err)
	assert.NotEmpty(t, entries)

	require.NoError(t, src.Close(), "a clean session must close cleanly")
	assert.NoError(t, src.Close(), "and Close is idempotent")

	// The session is over, so the FS is too.
	_, err = src.ReadDir(".")
	assert.Error(t, err, "reading after Close must fail rather than hang")
}

// TestAMissingRemoteFailsAtServe, with rclone's own words rather than an EOF.
func TestAMissingRemoteFailsAtServe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	src, err := ladingremote.Serve(ctx, "nosuchremote:definitely/not/here")
	if src != nil {
		_ = src.Close()
	}
	require.Error(t, err, "a remote rclone cannot open must fail at Serve")

	// The property this test exists for: Serve folds rclone's bounded stderr
	// and the remote's name into the error, so a failure says why rather than
	// only that NewClientPipe saw the far end close.
	//
	// The check is against the structured payload, not against Error(): the
	// house style puts context in Str/Int fields and keeps the message bare,
	// so a string assertion would be asserting the wrong half. The previous
	// form checked Error() for the literal "EOF only", which nothing emits —
	// it would have stayed green with the stderr dropped entirely.
	esd, ok := err.(eh.ErrorWithStructuredDataI)
	require.Truef(t, ok, "Serve's failure must carry structured data: %v", err)
	fields := decodeFields(t, esd.GetCBORStructuredData())
	assert.Equal(t, "nosuchremote:definitely/not/here", fields["remote"],
		"the error must name the remote rclone could not open")
	stderr, _ := fields["stderr"].(string)
	assert.NotEmpty(t, stderr,
		"the error must carry what rclone said on stderr, not just an EOF")
	assert.Contains(t, strings.ToLower(stderr), "config",
		"rclone's complaint about an unknown remote mentions its config: %q", stderr)
}

// decodeFields reads an eb-built error's structured payload.
//
// An independent CBOR implementation on purpose, the same oracle eb's own
// tests use: a payload is right when a decoder that shares no code with the
// encoder accepts it.
func decodeFields(t *testing.T, data []byte) map[string]any {
	t.Helper()
	require.NotEmpty(t, data, "structured payload is empty")
	var v any
	require.NoError(t, cbor.Unmarshal(data, &v))
	raw, ok := v.(map[any]any)
	require.Truef(t, ok, "payload decoded to %T, want a map", v)
	out := make(map[string]any, len(raw))
	for k, val := range raw {
		if s, isStr := k.(string); isStr {
			out[s] = val
		}
	}
	return out
}
