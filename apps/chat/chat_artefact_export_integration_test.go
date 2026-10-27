//go:build integration

package chat

import (
	"context"
	"fmt"
	"io/fs"
	"testing"
	"time"

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

func liveLading(t *testing.T) recordstore.ExecutorI {
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

// An export writes the text and the screenshot as one snapshot readers open,
// journals where it went, and a second export writes another snapshot of the
// same mount.
func TestAnExportLandsInTheStoreAndIsJournaled(t *testing.T) {
	exec := liveLading(t)
	inst := exportRig(t, func(context.Context) (recordstore.ExecutorI, error) { return exec, nil })
	name := withScreenshot(t, inst.conv.art)
	inst.planExport(false)
	p := inst.artView.export
	_, err := inst.exportNow(context.Background(), p, "test artefact")
	require.NoError(t, err)
	rec, ok := lastExport(inst.coord.journalNow().Entries())
	require.True(t, ok)
	mount := identifier.TaggedId(rec.Mount)
	t.Cleanup(func() {
		key, kerr := ladingschema.PhysicalPlainName("id")
		require.NoError(t, kerr)
		for _, tbl := range []string{ladingschema.TableNameMeta, ladingschema.TableNameData, ladingschema.TableNameSnap} {
			require.NoError(t, exec.Exec(context.Background(),
				fmt.Sprintf("DELETE FROM %s.%s WHERE %s = %d", ladingschema.DatabaseName, tbl, key, mount.Value())))
		}
	})
	assert.True(t, exportMountTag.GetTag().SameTag(mount))
	assert.Equal(t, p.rev, rec.Revision)
	require.Len(t, rec.Images, 1)

	stores := lading.NewStores(exec, ladingschema.Layout{})
	defer stores.Meta.Close()
	defer stores.Data.Close()
	v, err := ladingadapter.Open(stores, mount, time.Unix(0, rec.SnapNanos))
	require.NoError(t, err)
	text, err := fs.ReadFile(v, exportDocPath)
	require.NoError(t, err)
	assert.Equal(t, p.text, string(text))
	png, err := fs.ReadFile(v, name)
	require.NoError(t, err)
	want, err := inst.conv.art.store.read(p.images[0].hash)
	require.NoError(t, err)
	assert.Equal(t, want, png)

	inst.planExport(false)
	_, err = inst.exportNow(context.Background(), inst.artView.export, "test artefact")
	require.NoError(t, err)
	again, ok := lastExport(inst.coord.journalNow().Entries())
	require.True(t, ok)
	assert.Equal(t, rec.Mount, again.Mount, "one mount per conversation")
	assert.Greater(t, again.SnapNanos, rec.SnapNanos)
}

// A conversation resumed in another window gets its exported revision back,
// screenshot and all.
func TestAResumedConversationGetsItsExportBack(t *testing.T) {
	exec := liveLading(t)
	t.Setenv("BOXER_ADHOC_DIR", t.TempDir())
	first := exportRig(t, func(context.Context) (recordstore.ExecutorI, error) { return exec, nil })
	name := withScreenshot(t, first.conv.art)
	first.planExport(false)
	p := first.artView.export
	_, err := first.exportNow(context.Background(), p, "test artefact")
	require.NoError(t, err)
	rec, ok := lastExport(first.coord.journalNow().Entries())
	require.True(t, ok)
	t.Cleanup(func() {
		key, kerr := ladingschema.PhysicalPlainName("id")
		require.NoError(t, kerr)
		for _, tbl := range []string{ladingschema.TableNameMeta, ladingschema.TableNameData, ladingschema.TableNameSnap} {
			require.NoError(t, exec.Exec(context.Background(),
				fmt.Sprintf("DELETE FROM %s.%s WHERE %s = %d", ladingschema.DatabaseName, tbl, key, rec.Mount)))
		}
	})
	settleTitle(t, first)
	id := first.conv.id

	second := appOver(t, &fakeModel{}, first.coord.store)
	second.resumeRun(id)
	require.Eventually(t, func() bool { second.drain(); return second.conv.id == id }, 10*time.Second, 5*time.Millisecond)
	n, text := second.conv.art.head()
	assert.Equal(t, p.rev, n)
	assert.Equal(t, p.text, text)
	set := second.conv.art.headImages()
	require.Len(t, set, 1)
	assert.Equal(t, name, set[0].name)
	assert.Contains(t, second.conv.entries[len(second.conv.entries)-1].text, "restored from its export")
}
