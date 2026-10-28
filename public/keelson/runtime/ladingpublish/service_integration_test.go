//go:build integration

package ladingpublish

import (
	"context"
	"fmt"
	"io/fs"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingadapter"
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
	if err = lading.Verify(context.Background(), exec); err != nil {
		t.Skipf("no lading store on the server: %v", err)
	}
	return exec
}

// A publish writes one snapshot a reader opens with the adapter it already
// uses, and a second publish of the same mount writes a second snapshot.
func TestPublishWritesASnapshotReadersOpen(t *testing.T) {
	exec := liveExec(t)
	s := newService(Config{Limits: Limits{Ceiling: 2, MaxTtlDays: 90, MaxBytes: 8 << 20},
		Connect: func(context.Context) (recordstore.ExecutorI, error) { return exec, nil }}, zerolog.New(zerolog.NewTestWriter(t)), nil)
	s.owners = ownersOf(manifests(), s.log)
	// Above ladingingest's default inline bound, which a published file must
	// not be held to: it would be stored as a reference to nothing.
	big := make([]byte, 5<<20)
	for i := range big {
		big[i] = byte(i * 7)
	}
	files := []File{{Path: "artefact.md", Data: []byte("![[shot.png]]\n")}, {Path: "shot.png", Data: big}}
	res := s.Publish(context.Background(), appA, 0, tagA, Request{Name: "test tree", TtlDays: 7, Files: files})
	require.False(t, res.Refused || res.Failed, res.Reason)
	mount := identifier.TaggedId(res.Mount)
	t.Cleanup(func() {
		key, err := ladingschema.PhysicalPlainName("id")
		require.NoError(t, err)
		for _, tbl := range []string{ladingschema.TableNameMeta, ladingschema.TableNameData, ladingschema.TableNameSnap} {
			require.NoError(t, exec.Exec(context.Background(),
				fmt.Sprintf("DELETE FROM %s.%s WHERE %s = %d", ladingschema.DatabaseName, tbl, key, mount.Value())))
		}
	})
	assert.Equal(t, uint64(2), res.Files)

	stores := lading.NewStores(exec, ladingschema.Layout{})
	defer stores.Meta.Close()
	defer stores.Data.Close()
	v, err := ladingadapter.Open(stores, mount, res.Snap())
	require.NoError(t, err)
	got, err := fs.ReadFile(v, "artefact.md")
	require.NoError(t, err)
	assert.Equal(t, files[0].Data, got)
	got, err = fs.ReadFile(v, "shot.png")
	require.NoError(t, err, "a file above the default inline bound is stored whole")
	assert.Equal(t, big, got)

	again := s.Publish(context.Background(), appA, 0, tagA, Request{Mount: res.Mount, TtlDays: 7, Files: files[:1]})
	require.False(t, again.Refused || again.Failed, again.Reason)
	assert.Equal(t, res.Mount, again.Mount)
	assert.True(t, again.Snap().After(res.Snap()))
}
