//go:build integration

package ladingsftp_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/fs/lading"
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

// This file drives the real rclone binary against the real head, over a real
// pipe, against a real server — the ADR-0198 §SD9 lane.
//
// It is the only place the head meets a client it did not write. Everything
// else in this package tests it against `pkg/sftp`, which is the same library
// the head is built on; rclone is the one that decides whether the tree is a
// file system by somebody else's judgement.

// rcloneRig is a live store with one snapshot, plus a script rclone can run in
// place of ssh.
type rcloneRig struct {
	exec    recordstore.ExecutorI
	head    string // the ssh-replacement script
	remote  string // the rclone connection string
	snap    time.Time
	srcDir  string
	binPath string
}

func setupRclone(t *testing.T) *rcloneRig {
	t.Helper()
	rclone, err := extbin.Rclone.Command(context.Background(), extbin.Opts{})
	if err != nil {
		t.Skipf("rclone unavailable: %v", err)
	}
	cfg := chclient.ConfigFromEnv()
	client := chclient.New(cfg, nil)
	if err = client.Ping(context.Background()); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", cfg.URL, err)
	}
	ex, err := storeexec.New(client, nil)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, lading.Provision(ctx, ex, ladingschema.ProfileCorpus))

	st := lading.Stores{
		Meta: ladingmeta.NewMetaStore(ex, nil, ladingmeta.MetaStoreConfig{}),
		Data: ladingdata.NewDataStore(ex, nil, ladingdata.DataStoreConfig{}),
	}
	purge(t, ex)
	t.Cleanup(func() { st.Meta.Close(); st.Data.Close(); purge(t, ex) })

	// A real directory on disk, so `rclone copy` has something to compare the
	// round trip against.
	srcDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "a", "c"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "empty"), 0o755))
	var text bytes.Buffer
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&text, "line %d\n", i)
	}
	bin := make([]byte, 5000)
	for i := range bin {
		bin[i] = byte(i * 7)
	}
	write := func(rel string, data []byte, mode os.FileMode, mtime time.Time) {
		p := filepath.Join(srcDir, rel)
		require.NoError(t, os.WriteFile(p, data, mode))
		require.NoError(t, os.Chmod(p, mode))
		require.NoError(t, os.Chtimes(p, mtime, mtime))
	}
	mt := time.Unix(1_700_000_000, 0)
	write("a/b.txt", text.Bytes(), 0o644, mt)
	write("a/c/d.bin", bin, 0o600, mt.Add(time.Minute))
	write("top.md", []byte("# hi\n"), 0o644, mt.Add(2*time.Minute))

	pol := ladingingest.DefaultPolicy()
	pol.Ttl = ladingingest.TtlClass7d
	pol.Profile.BlockSize = 1024 // several blocks in d.bin
	pol.Profile.PerBlockHash = true
	res, err := ladingingest.Snapshot(ctx, os.DirFS(srcDir), testMount, pol, st)
	require.NoError(t, err)

	// The script rclone runs in place of ssh. It has to tolerate BOTH the
	// arguments rclone passes: `-s sftp` for the session, and the shell probe
	// `echo ${ShellId}%ComSpec%` — unless shell_type is pinned, which the
	// remote below does. Recorded at M0 (check 11a).
	head := filepath.Join(t.TempDir(), "head.sh")
	repo := repoRoot(t)
	script := fmt.Sprintf(`#!/bin/sh
exec go run -tags=%q %s/public/app fs sftp-stdio --mount %d
`, buildTags(t), repo, testMount.Value())
	require.NoError(t, os.WriteFile(head, []byte(script), 0o755))

	return &rcloneRig{
		exec: ex, head: head, snap: res.Snap, srcDir: srcDir, binPath: rclone.Path,
		remote: fmt.Sprintf(":sftp,ssh=%s,shell_type=unix:", head),
	}
}

func purge(t *testing.T, ex recordstore.ExecutorI) {
	t.Helper()
	ctx := context.Background()
	key, err := ladingschema.PhysicalPlainName("id")
	require.NoError(t, err)
	for _, tbl := range []string{
		ladingschema.TableNameMeta, ladingschema.TableNameData, ladingschema.TableNameSnap,
	} {
		require.NoError(t, ex.Exec(ctx, fmt.Sprintf("DELETE FROM %s.%s WHERE %s = %d",
			ladingschema.DatabaseName, tbl, key, testMount.Value())))
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	// .../public/fs/lading/ladingsftp
	return filepath.Clean(filepath.Join(wd, "..", "..", "..", ".."))
}

func buildTags(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "tags"))
	require.NoError(t, err)
	return strings.TrimSpace(string(raw))
}

// run invokes rclone and returns its stdout, failing the test on a non-zero
// exit with everything both streams said.
func (inst *rcloneRig) run(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	full := append([]string{"--transfers", "1", "--low-level-retries", "1"}, args...)
	cmd := exec.CommandContext(ctx, inst.binPath, full...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	require.NoErrorf(t, err, "rclone %s\nstdout:\n%s\nstderr:\n%s",
		strings.Join(full, " "), out.String(), errb.String())
	return out.String()
}

func lines(s string) (out []string) {
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	sort.Strings(out)
	return
}

func (inst *rcloneRig) mountDir() string { return fmt.Sprintf("%016x", testMount.Value()) }

func (inst *rcloneRig) snapDir() string {
	return inst.snap.UTC().Format("20060102T150405.000000000Z")
}

// TestRcloneWalksTheTree — lsd at each level, judged by rclone rather than by
// the head's own client.
func TestRcloneWalksTheTree(t *testing.T) {
	r := setupRclone(t)

	mounts := lines(r.run(t, "lsd", r.remote))
	require.Len(t, mounts, 1)
	assert.Contains(t, mounts[0], r.mountDir(), "the root lists the mount")

	snaps := lines(r.run(t, "lsd", r.remote+"/"+r.mountDir()))
	// Two: the snapshot, and `latest` — which rclone's sftp backend follows
	// rather than reporting as a link, so it presents as a second directory
	// with the same contents. That is the better outcome for the mount case
	// (`rclone mount …/latest` just works) and it is rclone's reading, not
	// something the head chose: an SFTP client that Lstats still sees a link.
	require.Len(t, snaps, 2)
	joined := strings.Join(snaps, "\n")
	assert.Contains(t, joined, r.snapDir())
	assert.Contains(t, joined, "latest")

	tree := lines(r.run(t, "lsd", r.remote+"/"+r.mountDir()+"/"+r.snapDir()))
	treeJoined := strings.Join(tree, "\n")
	assert.Contains(t, treeJoined, "a")
	assert.Contains(t, treeJoined, "empty")
}

// TestRcloneListsAndReadsASnapshot — `ls` agrees with the source tree, and
// `cat` returns the bytes.
func TestRcloneListsAndReadsASnapshot(t *testing.T) {
	r := setupRclone(t)
	base := r.remote + "/" + r.mountDir() + "/" + r.snapDir()

	listed := map[string]bool{}
	for _, l := range lines(r.run(t, "ls", base)) {
		fields := strings.Fields(l)
		require.Len(t, fields, 2, "rclone ls prints size and path: %q", l)
		listed[fields[1]] = true
	}
	assert.True(t, listed["a/b.txt"])
	assert.True(t, listed["a/c/d.bin"])
	assert.True(t, listed["top.md"])

	for _, name := range []string{"a/b.txt", "a/c/d.bin", "top.md"} {
		want, err := os.ReadFile(filepath.Join(r.srcDir, name))
		require.NoError(t, err)
		got := r.run(t, "cat", base+"/"+name)
		assert.Equalf(t, string(want), got, "rclone cat %s", name)
	}
}

// TestRcloneCopyRestoresTimesButNotModes.
//
// ADR-0198 §SD9 expected `--metadata` to carry both. Half of it holds and the
// other half is rclone's limit rather than the head's: rclone's `sftp` backend
// documents **no system metadata at all** — modification times ride
// `--sftp-set-modtime` (on by default), and there is no `mode` key for
// `--metadata` to carry. The head reports the mode correctly over the wire, as
// the pkg/sftp tests show; nothing on the rclone path asks for it.
//
// So a copy out of the store restores content and mtime, and the destination
// takes the local backend's default permissions. Worth knowing before someone
// treats an rclone copy as a faithful restore.
func TestRcloneCopyRestoresTimesButNotModes(t *testing.T) {
	r := setupRclone(t)
	base := r.remote + "/" + r.mountDir() + "/" + r.snapDir()
	dst := t.TempDir()

	r.run(t, "copy", "--metadata", base, dst)

	for _, name := range []string{"a/b.txt", "a/c/d.bin", "top.md"} {
		src, err := os.Stat(filepath.Join(r.srcDir, name))
		require.NoError(t, err)
		out, err := os.Stat(filepath.Join(dst, name))
		require.NoErrorf(t, err, "%s must have been copied", name)

		gotBytes, err := os.ReadFile(filepath.Join(dst, name))
		require.NoError(t, err)
		wantBytes, err := os.ReadFile(filepath.Join(r.srcDir, name))
		require.NoError(t, err)
		assert.Equalf(t, wantBytes, gotBytes, "content of %s", name)

		// SFTP carries whole seconds, so the comparison is to the second —
		// which is the source's resolution as it arrives, not a rounding this
		// store applied.
		assert.WithinDurationf(t, src.ModTime(), out.ModTime(), time.Second,
			"mtime of %s", name)
		_ = src.Mode() // modes do not survive; see the doc comment above.
	}
}

// TestRcloneFollowsLatestToTheNewestSnapshot.
//
// `latest` is the only mutable name in the tree. M0 check 11b pinned the
// `.rclonelink` mechanism, but that is the *local* backend's — reading a
// remote's symlinks over `sftp`, rclone follows them instead. So `latest`
// presents as a directory holding the newest snapshot, which is what makes
// `rclone mount …/latest` work without a flag, and an SFTP client that Lstats
// still sees the link (the pkg/sftp tests cover that half).
func TestRcloneFollowsLatestToTheNewestSnapshot(t *testing.T) {
	r := setupRclone(t)

	viaLatest := lines(r.run(t, "lsf", "-R", r.remote+"/"+r.mountDir()+"/latest"))
	viaName := lines(r.run(t, "lsf", "-R", r.remote+"/"+r.mountDir()+"/"+r.snapDir()))
	require.NotEmpty(t, viaName)
	assert.Equal(t, viaName, viaLatest, "latest must show exactly the newest snapshot")

	want, err := os.ReadFile(filepath.Join(r.srcDir, "top.md"))
	require.NoError(t, err)
	assert.Equal(t, string(want), r.run(t, "cat", r.remote+"/"+r.mountDir()+"/latest/top.md"))
}

// TestRcloneRefusesToWrite. rclone is a sync tool; the store is append-only
// and has no update path. The refusal has to reach rclone as an error rather
// than as a silent success.
func TestRcloneRefusesToWrite(t *testing.T) {
	r := setupRclone(t)
	base := r.remote + "/" + r.mountDir() + "/" + r.snapDir()
	src := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "intruder.txt"), []byte("nope\n"), 0o644))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.binPath, "--transfers", "1", "--low-level-retries", "1",
		"copy", src, base)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	require.Errorf(t, err, "rclone copy INTO the store must fail\nstdout:\n%s\nstderr:\n%s",
		out.String(), errb.String())

	// And nothing landed.
	listed := r.run(t, "lsf", base)
	assert.NotContains(t, listed, "intruder.txt")
}
