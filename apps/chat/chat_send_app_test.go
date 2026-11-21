package chat

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
)

// sendRig is a chat window with an artefact, over the llm service on an
// in-process bus, holding the app's manifest caps; Pixels is at level.
func sendRig(t *testing.T, level agent.PixelsE) (inst *App, fm *fakeModel, bus *inprocbus.Inst) {
	t.Helper()
	bus = inprocbus.NewInst(zerolog.Nop())
	fm = &fakeModel{}
	svc, err := llm.NewService(bus, zerolog.Nop(), llm.Config{Journal: llm.AcceptAnyJournal{}, Endpoint: "http://127.0.0.1:1234/v1", Model: "m", Client: fm, Retain: llm.RetainRing})
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	inst = newApp()
	inst.bus = bus.NewClient(ManifestId, manifest.Caps)
	inst.ioCtx = t.Context()
	inst.cli = llm.NewClient(inst.bus)
	inst.cli.Timeout = 5 * time.Second
	inst.coord = inst.newCoordinator(inst.conv.id)
	inst.coord.store = agentrun.NewMemStore()
	inst.keep, inst.artefact = true, true
	inst.conv.art.store = testStore(t, roomy())
	inst.conv.art.setPolicy(artPolicy{write: true})
	inst.coord.offerArtefact(inst.conv.art)
	inst.perms = inst.perms.withPixels(level, false)
	inst.coord.setPixels(inst.pixelPolicy())
	return
}

// Add image reads the one file the dialog grants, through the handle and
// under the manifest's capability, and the file becomes an image of the set
// (ADR-0385 §SD1); a dialog the person closes adds nothing.
func TestAddImageReadsTheFileTheDialogGrants(t *testing.T) {
	inst, _, bus := sendRig(t, agent.PixelsNone)
	broker, err := fsbroker.NewService(bus, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(broker.Close)
	path := filepath.Join(t.TempDir(), "Site Plan.jpg")
	file := testJPEG(t, 32, 24)
	require.NoError(t, os.WriteFile(path, file, 0o600))
	pending := func() (req fsbroker.PendingRequest) {
		require.Eventually(t, func() bool {
			all := broker.Pending()
			if len(all) == 1 {
				req = all[0]
			}
			return len(all) == 1
		}, 5*time.Second, time.Millisecond)
		return
	}

	type picked struct {
		data   []byte
		name   string
		picked bool
		err    error
	}
	out := make(chan picked, 1)
	pick := func() {
		d, n, p, err := pickFile(t.Context(), inst.bus)
		out <- picked{d, n, p, err}
	}
	go pick()
	_, err = broker.Resolve(pending().Id, path)
	require.NoError(t, err)
	got := <-out
	require.NoError(t, got.err)
	require.True(t, got.picked)
	assert.Equal(t, file, got.data)
	assert.Equal(t, "Site Plan.jpg", got.name, "the dialog names the file, not where it is")
	name, _, err := inst.conv.art.addUpload(got.data, got.name)
	require.NoError(t, err)
	assert.Equal(t, "upload-1.jpg", name)

	go pick()
	require.NoError(t, broker.Cancel(pending().Id))
	got = <-out
	require.NoError(t, got.err)
	assert.False(t, got.picked)
}

// The window sends what is chosen with the message, and the transcript
// names it; at Metadata only it sends nothing and keeps the draft's turn
// from starting (ADR-0385 §SD3, §SD4).
func TestTheWindowSendsWhatIsChosen(t *testing.T) {
	inst, fm, _ := sendRig(t, agent.PixelsAskEach)
	inst.coord.discloseHook = func(agent.Disclosure) error { return nil }
	name, _, err := inst.conv.art.addUpload(testPNG(t, 16, 16, 2), "mine.png")
	require.NoError(t, err)
	assert.Empty(t, inst.sendOffer())

	inst.toggleSend(name)
	inst.toggleSend("gone.png")
	choice := inst.sendChoice()
	require.Len(t, choice, 1, "a name the set does not hold leaves the choice")
	assert.Equal(t, []string{name}, inst.sendNames)
	require.True(t, inst.startTurnWith("what is it?", choice))
	drainUntil(t, inst)

	require.Len(t, inst.conv.entries, 2, "%+v", inst.conv.entries)
	mine := inst.conv.entries[0]
	assert.Equal(t, "what is it?", mine.text)
	assert.Equal(t, []sentRef{{name: name, hash: choice[0].hash}}, mine.sent)
	fm.mu.Lock()
	require.NotEmpty(t, fm.seen)
	var images int
	for _, m := range fm.seen[0].Messages {
		images += len(m.Images)
	}
	fm.mu.Unlock()
	assert.Equal(t, 1, images, "the turn's request carries the image")

	// Edit takes the turn back with its images chosen again; Cancel edit
	// puts the turn back and empties the choice (ADR-0385 §SD6).
	inst.sendNames = nil
	inst.edit()
	assert.Equal(t, "what is it?", inst.draft)
	assert.Equal(t, []string{name}, inst.sendNames)
	require.NotNil(t, inst.editing)
	inst.cancelEdit()
	assert.Empty(t, inst.draft)
	assert.Empty(t, inst.sendNames)
	require.Len(t, inst.conv.entries, 2)

	inst.perms = inst.perms.withPixels(agent.PixelsNone, false)
	inst.coord.setPixels(inst.pixelPolicy())
	assert.Contains(t, inst.sendOffer(), "Metadata only")
	assert.False(t, inst.startTurnWith("and this?", choice))
	assert.Nil(t, inst.pending)
	note, ok := inst.noteShown()
	require.True(t, ok)
	assert.True(t, note.failed)
	assert.Contains(t, note.text, "not sent")
	// The window may have asked the model for a title meanwhile; nothing
	// of the refused message is in any request.
	fm.mu.Lock()
	defer fm.mu.Unlock()
	for _, req := range fm.seen {
		for _, m := range req.Messages {
			assert.NotContains(t, m.Content, "and this?", "nothing of it left the host")
		}
	}
}

// A send the host cannot record is not sent: the turn ends with nothing
// written, and the message comes back to the composer with its images
// chosen and the reason on the status line (ADR-0385 §SD4).
func TestASendTheHostCannotRecordComesBack(t *testing.T) {
	inst, fm, _ := sendRig(t, agent.PixelsAskEach)
	inst.coord.discloseHook = func(agent.Disclosure) error { return errors.New("the trail is down") }
	name, _, err := inst.conv.art.addUpload(testPNG(t, 16, 16, 2), "mine.png")
	require.NoError(t, err)
	inst.toggleSend(name)
	require.True(t, inst.startTurnWith("what is it?", inst.sendChoice()))
	inst.sendNames = nil
	drainUntil(t, inst)

	fm.mu.Lock()
	assert.Empty(t, fm.seen, "no request left the host")
	fm.mu.Unlock()
	assert.Empty(t, inst.conv.entries, "the message is not left in the transcript as a failed turn")
	assert.Equal(t, "what is it?", inst.draft)
	assert.Equal(t, []string{name}, inst.sendNames)
	note, ok := inst.noteShown()
	require.True(t, ok)
	assert.Contains(t, note.text, "the trail is down")
}
