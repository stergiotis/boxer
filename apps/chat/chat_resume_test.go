package chat

import (
	"context"
	"encoding/json/v2"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// appOver is appOn over a journal store of the caller's, so two apps can
// share one record.
func appOver(t *testing.T, model openaichat.ClientI, store agentrun.StoreI) (inst *App) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	svc, err := llm.NewService(bus, zerolog.Nop(), llm.Config{Endpoint: "http://127.0.0.1:1234/v1", Model: "m", Client: model, Retain: llm.RetainRing})
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	inst = newApp()
	inst.bus = bus.NewClient(ManifestId, manifest.Caps)
	inst.cli = llm.NewClient(inst.bus)
	inst.cli.Timeout = 5 * time.Second
	inst.coord = inst.newCoordinator(inst.conv.id)
	inst.coord.store = store
	return
}

// A conversation outlives its process (ADR-0308 §SD4–§SD6): another app
// over the same record lists the run, resumes it with its transcript,
// settings and projection intact, tells the model what was left open, and
// continues from the last reply; a run under another fingerprint is
// stranded; a deleted run is read-only.
func TestAConversationResumesFromItsJournal(t *testing.T) {
	store := agentrun.NewMemStore()
	first := appOver(t, &fakeModel{}, store)
	first.keep = true
	require.True(t, first.startTurn("q1"))
	drainUntil(t, first)
	require.True(t, first.startTurn("q2"))
	drainUntil(t, first)
	settleTitle(t, first)
	id := first.conv.id
	w := first.coord.journalNow()
	require.NotNil(t, w)
	// The host stopped mid-call: a consequential intent with no outcome,
	// and a request with no reply, both left in the record.
	_, err := w.Append(context.Background(),
		agentrun.Entry{Kind: agentrun.KindIntent, Key: "k9", Name: "publish_result", Body: `{"name":"x"}`, Effect: agentrun.EffectConsequential, Turn: "t9"},
		agentrun.Entry{Kind: agentrun.KindRequest, Key: "t9#1", Turn: "t9"})
	require.NoError(t, err)
	before := projected(t, first.conv)

	second := appOver(t, &fakeModel{}, store)
	second.listRuns()
	require.Eventually(t, func() bool { second.renderRunsPanelData(); return len(second.runs) == 1 }, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, id, second.runs[0].Id)
	second.resumeRun(id)
	require.Eventually(t, func() bool { second.drain(); return second.conv.id == id }, 5*time.Second, 5*time.Millisecond)
	conv := second.conv
	assert.True(t, conv.keep, "settings restored")
	assert.False(t, conv.stranded)
	require.Len(t, conv.entries, 5, "q1, a1, q2, a2 and the resume line")
	assert.Equal(t, "q1", conv.entries[0].text)
	assert.Equal(t, first.conv.entries[3].text, conv.entries[3].text, "the second answer, whatever the title job's call made its number")
	assert.Contains(t, conv.entries[4].text, "resumed · a consequential command may have run")
	assert.Contains(t, conv.entries[4].text, "publish_result")
	after := projected(t, conv)
	require.Len(t, after, len(before)+1, "the record's messages, then the resume note")
	assert.Equal(t, before, after[:len(before)])
	assert.Contains(t, after[len(after)-1].Content, "This conversation resumed")

	require.True(t, second.startTurn("q3"))
	drainUntil(t, second)
	settleTitle(t, second)
	assert.Equal(t, "a1", second.conv.entries[len(second.conv.entries)-1].text, "the second app's model answers its first call")
	msgs := projected(t, second.conv)
	assert.Equal(t, "q3", msgs[len(msgs)-2].Content)

	// Under another fingerprint the run strands and does not continue.
	run, _, _ := store.GetRun(context.Background(), id)
	run.Fingerprint = "other"
	run.Version++
	require.NoError(t, store.UpdateRun(context.Background(), run))
	third := appOver(t, &fakeModel{}, store)
	third.resumeRun(id)
	require.Eventually(t, func() bool { third.drain(); return third.conv.id == id }, 5*time.Second, 5*time.Millisecond, "resume: %s", third.note.text)
	assert.True(t, third.conv.stranded)
	assert.False(t, third.startTurn("q4"), "a stranded run does not continue")
	run, _, _ = store.GetRun(context.Background(), id)
	assert.Equal(t, agentrun.StateStranded, run.State)

	third.deleteRun(id)
	require.Eventually(t, func() bool {
		third.drain()
		r, _, _ := store.GetRun(context.Background(), id)
		return r.State == agentrun.StateDeleted
	}, 5*time.Second, 5*time.Millisecond, "delete: %s", third.note.text)
	entries, _ := store.Entries(context.Background(), id, 0)
	for _, e := range entries {
		assert.Empty(t, e.Body)
	}
}

// settleTitle waits for the title call the first answer started, and its
// journal entries, so a later writer over the same run is not raced by
// it; two apps over one run is the test's shape, not the host's (ADR-0308
// §SD9).
func settleTitle(t *testing.T, inst *App) {
	t.Helper()
	require.Eventually(t, func() bool { inst.drain(); return !inst.titleJob.Running() }, 5*time.Second, 5*time.Millisecond)
}

// renderRunsPanelData takes the list job's result without drawing.
func (inst *App) renderRunsPanelData() {
	if runs, _, ok := inst.runsJob.TakeResult(); ok {
		inst.runs = *runs
	}
}

func TestTheTranscriptRebuildsFromTheJournal(t *testing.T) {
	entries := []agentrun.Entry{
		{Ordinal: 1, Kind: agentrun.KindSection, Name: "coordinator", Body: "x"},
		{Ordinal: 2, Kind: agentrun.KindUser, Body: "hello", At: time.Unix(1, 0)},
		{Ordinal: 3, Kind: agentrun.KindReply, Key: "r1", Body: "", Calls: projection.CallsBody([]openaichat.ToolCall{{Id: "c1"}})},
		{Ordinal: 4, Kind: agentrun.KindOutcome, Key: "r1#0", Name: "c1", Role: projection.RoleTool, Body: "{}", Form: "listed 0 window(s)"},
		{Ordinal: 5, Kind: agentrun.KindUser, Form: formPixels, Body: "[pixels]"},
		{Ordinal: 6, Kind: agentrun.KindReply, Key: "r2", Body: "done"},
		{Ordinal: 7, Kind: agentrun.KindUser, Body: "dropped", Turn: "t2"},
		{Ordinal: 8, Kind: agentrun.KindEdit, Target: 7, Form: agentrun.FormDrop},
	}
	out := transcriptOf(entries)
	require.Len(t, out, 3)
	assert.Equal(t, speakerUser, out[0].speaker)
	assert.Equal(t, "listed 0 window(s)", out[1].text)
	assert.Equal(t, "done", out[2].text)
}

// ctxStore is a journal store that honours its context, as the host's
// service does and MemStore does not: a write under a done context fails.
type ctxStore struct{ agentrun.StoreI }

func (inst ctxStore) Append(ctx context.Context, entries []agentrun.Entry) (err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	return inst.StoreI.Append(ctx, entries)
}

func (inst ctxStore) UpdateRun(ctx context.Context, r agentrun.Run) (err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	return inst.StoreI.UpdateRun(ctx, r)
}

// blockOnceModel blocks its first call until the context ends and answers
// every later one.
type blockOnceModel struct {
	mu    sync.Mutex
	calls int
}

func (inst *blockOnceModel) Complete(ctx context.Context, _ openaichat.CompletionRequest) (openaichat.CompletionResponse, error) {
	inst.mu.Lock()
	inst.calls++
	n := inst.calls
	inst.mu.Unlock()
	if n == 1 {
		<-ctx.Done()
		return openaichat.CompletionResponse{}, ctx.Err()
	}
	return openaichat.CompletionResponse{Content: "a" + strconv.Itoa(n), FinishReason: "stop", InputTokens: 10, OutputTokens: 5}, nil
}
func (inst *blockOnceModel) Close() (err error) { return }

// A Stop while the model answers does not poison the journal: the turn's
// tail is written under a context the cancel does not end, and the next
// turn runs over the same run.
func TestAStopLeavesTheJournalWritable(t *testing.T) {
	store := ctxStore{agentrun.NewMemStore()}
	inst := appOver(t, &blockOnceModel{}, store)
	require.True(t, inst.startTurn("q1"))
	require.Eventually(t, func() bool { return inst.coord.journalNow() != nil && inst.coord.journalNow().Len() > 2 }, 5*time.Second, 5*time.Millisecond)
	inst.turn.Cancel()
	drainUntil(t, inst)
	w := inst.coord.journalNow()
	require.NotNil(t, w)
	require.True(t, inst.conv.entries[0].failed)
	var turns int
	for _, e := range w.Entries() {
		if e.Kind == agentrun.KindTurn {
			turns++
			assert.Contains(t, e.Body, "failed")
		}
	}
	assert.Equal(t, 1, turns, "the cancelled turn's end is recorded")
	require.True(t, inst.startTurn("q2"))
	drainUntil(t, inst)
	assert.Equal(t, "a2", inst.conv.entries[len(inst.conv.entries)-1].text)
	msgs := projected(t, inst.conv)
	assert.Equal(t, "q2", msgs[len(msgs)-2].Content, "the cancelled turn is not resent")
}

// A run New conversation closed opens again on resume and continues
// (ADR-0308 §SD4).
func TestAClosedRunResumesAndContinues(t *testing.T) {
	store := agentrun.NewMemStore()
	first := appOver(t, &fakeModel{}, store)
	require.True(t, first.startTurn("q1"))
	drainUntil(t, first)
	id := first.conv.id
	first.newConversation()
	require.Eventually(t, func() bool {
		r, _, _ := store.GetRun(context.Background(), id)
		return r.State == agentrun.StateClosed
	}, 5*time.Second, 5*time.Millisecond)

	first.resumeRun(id)
	require.Eventually(t, func() bool { first.drain(); return first.conv.id == id }, 5*time.Second, 5*time.Millisecond, "resume: %s", first.note.text)
	r, _, _ := store.GetRun(context.Background(), id)
	assert.Equal(t, agentrun.StateOpen, r.State, "the run is open again")
	assert.False(t, first.conv.stranded)
	assert.NotEmpty(t, first.conv.parent, "the next turn continues from the last reply")
	require.True(t, first.startTurn("q2"))
	drainUntil(t, first)
	assert.False(t, first.conv.entries[len(first.conv.entries)-2].failed, first.conv.entries[len(first.conv.entries)-2].reason)
	msgs := projected(t, first.conv)
	assert.Equal(t, "q2", msgs[len(msgs)-2].Content)
}

// A process that died mid-call left a reply with tool calls and no result:
// resume records an outcome that says so, so the projection is one a route
// takes; the turn, unanswered, is dropped as a failed turn is and closed,
// so the next turn continues from the last answered reply (ADR-0265 §SD3)
// and a second resume finds nothing open; and the model is told what was
// left open.
func TestAResumeSettlesTheCallsTheProcessLeftOpen(t *testing.T) {
	store := agentrun.NewMemStore()
	p := profileNow()
	settings, _ := json.Marshal(journalSettings{Keep: true, Apps: true})
	w, err := agentrun.Open(context.Background(), store, agentrun.Run{Id: "chat-crashed-1", Conversation: "chat-crashed-1", OwnerApp: string(ManifestId),
		Profile: p.Id, Fingerprint: p.Fingerprint(), Settings: string(settings)})
	require.NoError(t, err)
	meta, _ := jsonMarshal(replyBody{CallId: "llm-1"})
	_, err = w.Append(context.Background(),
		agentrun.Entry{Kind: agentrun.KindSection, Name: "coordinator", Book: promptBookId, Body: promptText("coordinator"), Label: harnessLabel()},
		agentrun.Entry{Kind: agentrun.KindUser, Role: projection.RoleUser, Body: "q1", Turn: "t1", Label: harnessLabel()},
		agentrun.Entry{Kind: agentrun.KindRequest, Key: "t1#0", Turn: "t1"},
		agentrun.Entry{Kind: agentrun.KindReply, Key: "t1#0", Role: projection.RoleAssistant, Name: meta, Calls: projection.CallsBody([]openaichat.ToolCall{{Id: "c1", Name: "set", Arguments: "{}"}}), Turn: "t1", Label: agentrun.Label{Level: agentrun.LevelOwnModel}},
		agentrun.Entry{Kind: agentrun.KindIntent, Key: "llm-1#0", Name: "set", Body: "{}", Effect: agentrun.EffectRun, Turn: "t1"},
		// The host stopped in the last round: the note for that call alone
		// is still live.
		agentrun.Entry{Kind: agentrun.KindNote, Name: "last-round", Form: "once", Body: lastRoundNote, Turn: "t1", Label: harnessLabel()})
	require.NoError(t, err)

	inst := appOver(t, &fakeModel{}, store)
	inst.resumeRun("chat-crashed-1")
	require.Eventually(t, func() bool { inst.drain(); return inst.conv.id == "chat-crashed-1" }, 5*time.Second, 5*time.Millisecond, "resume: %s", inst.note.text)
	entries := inst.coord.journalNow().Entries()
	var settled bool
	for _, e := range entries {
		if e.Kind == agentrun.KindOutcome && e.Name == "c1" {
			settled = true
			assert.Contains(t, e.Body, "outcome unknown")
			assert.Equal(t, "llm-1#0", e.Key)
		}
	}
	assert.True(t, settled, "the open call got an outcome")
	batch := projection.Fold(entries, sectionOrder())
	assert.NoError(t, projection.Validate(batch), "no call is left unanswered")
	for _, m := range batch.Messages {
		assert.NotEqual(t, "q1", m.Content, "the unanswered turn is not resent")
		assert.NotEqual(t, lastRoundNote, m.Content, "the last round's note was for one call")
	}
	assert.Contains(t, inst.conv.entries[len(inst.conv.entries)-1].text, "outcome is unknown")
	assert.True(t, inst.conv.entries[0].failed, "the transcript shows the turn the host stopped in as failed")
	assert.Equal(t, "q1", inst.conv.entries[0].text)
	assert.True(t, inst.conv.apps, "settings restored")
	assert.Empty(t, inst.conv.parent, "the next turn continues from the last answered reply, of which there is none")
	fold := agentrun.Fold(entries)
	assert.Empty(t, fold.OpenTurn, "resume closed the turn")
	assert.Empty(t, fold.Unsettled)
	require.True(t, inst.startTurn("q2"))
	drainUntil(t, inst)
	assert.False(t, inst.conv.entries[len(inst.conv.entries)-2].failed, inst.conv.entries[len(inst.conv.entries)-2].reason)

	// A second resume of the same run repeats nothing.
	n := len(inst.coord.journalNow().Entries())
	inst.newConversation()
	inst.resumeRun("chat-crashed-1")
	require.Eventually(t, func() bool { inst.drain(); return inst.conv.id == "chat-crashed-1" }, 5*time.Second, 5*time.Millisecond, "resume: %s", inst.note.text)
	var notes int
	for _, e := range inst.coord.journalNow().Entries()[n:] {
		if e.Kind == agentrun.KindNote && e.Name == "resume" {
			notes++
		}
	}
	assert.Zero(t, notes, "nothing was left open, so nothing is told again")
}

// A deleted run resumes read-only: its bodies are gone, its chain stands,
// and nothing continues it.
func TestADeletedRunResumesReadOnly(t *testing.T) {
	store := agentrun.NewMemStore()
	first := appOver(t, &fakeModel{}, store)
	require.True(t, first.startTurn("q1"))
	drainUntil(t, first)
	id := first.conv.id
	first.deleteRun(id)
	require.Eventually(t, func() bool { first.drain(); return first.conv.stranded }, 5*time.Second, 5*time.Millisecond, "delete: %s", first.note.text)
	assert.False(t, first.startTurn("q2"), "a deleted conversation does not continue")
	r, _, _ := store.GetRun(context.Background(), id)
	assert.Equal(t, agentrun.StateDeleted, r.State)
	assert.Equal(t, "person", r.DeletedBy)

	second := appOver(t, &fakeModel{}, store)
	second.resumeRun(id)
	require.Eventually(t, func() bool { second.drain(); return second.conv.id == id }, 5*time.Second, 5*time.Millisecond, "resume: %s", second.note.text)
	assert.True(t, second.conv.stranded)
	assert.False(t, second.startTurn("q2"))
	entries := second.coord.journalNow().Entries()
	broken, err := agentrun.Verify(second.coord.journalNow().Run().Digest, entries)
	require.NoError(t, err)
	assert.Zero(t, broken, "the chain verifies after the deletion (L31)")
}

// Deleting the conversation a turn runs in waits for the turn: one writer
// stands on the run (ADR-0308 §SD9).
func TestDeletingTheCurrentConversationWaitsForTheTurn(t *testing.T) {
	store := agentrun.NewMemStore()
	inst := appOver(t, &blockOnceModel{}, store)
	require.True(t, inst.startTurn("q1"))
	inst.deleteRun(inst.conv.id)
	assert.Contains(t, inst.note.text, "a turn is running")
	assert.False(t, inst.action.Running(), "no delete was started")
	inst.turn.Cancel()
	drainUntil(t, inst)
	inst.deleteRun(inst.conv.id)
	require.Eventually(t, func() bool { inst.drain(); return inst.conv.stranded }, 5*time.Second, 5*time.Millisecond, "delete: %s", inst.note.text)
	r, _, _ := store.GetRun(context.Background(), inst.conv.id)
	assert.Equal(t, agentrun.StateDeleted, r.State)
}
