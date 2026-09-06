//go:build integration

package tally

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingadhoc"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/identity/identifier"
)

// The path-set half of ADR-0222 against a real store: publish a tree, run a
// query over it the way a launch config would, and read a file back through
// the tree the Results pane browses. Everything but the render call.
func TestPathSetOverAPublishedTree_LiveServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	sc, err := connect(ctx)
	if err != nil {
		t.Skipf("no store: %v", err)
	}
	defer sc.close()
	require.NoError(t, lading.Provision(ctx, sc.exec, ladingschema.ProfileCorpus))

	pub, err := ladingadhoc.Publish(ctx, sc.exec, ladingadhoc.PublishInput{
		FS: fstest.MapFS{
			"README.md":      &fstest.MapFile{Data: []byte("# published\n"), Mode: 0o644},
			"notes/a.md":     &fstest.MapFile{Data: []byte("note a\n"), Mode: 0o644},
			"src/main.go":    &fstest.MapFile{Data: []byte("package main\n"), Mode: 0o644},
			"src/deep/b.txt": &fstest.MapFile{Data: []byte("bbb"), Mode: 0o644},
		},
		Name:      "tally composition test tree",
		Publisher: "tally integration test",
	})
	require.NoError(t, err)
	t.Cleanup(func() { purgeMount(t, sc, pub.Mount) })

	// `ext` carries its dot, as the store records it.
	sql := fmt.Sprintf(
		"SELECT path, mount, snap, is_dir FROM fs(0x%X, %d) WHERE ext = '.md' ORDER BY path",
		pub.Mount.Value(), pub.Snap.UnixNano())
	rs, err := runPathSet(ctx, sc.exec, sql, location{}, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, rs.rows, "two markdown files in the published tree")
	assert.Zero(t, rs.dropped, "the query selected mount and snap, so nothing is unplaced")
	require.Len(t, rs.locs, 1, "one snapshot, so the tree carries no location prefix")

	// The rows became a browsable tree: the directory above a leaf was
	// synthesised, and opening a leaf reads the snapshot behind it.
	tree := newResultFS(func(loc location) (fs.FS, error) {
		return sc.view(loc.mount, loc.snap)
	}, rs.leaves, rs.dirs)
	root, err := tree.ReadDir(".")
	require.NoError(t, err)
	require.Len(t, root, 2)
	assert.Equal(t, "README.md", root[0].Name())
	assert.Equal(t, "notes", root[1].Name())
	assert.True(t, root[1].IsDir())

	f, err := tree.Open("notes/a.md")
	require.NoError(t, err)
	body, err := io.ReadAll(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	assert.Equal(t, "note a\n", string(body))

	// A leaf resolves to the snapshot Info and History would query.
	leaf, ok := tree.Leaf("notes/a.md")
	require.True(t, ok)
	assert.Equal(t, pub.Mount, leaf.loc.mount)
	assert.Equal(t, "notes/a.md", leaf.real)

	// A query that returns directories says so through `is_dir` — which the
	// surface projects as a UInt8, not a boolean — and they become
	// directories in the tree rather than leaves that pretend to be files.
	whole, err := runPathSet(ctx, sc.exec, fmt.Sprintf(
		"SELECT path, mount, snap, is_dir FROM fs(0x%X, %d) WHERE path != '.' ORDER BY path",
		pub.Mount.Value(), pub.Snap.UnixNano()), location{}, nil)
	require.NoError(t, err)
	assert.Equal(t, 4, whole.rows, "four files")
	assert.Equal(t, 3, whole.dirRows, "notes, src and src/deep")
	assert.Contains(t, whole.dirs, "notes")
	assert.Contains(t, whole.dirs, "src")
	assert.NotContains(t, whole.leaves, "src")
	assert.Contains(t, whole.dirs, "src/deep")
	assert.Contains(t, whole.summary("tree"), "3 directories")

	// And the same read refuses when it is not provably one.
	_, err = runPathSet(ctx, sc.exec, "ALTER TABLE "+ladingschema.DatabaseName+"."+ladingschema.TableNameMeta+" DELETE WHERE 1", location{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not provably read-only")
}

func purgeMount(t *testing.T, sc *storeConn, mount identifier.TaggedId) {
	t.Helper()
	key, err := ladingschema.PhysicalPlainName("id")
	require.NoError(t, err)
	for _, tbl := range []string{
		ladingschema.TableNameMeta, ladingschema.TableNameData, ladingschema.TableNameSnap,
	} {
		require.NoError(t, sc.exec.Exec(context.Background(),
			fmt.Sprintf("DELETE FROM %s.%s WHERE %s = %d",
				ladingschema.DatabaseName, tbl, key, mount.Value())))
	}
}
