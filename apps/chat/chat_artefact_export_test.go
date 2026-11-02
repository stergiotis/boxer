package chat

import (
	"context"
	"encoding/json/v2"
	"strconv"
	"testing"
	"testing/fstest"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
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

// confineBy writes a tool call whose result carries the confined tier, as a
// read of a sealed window leaves it, marks the conversation confined, and
// returns the call's key.
func confineBy(t *testing.T, inst *App) (key string) {
	t.Helper()
	coord := inst.coord
	key = "llm-confine#0"
	_, err := coord.append(context.Background(), coord.journalNow(),
		agentrun.Entry{Kind: agentrun.KindIntent, Key: key, Name: "sample_rows", Label: harnessLabel()},
		agentrun.Entry{Kind: agentrun.KindOutcome, Role: projection.RoleTool, Key: key, Name: "c9", Body: "rows",
			Label: agentrun.Label{Level: agentrun.LevelExternal, Tier: coord.confinedTier}})
	require.NoError(t, err)
	coord.mu.Lock()
	coord.confined = true
	coord.mu.Unlock()
	return
}

// exportRefusals are the journal's refused-export entries, in order.
func exportRefusals(entries []agentrun.Entry) (out []agentrun.Entry) {
	for _, e := range entries {
		if e.Kind == agentrun.KindIgnorable && e.Name == exportRefusedName {
			out = append(out, e)
		}
	}
	return
}

func refusalOf(t *testing.T, e agentrun.Entry) (rec exportRefusalRecord) {
	t.Helper()
	require.NoError(t, json.Unmarshal([]byte(e.Body), &rec))
	return
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

// A store the service cannot reach is a failure, not a refusal: it leaves
// neither an export nor a refusal.
func TestAnExportTheServiceCannotWriteLeavesNoRecord(t *testing.T) {
	inst := exportRig(t, nil)
	withScreenshot(t, inst.conv.art)
	inst.planExport(false)
	_, err := inst.exportNow(context.Background(), inst.artView.export, "x")
	var re *ladingpublish.RefusedError
	require.ErrorAs(t, err, &re)
	assert.Contains(t, re.Result.Reason, "no lading store")
	assert.True(t, re.Result.Failed)
	entries := inst.coord.journalNow().Entries()
	_, ok := lastExport(entries)
	assert.False(t, ok)
	assert.Empty(t, exportRefusals(entries))
	assert.True(t, inst.coord.endorsedHead(), "the endorsement came first, as law L15 asks")
}

func TestAConfinedConversationCannotExport(t *testing.T) {
	inst := exportRig(t, nil)
	withScreenshot(t, inst.conv.art)
	key := confineBy(t, inst)
	w := inst.coord.journalNow()
	n, _ := inst.conv.art.head()
	named := revisionsNamed(w.Entries())
	artN, artText, artOk := artefactOfEntries(w.Entries())
	inst.planExport(false)
	p := inst.artView.export
	require.True(t, p.confined)
	assert.Equal(t, key, p.confinedKey)

	// The confirmation refuses, and the refusal is on record with the call
	// that confined the conversation.
	require.Eventually(t, func() bool { return len(exportRefusals(w.Entries())) == 1 }, 5*time.Second, 5*time.Millisecond)
	first := exportRefusals(w.Entries())[0]
	assert.Equal(t, exportRefusedSealed, first.Form)
	assert.Equal(t, key, first.Key)
	assert.Equal(t, uint32(n), first.Target)
	assert.Equal(t, harnessLabel(), first.Label)
	rec := refusalOf(t, first)
	assert.Equal(t, exportStageConfirmation, rec.Stage)
	assert.Equal(t, n, rec.Revision)
	assert.Equal(t, "sample_rows (tool call c9)", rec.ConfinedBy)
	assert.Equal(t, exportRefusal(rec.ConfinedBy), rec.Reason)

	// The confirmation offers no Export; were it sent anyway, the service
	// refuses it before anything is written, and that is on record too.
	_, err := inst.exportNow(context.Background(), p, "x")
	var re *ladingpublish.RefusedError
	require.ErrorAs(t, err, &re)
	assert.True(t, re.Result.Refused)
	assert.Contains(t, re.Result.Reason, "sealed data")
	assert.False(t, inst.coord.endorsedHead(), "a refused export leaves no endorsement")
	refusals := exportRefusals(w.Entries())
	require.Len(t, refusals, 2)
	assert.Equal(t, exportRefusedSealed, refusals[1].Form)
	assert.Equal(t, key, refusals[1].Key)
	rec = refusalOf(t, refusals[1])
	assert.Equal(t, exportStageCheck, rec.Stage)
	assert.Contains(t, rec.Reason, "sealed data")

	// Nothing that reads exports or the artefact's snapshots takes a
	// refusal for one of theirs.
	entries := w.Entries()
	_, ok := lastExport(entries)
	assert.False(t, ok)
	assert.Equal(t, named, revisionsNamed(entries))
	gotN, gotText, gotOk := artefactOfEntries(entries)
	assert.Equal(t, artN, gotN)
	assert.Equal(t, artText, gotText)
	assert.Equal(t, artOk, gotOk)
	for _, e := range entries {
		assert.NotEqual(t, agentrun.KindEndorse, e.Kind, "a refused export endorses nothing")
	}
}

// A resumed conversation finds its export, its numbering and its artefact
// as if the refusals after them were not there.
func TestARefusalIsNotAnExportOnResume(t *testing.T) {
	inst := exportRig(t, nil)
	coord := inst.coord
	ctx := context.Background()
	_, err := coord.append(ctx, coord.journalNow(), agentrun.Entry{Kind: agentrun.KindIgnorable, Name: artefactEntryName, Form: artefactFormTurn,
		Target: 3, Body: "three\n", Label: harnessLabel()})
	require.NoError(t, err)
	exported := exportRecord{Revision: 3, Mount: 7, SnapNanos: 9, TextDigest: agentrun.ContentDigest("three\n")}
	require.NoError(t, coord.journalExport(ctx, exported))
	require.NoError(t, coord.journalExportRefusal(ctx, exportRefusedSealed, "llm-x#0", exportRefusalRecord{Revision: 9, Stage: exportStageConfirmation, Reason: "sealed"}))
	require.NoError(t, coord.journalExportRefusal(ctx, exportRefusedService, "", exportRefusalRecord{Revision: 11, Stage: exportStageCheck, Reason: "too large"}))

	entries := coord.journalNow().Entries()
	rec, ok := lastExport(entries)
	require.True(t, ok)
	assert.Equal(t, exported, rec)
	assert.Equal(t, 3, revisionsNamed(entries), "a refusal names no revision a resume must skip")
	coord.mu.Lock()
	coord.lastExportMount = 0
	coord.mu.Unlock()
	assert.Equal(t, uint64(7), coord.exportMount())
	n, text, ok := artefactOfEntries(entries)
	require.True(t, ok)
	assert.Equal(t, 3, n)
	assert.Equal(t, "three\n", text)
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
	entries := inst.coord.journalNow().Entries()
	_, ok := lastExport(entries)
	assert.False(t, ok)
	refusals := exportRefusals(entries)
	require.Len(t, refusals, 1)
	assert.Equal(t, exportRefusedService, refusals[0].Form)
	assert.Equal(t, exportStageCheck, refusalOf(t, refusals[0]).Stage)
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

// Open in mdedit hands over the export of the head only while the head is
// the text exported.
func TestTheHeadsExportIsFoundByItsText(t *testing.T) {
	inst := exportRig(t, nil)
	withScreenshot(t, inst.conv.art)
	n, text := inst.conv.art.head()
	rec := exportRecord{Revision: n, Mount: 7, SnapNanos: 9, TextDigest: agentrun.ContentDigest(text)}
	require.NoError(t, inst.coord.journalExport(context.Background(), rec))
	entries := inst.coord.journalNow().Entries()
	got, ok := exportOf(entries, n, text)
	require.True(t, ok)
	assert.Equal(t, rec.Mount, got.Mount)
	_, ok = exportOf(entries, n, text+"edited")
	assert.False(t, ok, "the text moved under the same number")
	_, ok = exportOf(entries, n+1, text)
	assert.False(t, ok, "another revision")
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
