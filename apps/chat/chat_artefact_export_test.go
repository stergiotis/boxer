package chat

import (
	"context"
	"strconv"
	"testing"
	"testing/fstest"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/ladingpublish"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// exportRig is an app with a journal, a screenshot store of the test's, and
// the host's publish service on its bus, connecting through connect.
func exportRig(t *testing.T, connect func(ctx context.Context) (recordstore.ExecutorI, error)) (inst *App) {
	t.Helper()
	return exportRigWith(t, connect, ladingpublish.Limits{Ceiling: agentrun.TierAmber, MaxTtlDays: 90, MaxBytes: 1 << 24})
}

func exportRigWith(t *testing.T, connect func(ctx context.Context) (recordstore.ExecutorI, error), limits ladingpublish.Limits) (inst *App) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	svc, err := llm.NewService(bus, zerolog.Nop(), llm.Config{Endpoint: "http://127.0.0.1:1234/v1", Model: "m", Client: &fakeModel{}, Retain: llm.RetainRing})
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	pub, err := ladingpublish.NewService(ladingpublish.Config{Bus: bus, Registry: app.DefaultRegistry, Connect: connect,
		Limits: limits, Log: zerolog.Nop()})
	require.NoError(t, err)
	t.Cleanup(pub.Close)
	inst = newApp()
	inst.bus = bus.NewClient(ManifestId, manifest.Caps)
	inst.cli = llm.NewClient(inst.bus)
	inst.cli.Timeout = 5 * time.Second
	inst.coord = inst.newCoordinator(inst.conv.id)
	inst.coord.store = agentrun.NewMemStore()
	inst.keep = true
	inst.artefact = true
	inst.conv.art.store = testStore(t, roomy())
	inst.conv.art.setPolicy(artPolicy{write: true})
	inst.coord.offerArtefact(inst.conv.art)
	require.True(t, inst.startTurn("q1"))
	drainUntil(t, inst)
	require.NotNil(t, inst.coord.journalNow())
	return
}

// withScreenshot commits a revision embedding one screenshot and returns its
// name.
func withScreenshot(t *testing.T, art *artefact) string {
	t.Helper()
	h, w, hh, err := art.store.put(testPNG(t, 12, 9, 3))
	require.NoError(t, err)
	shot := artImage{name: "screenshot-1.png", hash: h, w: w, h: hh, bytes: 100}
	n, _ := art.head()
	_, err = art.commit(n, artRevision{text: "# A\n\n![[screenshot-1.png]]\n", images: []artImage{shot}, ownImages: true, source: revSourceModel})
	require.NoError(t, err)
	return shot.name
}

func TestTheExportPlanNamesWhatGoesAndWhetherItEndorses(t *testing.T) {
	inst := exportRig(t, nil)
	name := withScreenshot(t, inst.conv.art)
	inst.planExport(true)
	p := inst.artView.export
	require.NotNil(t, p)
	n, _ := inst.conv.art.head()
	assert.Equal(t, n, p.rev)
	require.Len(t, p.images, 1)
	assert.Equal(t, name, p.images[0].name)
	assert.True(t, p.endorse, "a model wrote the head and nothing endorsed it")
	assert.False(t, p.confined)
	assert.True(t, p.thenMdedit)
}

func TestAnExportTheServiceCannotWriteLeavesNoRecord(t *testing.T) {
	inst := exportRig(t, nil)
	withScreenshot(t, inst.conv.art)
	inst.planExport(false)
	_, err := inst.exportNow(context.Background(), inst.artView.export, "x")
	var re *ladingpublish.RefusedError
	require.ErrorAs(t, err, &re)
	assert.Contains(t, re.Result.Reason, "no lading store")
	_, ok := lastExport(inst.coord.journalNow().Entries())
	assert.False(t, ok)
	assert.True(t, inst.coord.endorsedHead(), "the endorsement came first, as law L15 asks")
}

func TestAConfinedConversationCannotExport(t *testing.T) {
	inst := exportRig(t, nil)
	withScreenshot(t, inst.conv.art)
	inst.coord.mu.Lock()
	inst.coord.confined = true
	inst.coord.mu.Unlock()
	inst.planExport(false)
	p := inst.artView.export
	require.True(t, p.confined)
	// The confirmation offers no Export; were it sent anyway, the service
	// refuses it before anything is written.
	_, err := inst.exportNow(context.Background(), p, "x")
	var re *ladingpublish.RefusedError
	require.ErrorAs(t, err, &re)
	assert.True(t, re.Result.Refused)
	assert.Contains(t, re.Result.Reason, "sealed data")
	assert.False(t, inst.coord.endorsedHead(), "a refused export leaves no endorsement")
}

// What the service would refuse is refused before the endorsement: the
// journal holds no consent to a publish that could not happen.
func TestARefusedExportIsNotEndorsed(t *testing.T) {
	inst := exportRigWith(t, nil, ladingpublish.Limits{Ceiling: agentrun.TierAmber, MaxTtlDays: 90, MaxBytes: 16})
	withScreenshot(t, inst.conv.art)
	inst.planExport(false)
	require.True(t, inst.artView.export.endorse)
	_, err := inst.exportNow(context.Background(), inst.artView.export, "x")
	var re *ladingpublish.RefusedError
	require.ErrorAs(t, err, &re)
	assert.Contains(t, re.Result.Reason, "more than the 16")
	assert.False(t, inst.coord.endorsedHead())
	_, ok := lastExport(inst.coord.journalNow().Entries())
	assert.False(t, ok)
}

// An endorsement from before a resume covers the text it endorsed, not a
// revision the model writes afterwards under a number it once had.
func TestAnEndorsementFromBeforeAResumeCoversOnlyItsText(t *testing.T) {
	inst := exportRig(t, nil)
	withScreenshot(t, inst.conv.art)
	n, text := inst.conv.art.head()
	require.NoError(t, inst.coord.endorseArtefact(context.Background(), n, text))
	require.True(t, inst.coord.endorsedHead())
	entries := inst.coord.journalNow().Entries()
	assert.Equal(t, n, revisionsNamed(entries))

	// Numbered above what the journal names, as resume numbers it.
	resumed := testArtefact(t)
	resumed.reserveThrough(revisionsNamed(entries))
	m, err := resumed.commit(0, artRevision{text: "other\n", source: revSourceModel})
	require.NoError(t, err)
	assert.Greater(t, m, n)
	inst.coord.offerArtefact(resumed)
	assert.False(t, inst.coord.endorsedHead())

	// Were the number handed out again, the text still tells them apart.
	again := testArtefact(t)
	for i := 0; i < n; i++ {
		head, _ := again.head()
		_, err = again.commit(head, artRevision{text: "other " + strconv.Itoa(i) + "\n", source: revSourceModel})
		require.NoError(t, err)
	}
	head, _ := again.head()
	require.Equal(t, n, head)
	inst.coord.offerArtefact(again)
	assert.False(t, inst.coord.endorsedHead())
}

func TestAnExportOfAMovedHeadIsRefused(t *testing.T) {
	inst := exportRig(t, nil)
	withScreenshot(t, inst.conv.art)
	inst.planExport(false)
	n, _ := inst.conv.art.head()
	_, err := inst.conv.art.commit(n, artRevision{text: "moved\n", source: revSourceModel})
	require.NoError(t, err)
	_, err = inst.exportNow(context.Background(), inst.artView.export, "x")
	assert.ErrorContains(t, err, "moved")
}

// A resumed conversation gets the exported revision back under its own
// number, its screenshots checked against the hashes the export recorded.
func TestAnExportRestoresAsTheRevisionItWas(t *testing.T) {
	png := testPNG(t, 12, 9, 3)
	doc := "# A\n\n![[shot.png]]\n"
	v := fstest.MapFS{exportDocPath: {Data: []byte(doc)}, "shot.png": {Data: png}}
	digest := agentrun.ContentDigest(doc)
	art := testArtefact(t)
	require.NoError(t, restoreFrom(v, exportRecord{Revision: 4, TextDigest: digest, Images: []exportImage{{Name: "shot.png", Hash: imageHash(png)}}}, art))
	n, text := art.head()
	assert.Equal(t, 4, n)
	assert.Contains(t, text, "![[shot.png]]")
	set := art.headImages()
	require.Len(t, set, 1)
	assert.Equal(t, imageSourceRestored, set[0].source)
	assert.Equal(t, revSourceModel, art.revisions()[0].source, "restored text keeps the model's integrity (ADR-0311 §SD5)")

	err := restoreFrom(v, exportRecord{Revision: 1, TextDigest: digest, Images: []exportImage{{Name: "shot.png", Hash: "00"}}}, testArtefact(t))
	assert.ErrorContains(t, err, "not the ones exported")
	err = restoreFrom(v, exportRecord{Revision: 1, TextDigest: agentrun.ContentDigest("else"), Images: nil}, testArtefact(t))
	assert.ErrorContains(t, err, "not the one exported")
}

// A resume takes the export's revision, with its screenshots, and puts the
// journal's head on top only when the journal's is later.
func TestTheJournalsHeadGoesOverAnEarlierExport(t *testing.T) {
	exported := func() *artefact {
		art := testArtefact(t)
		art.lastN = 2
		_, err := art.commit(0, artRevision{text: "exported\n", ownImages: true, source: revSourceModel})
		require.NoError(t, err)
		return art
	}

	art := exported()
	require.True(t, art.restore(5, "later\n"))
	n, text := art.head()
	assert.Equal(t, 5, n)
	assert.Equal(t, "later\n", text)
	t3, ok := art.text(3)
	require.True(t, ok, "the export's revision stays, for a revert")
	assert.Equal(t, "exported\n", t3)

	art = exported()
	assert.False(t, art.restore(3, "same\n"), "the export is the journal's head or later")
	n, text = art.head()
	assert.Equal(t, 3, n)
	assert.Equal(t, "exported\n", text)

	assert.True(t, testArtefact(t).restore(4, "only\n"))
}
