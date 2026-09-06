//go:build integration

package ladingadhoc_test

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingadapter"
	"github.com/stergiotis/boxer/public/fs/lading/ladingadhoc"
	"github.com/stergiotis/boxer/public/fs/lading/ladingdata"
	"github.com/stergiotis/boxer/public/fs/lading/ladingmeta"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

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

// purge removes one mount's rows — the per-mount delete ADR-0198 §SD1
// promises. A published tree would expire on its own; a test that leaves it
// there for a day is a test that pollutes a shared server.
func purge(t *testing.T, exec recordstore.ExecutorI, mount identifier.TaggedId) {
	t.Helper()
	key, err := ladingschema.PhysicalPlainName("id")
	require.NoError(t, err)
	for _, tbl := range []string{
		ladingschema.TableNameMeta, ladingschema.TableNameData, ladingschema.TableNameSnap,
	} {
		require.NoError(t, exec.Exec(context.Background(),
			fmt.Sprintf("DELETE FROM %s.%s WHERE %s = %d",
				ladingschema.DatabaseName, tbl, key, mount.Value())))
	}
}

// The gesture ADR-0222 §SD5 describes, end to end: publish a tree, browse it
// back through the adapter the browser reads with, then republish into the
// same mount and see a second snapshot rather than a second mount.
func TestPublishThenBrowseThenRepublish(t *testing.T) {
	exec := liveExec(t)
	require.NoError(t, lading.Provision(context.Background(), exec, ladingschema.ProfileCorpus))

	first, err := ladingadhoc.Publish(context.Background(), exec, ladingadhoc.PublishInput{
		FS: fstest.MapFS{
			"README.md":      &fstest.MapFile{Data: []byte("# one\n"), Mode: 0o644},
			"src/main.go":    &fstest.MapFile{Data: []byte("package main\n"), Mode: 0o644},
			"src/deep/a.txt": &fstest.MapFile{Data: []byte("aaa"), Mode: 0o644},
		},
		Name:      "integration tree",
		Publisher: "ladingadhoc integration test",
	})
	require.NoError(t, err)
	t.Cleanup(func() { purge(t, exec, first.Mount) })

	assert.True(t, ladingadhoc.IsAdhoc(first.Mount))
	assert.Zero(t, first.Errors)
	assert.True(t, first.ExpiresAt.After(first.Snap))

	stores := lading.Stores{
		Meta: ladingmeta.NewMetaStore(exec, nil, ladingmeta.MetaStoreConfig{}),
		Data: ladingdata.NewDataStore(exec, nil, ladingdata.DataStoreConfig{}),
	}
	defer stores.Meta.Close()
	defer stores.Data.Close()

	view, err := ladingadapter.Open(stores, first.Mount, first.Snap)
	require.NoError(t, err)
	data, err := fs.ReadFile(view, "src/main.go")
	require.NoError(t, err)
	assert.Equal(t, "package main\n", string(data))

	f, err := view.Open("README.md")
	require.NoError(t, err)
	body, err := io.ReadAll(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	assert.Equal(t, "# one\n", string(body))

	// Republish: same mount, a new instant, and the mount list still holds
	// one mount — the handle-reuse rule.
	second, err := ladingadhoc.Publish(context.Background(), exec, ladingadhoc.PublishInput{
		FS:        fstest.MapFS{"README.md": &fstest.MapFile{Data: []byte("# two\n"), Mode: 0o644}},
		Name:      "integration tree",
		Publisher: "ladingadhoc integration test",
		Mount:     first.Mount,
	})
	require.NoError(t, err)
	assert.Equal(t, first.Mount, second.Mount)
	assert.True(t, second.Snap.After(first.Snap))

	snaps, err := ladingadapter.Snapshots(context.Background(), exec, first.Mount)
	require.NoError(t, err)
	require.Len(t, snaps, 2, "a republish is a second snapshot of one mount")

	// The first snapshot is untouched: a lading snapshot is written once.
	data, err = fs.ReadFile(view, "README.md")
	require.NoError(t, err)
	assert.Equal(t, "# one\n", string(data))
}
