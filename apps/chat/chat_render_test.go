package chat

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The window draws under the discard channel with every decision it can
// ask open and every side view opened in turn (ADR-0404 §SD7): a
// mismatched id stack compiles and vets clean and panics here.
func TestTheWindowRendersEveryDecisionAndView(t *testing.T) {
	t.Cleanup(egui2test.Install())
	inst := newApp()
	inst.artefact = true
	inst.model, inst.answered = llm.Description{Configured: true, Model: "scripted", EndpointHost: "localhost", Local: true}, true
	inst.coord = newCoordinator(nil, nil, inst.conv.id)
	inst.turnChip = inst.newTurnChip()
	frame := func() {
		inst.render()
		c.CurrentApplicationState.StateManager.ScriptReset()
	}

	frame()
	assert.Empty(t, inst.side.opened(inst.menuViews()), "the transcript has the window until something earns room")

	// A proposal brings the document forward, once.
	inst.conv.artefact = true
	art := inst.conv.art
	art.mu.Lock()
	art.proposal = &artProposal{seq: 1, text: "# One\n\ntwo\n", tool: "artefact_write", reply: make(chan bool, 1)}
	art.mu.Unlock()
	frame()
	frame()
	require.Equal(t, []chatTabE{tabArtefact}, inst.side.opened(inst.menuViews()))
	inst.setView(tabArtefact, false)
	frame()
	assert.Empty(t, inst.side.opened(inst.menuViews()), "closed over the same decision, it stays closed")

	// The export confirmation, a stale one, and a screenshot's view.
	inst.side.show(tabArtefact)
	inst.planExport(false)
	frame()
	inst.artView.export.rev = 7
	frame()
	inst.coord.pix.ask = &pixelAsk{name: "screenshot-1.png", hash: "h", w: 4, h: 3, origin: "captured from window 2", level: agent.PixelsAskOnce, reply: make(chan pixelVerdictE, 1)}
	frame()

	// The model's form and the harness's buttons, in the waiting bubble.
	inst.pending = &pendingTurn{started: time.Now()}
	qs := []askQuestion{{Question: "Which format?", Options: []askOption{{Label: "Markdown"}, {Label: "Plain"}}}}
	inst.coord.ask.open = &openAsk{seq: 1, questions: qs, reply: make(chan askReply, 2), form: newAskForm(qs)}
	frame()
	inst.coord.ask.open.form.incomplete = true
	frame()
	stop := []askQuestion{{Header: "Stop", Question: "The stop fired.", Buttons: true,
		Options: []askOption{{Label: stopChoiceContinue, Description: "the turn goes on"}, {Label: stopChoiceStop, Description: "the turn ends here"}}}}
	inst.coord.ask.open = &openAsk{seq: 2, questions: stop, reply: make(chan askReply, 2), form: newAskForm(stop)}
	frame()
	inst.coord.ask.open, inst.pending = nil, nil

	// Every view opened from the View menu, joining the others.
	for _, tab := range inst.menuViews() {
		inst.setView(tab, true)
		frame()
	}
	assert.Equal(t, inst.menuViews(), inst.side.opened(inst.menuViews()))

	// The chip's inspector, with the facts.
	inst.turnChip.Open()
	frame()
	inst.conv.started = true
	frame()
}

func TestTheBarSaysWhatTheModelMayDoInWords(t *testing.T) {
	inst := newApp()
	assert.Equal(t, "Model: talks only", inst.mayWords(), "no host to share windows with")
	inst.coord = newCoordinator(nil, nil, inst.conv.id)
	inst.apps = false
	assert.Equal(t, "Model: talks only", inst.mayWords())
	inst.apps = true
	inst.syncAuthority()
	assert.Equal(t, "Model: may edit and run in your windows", inst.mayWords(), "the default ceiling, run")
	inst.apps, inst.artefact = false, true
	inst.syncAuthority()
	assert.Equal(t, "Model: talks and writes the artefact", inst.mayWords())
}

func TestReaderLineSaysWhatWasDone(t *testing.T) {
	assert.Equal(t, "set sql in window 2 · done", readerLine("set_sql in window 2 · rendered"))
	assert.Equal(t, "Reading the clusters · get projection in window 2 · done", readerLine("Reading the clusters · get_projection in window 2 · completed"))
	assert.Equal(t, "wrote the artefact · revision 1 · lines 1–27 · 1 finding", readerLine("artefact_write · revision 1 · lines 1–27 · 1 finding"))
	assert.Equal(t, "read 3 rows of the host's windows table", readerLine("read 3 row(s) of keelson('windows')"))
	assert.Equal(t, "read the windows: no client", readerLine("query_windows: no client"))
	assert.Equal(t, "clear marks: no task", readerLine("clear marks: no task"))
	assert.Equal(t, "some op: no task", readerLine("some_op: no task"))
	assert.Equal(t, "set note in window 2 · refused · the buffer moved", readerLine("set_note in window 2 · refused · the buffer moved"))
	assert.Equal(t, "opened play as window 2", readerLine("opened play as window 2"))
	assert.Equal(t, "access granted for a task", readerLine("access granted for task task-b0248341cbb3"))
}

func TestBarTitleIsCutAtAWord(t *testing.T) {
	assert.Equal(t, "Calls by model, last hour", barTitle("Calls by model, last hour"))
	assert.Equal(t, "Summarise what agents did on…", barTitle("Summarise what agents did on this host in the last 24 hours"))
}

// A compact pane draws every view its View menu and its row can open in the
// transcript's place (ADR-0368, ADR-0404 §SD8): the menu's views are drawn
// by the same bodies as the window's dock.
func TestTheCompactPaneDrawsEveryViewItOpens(t *testing.T) {
	t.Cleanup(egui2test.Install())
	inst := embedded(t, "notes", EmbedLayoutCompact)
	require.True(t, inst.compact())
	inst.coord = newCoordinator(nil, nil, inst.conv.id)
	inst.turnChip = inst.newTurnChip()
	frame := func() {
		inst.render()
		c.CurrentApplicationState.StateManager.ScriptReset()
	}
	frame()
	for _, tab := range append(inst.menuViews(), tabArtefact) {
		inst.setView(tab, true)
		frame()
		frame()
		assert.Equal(t, tab, inst.embed.shown)
		assert.True(t, inst.viewOpen(tab))
		inst.setView(tab, false)
		frame()
		assert.False(t, inst.viewOpen(tab))
	}
}
