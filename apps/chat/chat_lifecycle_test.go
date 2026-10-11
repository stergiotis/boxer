package chat

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// A delete refused because another action runs installs nothing: when the
// action lands, the conversation is not stranded.
func TestADeleteRefusedWhileAnActionRunsStrandsNothing(t *testing.T) {
	store := agentrun.NewMemStore()
	inst := appOver(t, &fakeModel{}, store)
	require.True(t, inst.startTurn("q1"))
	drainUntil(t, inst)
	release := make(chan struct{})
	require.True(t, inst.runAction("chat-test", "copy", func(ctx context.Context) (string, error) {
		<-release
		return "copied", nil
	}, nil))
	inst.deleteRun(inst.conv.id)
	assert.Contains(t, inst.note.text, "still running")
	assert.Nil(t, inst.afterAction, "a refused delete installs no hook")
	assert.Empty(t, inst.deleting)
	close(release)
	require.Eventually(t, func() bool { inst.drain(); return inst.note.text == "copied" }, 5*time.Second, 5*time.Millisecond)
	assert.False(t, inst.conv.stranded)
	r, _, _ := store.GetRun(context.Background(), inst.conv.id)
	assert.Equal(t, agentrun.StateOpen, r.State)
}

// failingDeleteStore refuses the tombstone: a delete that fails.
type failingDeleteStore struct{ agentrun.StoreI }

var errTombstone = errors.New("the store is read-only today")

func (inst failingDeleteStore) Tombstone(context.Context, string) error { return errTombstone }

// A delete that fails runs nothing after it: the next action that lands
// well does not strand the conversation, and a turn may start again.
func TestAFailedDeleteRunsNothingAfterIt(t *testing.T) {
	store := failingDeleteStore{agentrun.NewMemStore()}
	inst := appOver(t, &fakeModel{}, store)
	require.True(t, inst.startTurn("q1"))
	drainUntil(t, inst)
	id := inst.conv.id
	inst.deleteRun(id)
	assert.Equal(t, id, inst.deleting, "no turn starts over a run being deleted")
	assert.False(t, inst.startTurn("q2"))
	assert.Contains(t, inst.note.text, "being deleted")
	require.Eventually(t, func() bool { inst.drain(); return strings.Contains(inst.note.text, errTombstone.Error()) }, 5*time.Second, 5*time.Millisecond)
	assert.Nil(t, inst.afterAction)
	assert.Empty(t, inst.deleting)
	require.True(t, inst.runAction("chat-test", "copy", func(ctx context.Context) (string, error) { return "copied", nil }, nil))
	require.Eventually(t, func() bool { inst.drain(); return inst.note.text == "copied" }, 5*time.Second, 5*time.Millisecond)
	assert.False(t, inst.conv.stranded, "the failed delete's hook did not run on the copy")
}

// A compaction re-tells the snapshots the cut took and composes no notice:
// a pending notice — the pixels one here, the changes note alike — is
// told at the next turn's start, not spent by the cut (ADR-0269 §SD8).
func TestACompactionSpendsNoNotice(t *testing.T) {
	inst := appOver(t, &fakeModel{}, agentrun.NewMemStore())
	coord := inst.coord
	coord.offer(true, false)
	coord.offerArtefact(inst.conv.art)
	coord.setPixels(pixelPolicy{level: agent.PixelsCaptures, localOnly: true, modelLocal: true, endpoint: "m"})
	ctx := context.Background()
	w, err := coord.openJournal(ctx, false)
	require.NoError(t, err)
	_, err = w.Append(ctx, agentrun.Entry{Kind: agentrun.KindUser, Role: projection.RoleUser, Body: "q1", Turn: "t1", Label: harnessLabel()})
	require.NoError(t, err)
	// The ceiling was told once; the pixels notice is pending.
	require.NotEmpty(t, coord.ceilingNote())
	s := &requestState{ctx: ctx, jctx: ctx, coord: coord, w: w, req: llm.Request{Turn: "t2"}}
	plan := projection.Plan{Verdict: projection.VerdictElide, Cut: 2, Elided: "", Label: harnessLabel()}
	require.NoError(t, coord.compact(s, plan, profileNow().Compact))
	var notes []string
	for _, e := range w.Entries() {
		if e.Kind == agentrun.KindNote {
			notes = append(notes, e.Name)
		}
	}
	assert.Equal(t, []string{"ceiling", "artefact"}, notes, "the snapshots are told again; no notice is composed")
	assert.NotEmpty(t, coord.pixelsNote(), "the pixels notice is still news")
}

// A turn that starts after the conversation moved on writes nothing: the
// coordinator retired, and the turn ends before its first act.
func TestATurnAfterRetirementWritesNothing(t *testing.T) {
	store := agentrun.NewMemStore()
	coord := newCoordinator(nil, nil, "chat-retired-1")
	coord.store = store
	coord.retire()
	_, err := runTurn(context.Background(), nil, coord, llm.Request{Turn: "t1", Messages: []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "q1"}}}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "replaced")
	runs, err := store.ListRuns(context.Background(), string(ManifestId), 10)
	require.NoError(t, err)
	assert.Empty(t, runs)
	assert.True(t, coord.awaitClosed(context.Background()), "nothing to close: closed at once")
}

// slowCloseStore lands a close late, as a host's service under load would.
type slowCloseStore struct {
	agentrun.StoreI
	delay time.Duration
}

func (inst slowCloseStore) UpdateRun(ctx context.Context, r agentrun.Run) (err error) {
	if r.State == agentrun.StateClosed {
		time.Sleep(inst.delay)
	}
	return inst.StoreI.UpdateRun(ctx, r)
}

// A resume of the run this window just moved on from waits for its close:
// the late close never lands on the writer resume adopted, and the run is
// open again when the next turn runs.
func TestAResumeWaitsForTheLateClose(t *testing.T) {
	store := slowCloseStore{agentrun.NewMemStore(), 300 * time.Millisecond}
	inst := appOver(t, &fakeModel{}, store)
	require.True(t, inst.startTurn("q1"))
	drainUntil(t, inst)
	id := inst.conv.id
	inst.newConversation()
	require.Contains(t, inst.retiring, id)
	inst.resumeRun(id)
	assert.NotContains(t, inst.retiring, id, "the resume took the retired coordinator over")
	require.Eventually(t, func() bool { inst.drain(); return inst.conv.id == id }, 5*time.Second, 5*time.Millisecond, "resume: %s", inst.note.text)
	r, _, _ := store.GetRun(context.Background(), id)
	assert.Equal(t, agentrun.StateOpen, r.State)
	require.True(t, inst.startTurn("q2"))
	drainUntil(t, inst)
	last := inst.conv.entries[len(inst.conv.entries)-2]
	assert.False(t, last.failed, last.reason)
	time.Sleep(400 * time.Millisecond)
	r, _, _ = store.GetRun(context.Background(), id)
	assert.Equal(t, agentrun.StateOpen, r.State, "no close landed after the resume")
}

// failOnceStore refuses one append, the one after arm is set, as a store
// whose service timed out would.
type failOnceStore struct {
	agentrun.StoreI
	mu  sync.Mutex
	arm bool
}

var errStoreDown = errors.New("the journal service did not answer")

func (inst *failOnceStore) Append(ctx context.Context, entries []agentrun.Entry) (err error) {
	inst.mu.Lock()
	armed := inst.arm
	inst.arm = false
	inst.mu.Unlock()
	if armed {
		return errStoreDown
	}
	return inst.StoreI.Append(ctx, entries)
}

// A writer a failed write left refusing every write is reopened on the
// next turn: the run is read back from the store, its chain verified, the
// record says so, and the turn runs.
func TestAFailedWriteIsRecoveredByReopeningTheRun(t *testing.T) {
	store := &failOnceStore{StoreI: agentrun.NewMemStore()}
	inst := appOver(t, &fakeModel{}, store)
	require.True(t, inst.startTurn("q1"))
	drainUntil(t, inst)
	settleTitle(t, inst)
	store.mu.Lock()
	store.arm = true
	store.mu.Unlock()
	require.True(t, inst.startTurn("q2"))
	drainUntil(t, inst)
	assert.True(t, inst.conv.entries[len(inst.conv.entries)-1].failed, "the turn the write failed in is failed")
	require.True(t, inst.startTurn("q3"))
	drainUntil(t, inst)
	last := inst.conv.entries[len(inst.conv.entries)-1]
	assert.Equal(t, speakerModel, last.speaker, "the next turn answered: %s", last.reason)
	entries := inst.coord.journalNow().Entries()
	var reopened bool
	for _, e := range entries {
		if e.Kind == agentrun.KindIgnorable && e.Name == "journal" && e.Form == "reopened" {
			reopened = true
		}
	}
	assert.True(t, reopened, "the record says the run was reopened")
	broken, err := agentrun.Verify(inst.coord.journalNow().Run().Digest, entries)
	require.NoError(t, err)
	assert.Zero(t, broken)
	msgs := projected(t, inst.conv)
	assert.Equal(t, "q3", msgs[len(msgs)-2].Content)
}

// The effect class of a call into a window is the operation's as the
// window's app declares it, whatever the operation-tools setting: the
// window's app is known from the listing and the opening, the catalog is
// read on demand; a call into a window nothing listed is consequential,
// the conservative reading (ADR-0269 §SD5, ADR-0308 §SD4).
func TestTheEffectClassIsTheCatalogsInTheDefaultConfig(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("c1", "request_access", `{"plan":"tidy the note","open":[{"app":"notes"}]}`),
		toolCall("c2", "open_window", `{"app":"notes"}`),
		toolCall("c3", "call_operation", `{"window":100,"operation":"get_note","args":{}}`),
		toolCall("c4", "call_operation", `{"window":100,"operation":"set_note","args":{"text":"tidied"}}`),
		toolCall("c5", "call_operation", `{"window":999,"operation":"set_note","args":{"text":"x"}}`),
		{Content: "done", FinishReason: "stop"},
	}}
	_, coord, cli, req, ctx := coordRig(t, bus, model, false)
	require.False(t, coord.opTools)
	_, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	effects := map[string]string{}
	for _, e := range coord.journalNow().Entries() {
		if e.Kind == agentrun.KindIntent && e.Name == "call_operation" {
			effects[e.Body] = e.Effect
		}
	}
	assert.Equal(t, agentrun.EffectNone, effects[`{"window":100,"operation":"get_note","args":{}}`], "a query")
	assert.Equal(t, agentrun.EffectDocument, effects[`{"window":100,"operation":"set_note","args":{"text":"tidied"}}`], "the catalog's class")
	assert.Equal(t, agentrun.EffectConsequential, effects[`{"window":999,"operation":"set_note","args":{"text":"x"}}`], "a window nothing listed")
}
