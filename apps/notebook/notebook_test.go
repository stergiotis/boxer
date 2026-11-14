package notebook

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/notebook/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/ladingpublish"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook/samples"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

func TestTheCatalogRegisters(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(AppId)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(AppId))
	for _, name := range []string{opGetDocument, opCheckNotebook, opSetRecords, opShowFace, opOpenSample, opOpenSnapshot, opCollectNotebook} {
		spec, found := m.Operations.Lookup(name)
		require.True(t, found, name)
		assert.True(t, spec.Agents, name)
	}
}

func query[T any](t *testing.T, h app.OperationsHandlerI, op string) (out T, err error) {
	t.Helper()
	res, err := h.Snapshot().Query(op, nil)
	if err != nil {
		return out, err
	}
	out, dErr := buscodec.Decode[T](res)
	require.NoError(t, dErr)
	return out, nil
}

func command(t *testing.T, h app.OperationsHandlerI, op string, args any) error {
	t.Helper()
	raw, err := buscodec.Encode(args)
	require.NoError(t, err)
	_, err = h.ApplyCommand(app.OperationCall{Writer: "task:t"}, op, raw)
	return err
}

// read polls the window's lanes until the text on hand was read.
func read(t *testing.T, inst *App) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !inst.checked {
		require.True(t, time.Now().Before(deadline), "the notebook was not read in time")
		inst.poll()
		time.Sleep(2 * time.Millisecond)
	}
}

// A sample is opened, read as sealed, and its records worked on: a buffer
// written back changes the record cells alone, and the check then names
// what collection owes.
func TestASampleIsReadCheckedAndItsRecordsSet(t *testing.T) {
	t.Cleanup(scenetest.Install())
	inst := newApp()
	h := inst.Operations()

	_, err := query[Document](t, h, opGetDocument)
	require.ErrorContains(t, err, "no notebook is open")
	require.ErrorContains(t, command(t, h, opOpenSample, OpenSampleArgs{Name: "nope"}), "no sample of that name")
	require.NoError(t, command(t, h, opOpenSample, OpenSampleArgs{Name: "minimal"}))

	doc, err := query[Document](t, h, opGetDocument)
	require.NoError(t, err)
	assert.Equal(t, "minimal.md", doc.Name)
	assert.Equal(t, "sample minimal.md", doc.Origin)
	assert.Equal(t, []string{"readings", "totals"}, doc.Records)
	assert.False(t, doc.Edited)
	assert.Equal(t, faceNameRead, doc.Face)

	_, err = query[CheckReport](t, h, opCheckNotebook)
	require.ErrorContains(t, err, "still being read")
	read(t, inst)
	ck, err := query[CheckReport](t, h, opCheckNotebook)
	require.NoError(t, err)
	assert.True(t, ck.Sealed, "%+v", ck)

	// The notebook's own buffer changes nothing.
	nb, err := mdnotebook.Read([]byte(doc.Text))
	require.NoError(t, err)
	sql, err := mdnotebook.Buffer(nb)
	require.NoError(t, err)
	require.NoError(t, command(t, h, opSetRecords, SetRecordsArgs{Sql: sql}))
	same, err := query[Document](t, h, opGetDocument)
	require.NoError(t, err)
	assert.Equal(t, doc.Revision, same.Revision)
	assert.False(t, same.Edited)

	// One statement edited.
	require.NoError(t, command(t, h, opSetRecords, SetRecordsArgs{Sql: strings.Replace(sql, "numbers(24)", "numbers(12)", 1)}))
	edited, err := query[Document](t, h, opGetDocument)
	require.NoError(t, err)
	assert.NotEqual(t, doc.Revision, edited.Revision)
	assert.True(t, edited.Edited)
	assert.Contains(t, edited.Text, "numbers(12)")
	read(t, inst)
	ck, err = query[CheckReport](t, h, opCheckNotebook)
	require.NoError(t, err)
	assert.False(t, ck.Sealed)
	assert.Equal(t, []string{"readings"}, ck.Stale)
	assert.Equal(t, []string{"totals reads readings"}, ck.Derived)

	// A buffer no notebook holds, and one that drops a record.
	require.ErrorContains(t, command(t, h, opSetRecords, SetRecordsArgs{Sql: "SELECT {x:Int64} AS a"}), "parameter")
	require.ErrorContains(t, command(t, h, opSetRecords, SetRecordsArgs{Sql: "SELECT 1 AS hours, 1.0 AS mean_ms"}), "does not hold the record readings")

	require.ErrorContains(t, command(t, h, opShowFace, ShowFaceArgs{Face: "both"}), "read or records")
	require.NoError(t, command(t, h, opShowFace, ShowFaceArgs{Face: faceNameRecords}))
	assert.Equal(t, faceRecords, inst.face)
	require.NoError(t, inst.Unmount(nil))
}

func TestSnapshotRef(t *testing.T) {
	ref, err := parseSnapshotRef("1f@1731500000000000000:reports/w45.md")
	require.NoError(t, err)
	assert.EqualValues(t, 0x1f, ref.mount.Value())
	assert.EqualValues(t, 1731500000000000000, ref.snap.UnixNano())
	assert.Equal(t, "reports/w45.md", ref.path)
	assert.Equal(t, "1f@1731500000000000000:reports/w45.md", ref.String())
	for _, bad := range []string{"", "1f", "1f@12", "zz@12:a.md", "1f@x:a.md", "1f@12:"} {
		_, err = parseSnapshotRef(bad)
		assert.Error(t, err, bad)
	}
}

// A collection runs each record once, publishes the notebook's text with
// the files it names under the tier the caller states, and the window
// turns to the snapshot that was written. The server and the store's write
// path are stood in for: what is checked is what the window asks of them.
func TestCollectPublishesTheNotebookUnderTheStatedTier(t *testing.T) {
	t.Cleanup(scenetest.Install())
	inst := newApp()
	h := inst.Operations()
	require.ErrorContains(t, command(t, h, opCollectNotebook, CollectArgs{Tier: "AMBER"}), "no notebook is open")
	require.NoError(t, command(t, h, opOpenSample, OpenSampleArgs{Name: "minimal"}))
	require.ErrorContains(t, command(t, h, opCollectNotebook, CollectArgs{Tier: "AMBER"}), "no bus")

	// The server answers the i-th statement with what the sample stores for
	// its i-th record.
	var stored [][]byte
	for _, name := range inst.records() {
		ref, found := inst.nb.Record(name)
		require.True(t, found)
		blob, err := fs.ReadFile(samples.FS(), ref.Blob)
		require.NoError(t, err)
		stored = append(stored, blob)
	}
	var asked []string
	inst.run = func(_ context.Context, statement string) ([]byte, error) {
		asked = append(asked, statement)
		return stored[len(asked)-1], nil
	}
	var published []ladingpublish.Request
	inst.publish = func(req ladingpublish.Request) (ladingpublish.Result, error) {
		published = append(published, req)
		return ladingpublish.Result{Mount: 0x2a, SnapNanos: 1731500000000000000, ExpiresNanos: 1734092000000000000, Files: uint64(len(req.Files))}, nil
	}
	require.ErrorContains(t, command(t, h, opCollectNotebook, CollectArgs{Tier: "secret"}), "a tier is")

	// An edited record, so the collection has something to cure.
	sql, err := mdnotebook.Buffer(inst.nb)
	require.NoError(t, err)
	require.NoError(t, command(t, h, opSetRecords, SetRecordsArgs{Sql: strings.Replace(sql, "numbers(24)", "numbers( 24 )", 1)}))
	read(t, inst)
	require.Equal(t, []string{"readings"}, inst.check.Stale)

	require.NoError(t, command(t, h, opCollectNotebook, CollectArgs{Tier: "green"}))
	deadline := time.Now().Add(10 * time.Second)
	for inst.collect.done < inst.collect.seq {
		require.True(t, time.Now().Before(deadline), "the collection did not end in time")
		inst.landCollect()
		time.Sleep(2 * time.Millisecond)
	}
	require.Empty(t, inst.collect.lastErr)
	require.Len(t, asked, 2, "each record is computed once")
	assert.True(t, strings.HasSuffix(asked[0], "FORMAT ArrowStream"), asked[0])
	require.Len(t, published, 1)
	req := published[0]
	assert.EqualValues(t, agentrun.TierGreen, req.Tier)
	assert.False(t, req.Confined)
	assert.Zero(t, req.Mount, "the first collection mints a mount")
	assert.Equal(t, "minimal", req.Name)

	// What was published is a sealed notebook: its text at the root, its
	// blobs beside it.
	tree := fstest.MapFS{}
	for _, f := range req.Files {
		tree[f.Path] = &fstest.MapFile{Data: f.Data}
	}
	require.Contains(t, tree, "minimal.md")
	require.Len(t, tree, 3)
	nb, err := mdnotebook.Read(tree["minimal.md"].Data)
	require.NoError(t, err)
	ld := mdnotebook.Load(nb, tree)
	defer ld.Release()
	assert.Empty(t, mdnotebook.Problems(nb, ld))
	assert.Empty(t, ld.Stale)
	assert.Contains(t, string(tree["minimal.md"].Data), "numbers( 24 )")

	// The window turns to the snapshot, and writes its mount the next time.
	require.NotNil(t, inst.opening)
	assert.Equal(t, "2a@1731500000000000000:minimal.md", inst.opening.String())
	assert.Contains(t, inst.note, "collected 2 records")
	assert.EqualValues(t, 0x2a, inst.collect.mount)
	// The store is not at hand in a test: the open is not followed.
	inst.opening = nil

	// A publish the store refuses is said so, and nothing is opened.
	inst.publish = func(ladingpublish.Request) (ladingpublish.Result, error) {
		return ladingpublish.Result{}, &ladingpublish.RefusedError{Result: ladingpublish.Result{Refused: true, Reason: "the tier is above the store's ceiling"}}
	}
	asked = nil
	require.NoError(t, command(t, h, opCollectNotebook, CollectArgs{Tier: "RED"}))
	for inst.collect.done < inst.collect.seq {
		require.True(t, time.Now().Before(deadline), "the collection did not end in time")
		inst.landCollect()
		time.Sleep(2 * time.Millisecond)
	}
	assert.Contains(t, inst.collect.lastErr, "above the store's ceiling")
	assert.Contains(t, inst.note, "not collected")
	assert.Nil(t, inst.opening)
	require.NoError(t, inst.Unmount(nil))
}

// A launch config opens the notebook it carries: a text alone is shown
// with no stored result, a snapshot is opened with the text that came
// along kept for when it lands, and bytes that do not decode fail the open.
func TestALaunchConfigHandsANotebookOver(t *testing.T) {
	t.Cleanup(scenetest.Install())
	text, err := fs.ReadFile(samples.FS(), "minimal.md")
	require.NoError(t, err)

	inst := newApp()
	handed, err := inst.handOver(nil)
	require.NoError(t, err)
	assert.False(t, handed)
	_, err = inst.handOver([]byte("not a launch config"))
	require.ErrorContains(t, err, "decode launch config")

	raw, err := buscodec.Encode(launchcfg.NotebookLaunch{Text: string(text), Name: "draft.md"})
	require.NoError(t, err)
	handed, err = inst.handOver(raw)
	require.NoError(t, err)
	require.True(t, handed)
	doc, err := query[Document](t, inst.Operations(), opGetDocument)
	require.NoError(t, err)
	assert.Equal(t, "draft.md", doc.Name)
	assert.Equal(t, "handed over as draft.md", doc.Origin)
	read(t, inst)
	ck, err := query[CheckReport](t, inst.Operations(), opCheckNotebook)
	require.NoError(t, err)
	assert.False(t, ck.Sealed, "the text has no file beside it: nothing it stores is found")
	assert.NotEmpty(t, ck.Problems)
	require.NoError(t, inst.Unmount(nil))

	other := newApp()
	raw, err = buscodec.Encode(launchcfg.NotebookLaunch{Text: "edited", Mount: 0x2a, SnapNanos: 1731500000000000000, Path: "reports/./w45.md"})
	require.NoError(t, err)
	handed, err = other.handOver(raw)
	require.NoError(t, err)
	require.True(t, handed)
	require.NotNil(t, other.opening)
	assert.Equal(t, "2a@1731500000000000000:reports/w45.md", other.opening.String())
	assert.Equal(t, "edited", other.handed)
	assert.Equal(t, "w45.md", other.handedName)
	assert.Nil(t, other.doc, "the snapshot is read first")
}
